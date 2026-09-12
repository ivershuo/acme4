package main

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"gopkg.in/yaml.v2"

	"acme4/notification"
	"acme4/providers"
	"acme4/providers/hurricane"
)

const renewBeforeDefault = 30 // 默认提前30天续期

type EmailNotificationConfig struct {
	Enabled         bool     `yaml:"enabled"`
	ResendAPIKey    string   `yaml:"resend_api_key"`
	FromEmail       string   `yaml:"from_email"`
	FromName        string   `yaml:"from_name"`
	ToEmails        []string `yaml:"to_emails"`
	NotifyOnSuccess *bool    `yaml:"notify_on_success"`
	NotifyOnFailure *bool    `yaml:"notify_on_failure"`
	NotifyOnExpiry  *bool    `yaml:"notify_on_expiry"`
}

type Config struct {
	Email             string                   `yaml:"email"`
	Domains           []providers.Domain       `yaml:"domains"`
	CertDir           string                   `yaml:"cert_dir"`
	AccountDir        string                   `yaml:"account_dir"`
	PostRenewHooks    []string                 `yaml:"post_renew_hooks"`
	Hooks             []HookConfig             `yaml:"hooks"`
	RenewBefore       int                      `yaml:"renew_before"` // 证书到期前多少天续期
	DNSResolvers      []string                 `yaml:"dns_resolvers"`
	ACMEDirectoryURL  string                   `yaml:"acme_directory_url"`
	EmailNotification *EmailNotificationConfig `yaml:"email_notification"`
}

type MyUser struct {
	Email          string
	Registration   *registration.Resource
	key            crypto.PrivateKey
	accountDir     string
	registrationCA string
}

func (u *MyUser) GetEmail() string {
	return u.Email
}
func (u *MyUser) GetRegistration() *registration.Resource {
	return u.Registration
}
func (u *MyUser) GetPrivateKey() crypto.PrivateKey {
	return u.key
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return nil, fmt.Errorf("strict YAML parse: %w", err)
	}
	if err := resolveCredentialFiles(&cfg, filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func resolveCredentialFiles(cfg *Config, configDir string) error {
	for i := range cfg.Domains {
		domain := &cfg.Domains[i]
		if domain.CredentialsFile == "" {
			continue
		}
		if len(domain.Credentials) > 0 {
			return fmt.Errorf("domains[%d]: credentials and credentials_file are mutually exclusive", i)
		}
		path := domain.CredentialsFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(configDir, path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("domains[%d].credentials_file: cannot read credential file: %w", i, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("domains[%d].credentials_file: must be a regular file", i)
		}
		if info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("domains[%d].credentials_file: permissions must not allow group or other access", i)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("domains[%d].credentials_file: cannot read credential file: %w", i, err)
		}
		credentials := map[string]string{}
		if err := yaml.UnmarshalStrict(content, &credentials); err != nil {
			return fmt.Errorf("domains[%d].credentials_file: invalid credential mapping", i)
		}
		domain.Credentials = credentials
	}
	return nil
}

func checkConfig(path string) error {
	if _, err := loadConfig(path); err != nil {
		return err
	}
	return nil
}

func boolDefault(value *bool, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	return *value
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0700)
}

func loadOrCreateUser(email, accountDir string) (*MyUser, error) {
	keyPath := filepath.Join(accountDir, email+".key")
	var key crypto.PrivateKey
	if _, err := os.Stat(keyPath); err == nil {
		keyBytes, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("read account key: %w", err)
		}
		block, _ := pem.Decode(keyBytes)
		if block == nil {
			return nil, fmt.Errorf("parse account key: invalid PEM")
		}
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse account key: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat account key: %w", err)
	} else {
		generated, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("generate account key: %w", err)
		}
		key = generated
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(generated)})
		temp, err := writeTempFile(accountDir, ".account-key-*", keyPEM)
		if err != nil {
			return nil, fmt.Errorf("write account key: %w", err)
		}
		defer os.Remove(temp)
		if err := replaceFile(temp, keyPath); err != nil {
			return nil, fmt.Errorf("commit account key: %w", err)
		}
	}
	user := &MyUser{Email: email, key: key, accountDir: accountDir}
	return user, nil
}

func certPaths(certDir string, names []string) (certPath, keyPath string) {
	base := names[0] // 用第一个域名做文件名
	return filepath.Join(certDir, base+".crt"), filepath.Join(certDir, base+".key")
}

// certNeedRenew 返回证书剩余有效期、到期时间和错误
func certNeedRenew(certPath string) (time.Duration, time.Time, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, time.Time{}, nil // 文件不存在，视为需要申请
		}
		return 0, time.Time{}, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return 0, time.Time{}, fmt.Errorf("invalid cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return 0, time.Time{}, err
	}
	remain := time.Until(cert.NotAfter)
	return remain, cert.NotAfter, nil
}

func obtainOrRenew(certDir string, user *MyUser, domain providers.Domain, hooks []string, structuredHooks []HookConfig, renewBeforeDays int, emailService *notification.EmailService, acmeDirectoryURL string, dnsResolvers []string) error {
	if len(domain.Names) == 0 {
		return errors.New("configuration: domain names cannot be empty")
	}
	if pending, summary, err := deployPendingCertificate(certDir, domain.Names, hooks, structuredHooks); err != nil {
		return fmt.Errorf("deployment retry: %w", err)
	} else if pending {
		log.Printf("域名 %v: %s", domain.Names, summary)
		pendingCertPath, pendingKeyPath := certPaths(certDir, domain.Names)
		if status, inspectErr := inspectCertificatePair(pendingCertPath, pendingKeyPath, domain.Names, renewBeforeDays); inspectErr == nil {
			state, _ := loadDeploymentState(certDir, domain.Names)
			sendRenewalSuccessOnce(emailService, certDir, domain.Names, status, summary, state.CurrentVersion)
		}
		return nil
	}
	certPath, keyPath := certPaths(certDir, domain.Names)
	status, err := inspectCertificatePair(certPath, keyPath, domain.Names, renewBeforeDays)
	if err != nil {
		return fmt.Errorf("certificate inspection: %w", err)
	}
	sendExpiryNotificationOnce(emailService, certDir, domain.Names, status, renewBeforeDays)
	if status.NeedsRenew {
		if status.NotAfter.IsZero() {
			log.Printf("证书 %v 需要申请，原因=%s", domain.Names, status.Reason)
		} else {
			log.Printf("证书 %v 需要更新，原因=%s 剩余有效期=%v 到期时间=%v", domain.Names, status.Reason, status.Remaining, status.NotAfter.Format("2006-01-02 15:04:05"))
		}
		provider, err := providers.GetDNSProvider(domain)
		if err != nil {
			return fmt.Errorf("provider configuration: %w", err)
		}
		config := lego.NewConfig(user)
		if acmeDirectoryURL != "" {
			config.CADirURL = acmeDirectoryURL
		}
		if err := loadRegistrationForCA(user, config.CADirURL); err != nil {
			return fmt.Errorf("load ACME registration: %w", err)
		}
		client, err := lego.NewClient(config)
		if err != nil {
			return fmt.Errorf("ACME client initialization: %w", err)
		}
		var dnsOptions []dns01.ChallengeOption
		if len(dnsResolvers) > 0 {
			dnsOptions = append(dnsOptions, dns01.AddRecursiveNameservers(dnsResolvers))
		}
		if err := client.Challenge.SetDNS01Provider(provider, dnsOptions...); err != nil {
			return fmt.Errorf("DNS-01 configuration: %w", err)
		}

		if user.Registration == nil {
			reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
			if err != nil {
				return fmt.Errorf("ACME account registration: %w", err)
			}
			user.Registration = reg
			if err := saveRegistrationForCA(user, config.CADirURL); err != nil {
				return fmt.Errorf("persist ACME registration: %w", err)
			}
		}

		request := certificate.ObtainRequest{
			Domains: domain.Names,
			Bundle:  true,
		}
		certs, err := client.Certificate.Obtain(request)
		if err != nil {
			return fmt.Errorf("validation/issuance: %w", err)
		}
		if err := savePendingCertificate(certDir, domain.Names, certs.Certificate, certs.PrivateKey); err != nil {
			return fmt.Errorf("certificate persistence: %w", err)
		}
		_, hookSummary, err := deployPendingCertificate(certDir, domain.Names, hooks, structuredHooks)
		if err != nil {
			return fmt.Errorf("deployment: %w", err)
		}
		log.Printf("证书 %v 已更新并完成部署步骤\n", domain.Names)

		// 获取新证书信息并发送成功通知
		newStatus, _ := inspectCertificatePair(certPath, keyPath, domain.Names, renewBeforeDays)
		state, _ := loadDeploymentState(certDir, domain.Names)
		sendRenewalSuccessOnce(emailService, certDir, domain.Names, newStatus, hookSummary, state.CurrentVersion)
	} else {
		log.Printf("证书 %v 有效，无需续期，剩余 %v，到期时间 %v\n", domain.Names, status.Remaining, status.NotAfter.Format("2006-01-02 15:04:05"))
		state, _ := loadDeploymentState(certDir, domain.Names)
		sendRenewalSuccessOnce(emailService, certDir, domain.Names, status, "此前成功事件通知重试", state.CurrentVersion)
	}
	return nil
}

func installAndDeploy(certPath, keyPath string, certPEM, keyPEM []byte, hooks []string, domain string) (string, error) {
	return installAndDeployConfigured(certPath, keyPath, certPEM, keyPEM, hooks, nil, domain)
}

func installAndDeployConfigured(certPath, keyPath string, certPEM, keyPEM []byte, hooks []string, structuredHooks []HookConfig, domain string) (string, error) {
	if err := saveCertificatePair(certPath, keyPath, certPEM, keyPEM); err != nil {
		return "", fmt.Errorf("certificate persistence: %w", err)
	}
	var summary string
	var err error
	if len(structuredHooks) > 0 {
		summary, err = runStructuredHooks(structuredHooks, domain, certPath, keyPath)
	} else {
		summary, err = runPostRenewHooks(hooks, domain, certPath, keyPath)
	}
	if err != nil {
		return summary, fmt.Errorf("deployment: %w", err)
	}
	return summary, nil
}

func writeTempFile(dir, pattern string, data []byte) (path string, err error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path = f.Name()
	defer func() {
		if closeErr := f.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	if err = f.Chmod(0600); err != nil {
		return path, err
	}
	if _, err = f.ReadFrom(bytes.NewReader(data)); err != nil {
		return path, err
	}
	if err = f.Sync(); err != nil {
		return path, err
	}
	return path, nil
}

func saveCertificatePair(certPath, keyPath string, certPEM, keyPEM []byte) error {
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return fmt.Errorf("certificate and private key do not match or are invalid: %w", err)
	}

	certTemp, err := writeTempFile(filepath.Dir(certPath), ".acme4-cert-*", certPEM)
	if err != nil {
		return fmt.Errorf("write certificate temporary file: %w", err)
	}
	defer os.Remove(certTemp)
	keyTemp, err := writeTempFile(filepath.Dir(keyPath), ".acme4-key-*", keyPEM)
	if err != nil {
		return fmt.Errorf("write private key temporary file: %w", err)
	}
	defer os.Remove(keyTemp)
	for _, destination := range []string{certPath, keyPath} {
		if info, statErr := os.Stat(destination); statErr == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("destination is not a regular file: %s", destination)
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return fmt.Errorf("inspect destination %s: %w", destination, statErr)
		}
	}
	oldKey, oldKeyErr := os.ReadFile(keyPath)
	keyExisted := oldKeyErr == nil
	if oldKeyErr != nil && !os.IsNotExist(oldKeyErr) {
		return fmt.Errorf("backup private key: %w", oldKeyErr)
	}

	if err := replaceFile(keyTemp, keyPath); err != nil {
		return fmt.Errorf("install private key: %w", err)
	}
	if err := replaceFile(certTemp, certPath); err != nil {
		var rollbackErr error
		if keyExisted {
			rollbackErr = os.WriteFile(keyPath, oldKey, 0600)
		} else {
			rollbackErr = os.Remove(keyPath)
		}
		return errors.Join(fmt.Errorf("install certificate: %w", err), rollbackErr)
	}
	return nil
}

func shouldSendExpiryWarning(remain time.Duration, renewBeforeDays int) bool {
	if remain <= 0 {
		return true
	}

	remainingDays := int(math.Ceil(remain.Hours() / 24))
	for _, day := range []int{renewBeforeDays + 7, renewBeforeDays + 3, renewBeforeDays + 1} {
		if remainingDays == day {
			return true
		}
	}

	return false
}

func hurricaneFailureAdvice(err error, names []string) string {
	if err == nil {
		return ""
	}

	switch hurricane.DiagnosticCodeFromError(err) {
	case hurricane.DiagnosticInterval:
		return "Hurricane Electric 返回 interval，表示动态 DNS 更新触发限流；建议拉长 HURRICANE_SEQUENCE_INTERVAL、降低续期频率，或避免短时间内重复清理/写入同一 TXT 记录。"
	case hurricane.DiagnosticBadAuth:
		return "Hurricane Electric 返回 badauth，表示 TXT 动态更新 token 不匹配；请检查 credentials.api_key/HURRICANE_TOKENS 中域名或完整主机名对应的 token。"
	case hurricane.DiagnosticNoHost:
		return "Hurricane Electric 返回 nohost，表示该 TXT 记录未在账号中作为动态记录存在；请确认 _acme-challenge 记录已创建并启用 dynamic DNS。"
	}

	errText := err.Error()
	if strings.Contains(errText, "propagation") || strings.Contains(errText, "DNS record") {
		return fmt.Sprintf("DNS-01 传播检查失败；请确认挑战 TXT 已在权威 DNS 可见。当前域名 %v 可能共享同一个 _acme-challenge 记录，Hurricane 会顺序写入/清理，失败可能来自上一轮清理值 `.` 或新值尚未传播完成。", names)
	}

	return ""
}

func logHurricaneSharedChallengeWarnings(domains []providers.Domain) {
	for _, d := range domains {
		hosts := map[string][]string{}
		for _, name := range d.Names {
			host := hurricaneChallengeHost(name)
			hosts[host] = append(hosts[host], name)
		}

		for host, names := range hosts {
			if len(names) < 2 {
				continue
			}
			log.Printf("[Hurricane Electric] 风险提示: 域名 %v 共享 TXT 记录 %s。Hurricane 动态 DNS 不能像常规 DNS API 一样维护同名多 TXT 值，lego 会顺序写入、验证、清理；如果续期失败，优先检查动态更新传播延迟、interval 限流、token 和记录是否启用 dynamic DNS。", names, host)
		}
	}
}

func hurricaneChallengeHost(name string) string {
	base := strings.TrimPrefix(strings.TrimSuffix(name, "."), "*.")
	return "_acme-challenge." + base
}

var hookPlaceholderPattern = regexp.MustCompile(`\{[a-z_]+\}`)

func expandHookCommand(cmdStr, domain, certPath, keyPath string) (string, error) {
	replacements := strings.NewReplacer(
		"{domain}", domain,
		"{cert_path}", certPath,
		"{key_path}", keyPath,
	)

	expanded := replacements.Replace(cmdStr)
	if placeholder := hookPlaceholderPattern.FindString(expanded); placeholder != "" {
		return "", fmt.Errorf("unsupported hook placeholder: %s", placeholder)
	}

	return expanded, nil
}

func runPostRenewHooks(hooks []string, domain, certPath, keyPath string) (string, error) {
	if len(hooks) == 0 {
		return "未配置后续命令；证书文件已更新。", nil
	}

	successCount := 0
	failureCount := 0
	for _, cmdStr := range hooks {
		expanded, err := expandHookCommand(cmdStr, domain, certPath, keyPath)
		if err != nil {
			log.Printf("[后续命令失败] 命令模板: %s，错误: %v\n请检查 hook 占位符是否正确。", cmdStr, err)
			failureCount++
			continue
		}

		log.Printf("执行后续命令: %s", expanded)
		cmd := exec.Command("sh", "-c", expanded)
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[后续命令失败] 命令: %s，错误: %v，输出: %s\n请检查命令是否可用及相关权限。", expanded, err, string(output))
			failureCount++
		} else {
			log.Printf("后续命令成功，输出: %s", string(output))
			successCount++
		}
	}

	if failureCount > 0 {
		summary := fmt.Sprintf("后续命令执行完成：成功 %d 个，失败 %d 个；请查看日志确认服务是否已加载新证书。", successCount, failureCount)
		return summary, errors.New(summary)
	}

	return fmt.Sprintf("后续命令执行完成：成功 %d 个，失败 0 个。", successCount), nil
}

func run(configPath string) error {
	runID := fmt.Sprintf("%x", time.Now().UnixNano())
	log.Printf("run=%s stage=start config=%s", runID, configPath)
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("配置文件加载失败: %w", err)
	}
	if cfg.AccountDir == "" || cfg.CertDir == "" {
		return errors.New("配置错误: account_dir 和 cert_dir 不能为空")
	}
	if err := ensureDir(cfg.AccountDir); err != nil {
		return fmt.Errorf("创建账户目录失败: %w", err)
	}
	lock, err := acquireOperationLock(cfg.AccountDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := ensureDir(cfg.CertDir); err != nil {
		return fmt.Errorf("创建证书目录失败: %w", err)
	}

	resolvers, err := normalizeResolvers(cfg.DNSResolvers)
	if err != nil {
		return fmt.Errorf("DNS resolver 配置错误: %w", err)
	}
	if len(resolvers) > 0 {
		log.Printf("使用自定义 DNS 检测解析器: %s", strings.Join(resolvers, ", "))
	}

	user, err := loadOrCreateUser(cfg.Email, cfg.AccountDir)
	if err != nil {
		return fmt.Errorf("账户初始化失败: %w", err)
	}
	// 初始化邮件服务
	var emailService *notification.EmailService
	if cfg.EmailNotification != nil && cfg.EmailNotification.Enabled {
		notifyOnSuccess := boolDefault(cfg.EmailNotification.NotifyOnSuccess, true)
		notifyOnFailure := boolDefault(cfg.EmailNotification.NotifyOnFailure, true)
		notifyOnExpiry := boolDefault(cfg.EmailNotification.NotifyOnExpiry, true)
		emailService = notification.NewEmailService(
			cfg.EmailNotification.ResendAPIKey,
			cfg.EmailNotification.FromEmail,
			cfg.EmailNotification.FromName,
			cfg.EmailNotification.ToEmails,
			cfg.EmailNotification.Enabled,
			notifyOnSuccess,
			notifyOnFailure,
			notifyOnExpiry,
		)
		if emailService.IsEnabled() {
			log.Printf("邮件通知服务已启用，收件人: %v，成功通知: %t，失败通知: %t，到期提醒: %t", cfg.EmailNotification.ToEmails, notifyOnSuccess, notifyOnFailure, notifyOnExpiry)
		} else {
			log.Printf("邮件通知配置已启用但缺少 Resend API Key，邮件通知已禁用")
		}
	} else {
		log.Printf("邮件通知服务未启用")
	}

	renewBefore := cfg.RenewBefore
	if renewBefore <= 0 {
		renewBefore = renewBeforeDefault
	}

	var hurricaneDomains []providers.Domain
	var otherDomains []providers.Domain
	for _, d := range cfg.Domains {
		if d.Provider == "hurricane" {
			hurricaneDomains = append(hurricaneDomains, d)
		} else {
			otherDomains = append(otherDomains, d)
		}
	}
	logHurricaneSharedChallengeWarnings(hurricaneDomains)

	var failures []error
	process := func(d providers.Domain) {
		started := time.Now()
		certificateID := filepath.Base(certificateStateDir(cfg.CertDir, d.Names))
		if err := obtainOrRenew(cfg.CertDir, user, d, cfg.PostRenewHooks, cfg.Hooks, renewBefore, emailService, cfg.ACMEDirectoryURL, resolvers); err != nil {
			log.Printf("run=%s certificate=%s stage=complete result=failed category=%s duration=%s domains=%v error=%v", runID, certificateID, failureCategory(err), time.Since(started), d.Names, err)
			if d.Provider == "hurricane" {
				if advice := hurricaneFailureAdvice(err, d.Names); advice != "" {
					log.Printf("[Hurricane Electric] 诊断建议: %s", advice)
				}
			}
			if emailService != nil && emailService.IsEnabled() {
				now := time.Now()
				category := failureCategory(err)
				ledger, due := failureNotificationDue(cfg.CertDir, d.Names, category, now)
				if !due {
					failures = append(failures, fmt.Errorf("%v: %w", d.Names, err))
					return
				}
				advice := ""
				if d.Provider == "hurricane" {
					advice = hurricaneFailureAdvice(err, d.Names)
				}
				notificationData := notification.NotificationData{
					Domains:   d.Names,
					Success:   false,
					Error:     err.Error(),
					Timestamp: time.Now(),
					Advice:    advice,
				}
				if emailErr := emailService.SendFailureNotification(notificationData); emailErr != nil {
					log.Printf("[警告] 邮件通知发送失败: %v", emailErr)
				} else if stateErr := markFailureNotification(cfg.CertDir, d.Names, ledger, category, now); stateErr != nil {
					log.Printf("[警告] 失败通知去重状态保存失败: %v", stateErr)
				}
			}
			failures = append(failures, fmt.Errorf("%v: %w", d.Names, err))
			return
		}
		log.Printf("run=%s certificate=%s stage=complete result=success duration=%s domains=%v", runID, certificateID, time.Since(started), d.Names)
	}
	for _, d := range otherDomains {
		process(d)
	}
	log.Printf("[Hurricane Electric] 开始顺序处理 %d 个 Hurricane Electric 域名", len(hurricaneDomains))
	for i, d := range hurricaneDomains {
		log.Printf("[Hurricane Electric] 处理第 %d/%d 个域名: %v", i+1, len(hurricaneDomains), d.Names)
		process(d)
	}
	log.Printf("处理汇总: 总计=%d 成功=%d 失败=%d", len(cfg.Domains), len(cfg.Domains)-len(failures), len(failures))
	if len(failures) > 0 {
		log.Printf("run=%s stage=finish result=failed failures=%d", runID, len(failures))
		return fmt.Errorf("处理完成，共 %d 组域名失败: %w", len(failures), errors.Join(failures...))
	}
	log.Printf("run=%s stage=finish result=success", runID)
	return nil
}

func normalizeResolvers(values []string) ([]string, error) {
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if host, port, err := net.SplitHostPort(value); err == nil {
			portNumber, parseErr := strconv.Atoi(port)
			if parseErr != nil || portNumber < 1 || portNumber > 65535 {
				return nil, fmt.Errorf("解析器 %q 的端口无效", value)
			}
			result = append(result, net.JoinHostPort(host, port))
			continue
		}
		if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
		}
		if ip := net.ParseIP(value); ip != nil {
			result = append(result, net.JoinHostPort(value, "53"))
			continue
		}
		if strings.Contains(value, ":") {
			return nil, fmt.Errorf("解析器 %q 不是有效的 IPv6 地址或 host:port", value)
		}
		result = append(result, net.JoinHostPort(value, "53"))
	}
	return result, nil
}

func main() {
	log.Printf("程序启动: %s", time.Now().Format("2006-01-02 15:04:05"))
	sslDomain := flag.String("ssl-domain", "", "检查远程主机(域名)的TLS证书信息")
	sslServerName := flag.String("ssl-server-name", "", "远程 TLS 诊断使用的显式 SNI/主机名校验名称")
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	checkConfigOnly := flag.Bool("check-config", false, "只校验配置，不申请证书或执行 hook")
	flag.Parse()

	if *checkConfigOnly {
		if *sslDomain != "" || *sslServerName != "" {
			log.Printf("[失败] -check-config 与 -ssl-domain 不能同时使用")
			os.Exit(2)
		}
		if err := checkConfig(*configPath); err != nil {
			log.Printf("[配置无效] %v", err)
			os.Exit(2)
		}
		log.Printf("配置有效: %s", *configPath)
		return
	}

	if *sslDomain != "" {
		var err error
		if *sslServerName == "" {
			err = checkRemoteDomain(*sslDomain)
		} else {
			var endpoint remoteEndpoint
			endpoint, err = parseRemoteEndpoint(*sslDomain)
			if err == nil {
				err = checkRemoteEndpoint(endpoint, *sslServerName)
			}
		}
		if err != nil {
			fmt.Printf("检查远程域名失败: %v\n", err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	if *sslServerName != "" {
		log.Printf("[失败] -ssl-server-name 必须与 -ssl-domain 一起使用")
		os.Exit(2)
	}

	if err := run(*configPath); err != nil {
		log.Printf("[失败] %v", err)
		os.Exit(1)
	}
	log.Printf("程序结束: %s", time.Now().Format("2006-01-02 15:04:05"))
}
