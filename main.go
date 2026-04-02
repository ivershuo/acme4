package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"gopkg.in/yaml.v2"

	"acme4/notification"
	"acme4/providers"
)

const renewBeforeDefault = 30 // 默认提前30天续期

type EmailNotificationConfig struct {
	Enabled         bool     `yaml:"enabled"`
	ResendAPIKey    string   `yaml:"resend_api_key"`
	FromEmail       string   `yaml:"from_email"`
	FromName        string   `yaml:"from_name"`
	ToEmails        []string `yaml:"to_emails"`
	NotifyOnSuccess bool     `yaml:"notify_on_success"`
	NotifyOnFailure bool     `yaml:"notify_on_failure"`
	NotifyOnExpiry  bool     `yaml:"notify_on_expiry"`
}

type Config struct {
	Email             string                   `yaml:"email"`
	Domains           []providers.Domain       `yaml:"domains"`
	CertDir           string                   `yaml:"cert_dir"`
	AccountDir        string                   `yaml:"account_dir"`
	PostRenewHooks    []string                 `yaml:"post_renew_hooks"`
	RenewBefore       int                      `yaml:"renew_before"` // 证书到期前多少天续期
	DNSResolvers      []string                 `yaml:"dns_resolvers"`
	EmailNotification *EmailNotificationConfig `yaml:"email_notification"`
}

type MyUser struct {
	Email        string
	Registration *registration.Resource
	key          crypto.PrivateKey
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
	err = yaml.Unmarshal(data, &cfg)
	return &cfg, err
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0700)
}

func loadOrCreateUser(email, accountDir string) (*MyUser, error) {
	keyPath := filepath.Join(accountDir, email+".key")
	var key crypto.PrivateKey
	if _, err := os.Stat(keyPath); err == nil {
		keyBytes, _ := os.ReadFile(keyPath)
		block, _ := pem.Decode(keyBytes)
		key, _ = x509.ParsePKCS1PrivateKey(block.Bytes)
	} else {
		key, _ = rsa.GenerateKey(rand.Reader, 2048)
		keyOut, _ := os.Create(keyPath)
		defer keyOut.Close()
		pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key.(*rsa.PrivateKey))})
	}
	user := &MyUser{Email: email, key: key}
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
		return 0, time.Time{}, nil // 文件不存在，视为需要申请
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

func obtainOrRenew(certDir string, user *MyUser, domain providers.Domain, hooks []string, renewBeforeDays int, emailService *notification.EmailService) error {
	provider, err := providers.GetDNSProvider(domain)
	if err != nil {
		return err
	}
	config := lego.NewConfig(user)
	client, err := lego.NewClient(config)
	if err != nil {
		return err
	}
	client.Challenge.SetDNS01Provider(provider)

	if user.Registration == nil {
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return err
		}
		user.Registration = reg
	}

	certPath, keyPath := certPaths(certDir, domain.Names)
	remain, notAfter, err := certNeedRenew(certPath)
	if err != nil {
		log.Printf("[警告] 证书状态检查失败，域名: %v，原因: %v\n请检查证书文件是否存在且格式正确。", domain.Names, err)
	}
	if remain == 0 || remain < time.Duration(renewBeforeDays)*24*time.Hour {
		if notAfter.IsZero() {
			log.Printf("证书 %v 不存在或解析失败，准备申请...", domain.Names)
		} else {
			log.Printf("证书 %v 剩余有效期 %v, 到期时间 %v，准备续期...", domain.Names, remain, notAfter.Format("2006-01-02 15:04:05"))
		}
		request := certificate.ObtainRequest{
			Domains: domain.Names,
			Bundle:  true,
		}
		certs, err := client.Certificate.Obtain(request)
		if err != nil {
			// 发送失败通知
			if emailService != nil && emailService.IsEnabled() {
				notificationData := notification.NotificationData{
					Domains:   domain.Names,
					Success:   false,
					Error:     err.Error(),
					Timestamp: time.Now(),
				}
				if emailErr := emailService.SendFailureNotification(notificationData); emailErr != nil {
					log.Printf("[警告] 邮件通知发送失败: %v", emailErr)
				}
			}
			return err
		}
		_ = os.WriteFile(certPath, certs.Certificate, 0600)
		_ = os.WriteFile(keyPath, certs.PrivateKey, 0600)
		log.Printf("证书 %v 已更新\n", domain.Names)
		runPostRenewHooks(hooks)

		// 获取新证书信息并发送成功通知
		if emailService != nil && emailService.IsEnabled() {
			newRemain, newNotAfter, _ := certNeedRenew(certPath)
			notificationData := notification.NotificationData{
				Domains:   domain.Names,
				Success:   true,
				Timestamp: time.Now(),
			}
			if newRemain > 0 {
				notificationData.CertExpiry = &newNotAfter
				notificationData.Remaining = &newRemain
			}
			if emailErr := emailService.SendSuccessNotification(notificationData); emailErr != nil {
				log.Printf("[警告] 邮件通知发送失败: %v", emailErr)
			}
		}
	} else {
		log.Printf("证书 %v 有效，无需续期，剩余 %v，到期时间 %v\n", domain.Names, remain, notAfter.Format("2006-01-02 15:04:05"))

		// 检查是否需要发送即将到期警告（可选功能）
		if emailService != nil && emailService.IsEnabled() && remain < time.Duration(renewBeforeDays+7)*24*time.Hour {
			notificationData := notification.NotificationData{
				Domains:    domain.Names,
				Success:    true,
				Timestamp:  time.Now(),
				CertExpiry: &notAfter,
				Remaining:  &remain,
			}
			if emailErr := emailService.SendExpiryWarningNotification(notificationData); emailErr != nil {
				log.Printf("[警告] 即将到期邮件通知发送失败: %v", emailErr)
			}
		}
	}
	return nil
}

func runPostRenewHooks(hooks []string) {
	for _, cmdStr := range hooks {
		log.Printf("执行后续命令: %s", cmdStr)
		cmd := exec.Command("sh", "-c", cmdStr)
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[后续命令失败] 命令: %s，错误: %v，输出: %s\n请检查命令是否可用及相关权限。", cmdStr, err, string(output))
		} else {
			log.Printf("后续命令成功，输出: %s", string(output))
		}
	}
}

func main() {
	log.Printf("程序启动: %s", time.Now().Format("2006-01-02 15:04:05"))
	sslDomain := flag.String("ssl-domain", "", "检查远程主机(域名)的TLS证书信息")
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	if *sslDomain != "" {
		err := checkRemoteDomain(*sslDomain)
		if err != nil {
			fmt.Printf("检查远程域名失败: %v\n", err)
			os.Exit(2)
		}
		os.Exit(0)
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("[致命] 配置文件加载失败: %v\n请检查 config.yaml 路径和内容是否正确。", err)
	}
	// 如果配置中指定了 DNS 解析器，则仅在此时设置为生效
	if len(cfg.DNSResolvers) > 0 {
		var normalized []string
		for _, r := range cfg.DNSResolvers {
			rr := strings.TrimSpace(r)
			if rr == "" {
				continue
			}
			if !strings.Contains(rr, ":") {
				rr = rr + ":53"
			}
			normalized = append(normalized, rr)
		}
		if len(normalized) > 0 {
			os.Setenv("LEGO_DNS_RESOLVERS", strings.Join(normalized, ","))
			log.Printf("使用自定义 DNS 检测解析器: %s", strings.Join(cfg.DNSResolvers, ", "))
		}
	}
	_ = ensureDir(cfg.CertDir)
	_ = ensureDir(cfg.AccountDir)

	user, err := loadOrCreateUser(cfg.Email, cfg.AccountDir)
	if err != nil {
		log.Fatalf("[致命] 账户初始化失败: %v\n请检查邮箱配置和账户目录权限。", err)
	}
	// 初始化邮件服务
	var emailService *notification.EmailService
	if cfg.EmailNotification != nil && cfg.EmailNotification.Enabled {
		emailService = notification.NewEmailService(
			cfg.EmailNotification.ResendAPIKey,
			cfg.EmailNotification.FromEmail,
			cfg.EmailNotification.FromName,
			cfg.EmailNotification.ToEmails,
			cfg.EmailNotification.Enabled,
		)
		log.Printf("邮件通知服务已启用，收件人: %v", cfg.EmailNotification.ToEmails)
	} else {
		log.Printf("邮件通知服务未启用")
	}

	renewBefore := cfg.RenewBefore
	if renewBefore <= 0 {
		renewBefore = renewBeforeDefault
	}

	// 分离 Hurricane Electric 和其他 provider 的域名
	var hurricaneDomains []providers.Domain
	var otherDomains []providers.Domain

	for _, d := range cfg.Domains {
		if d.Provider == "hurricane" {
			hurricaneDomains = append(hurricaneDomains, d)
		} else {
			otherDomains = append(otherDomains, d)
		}
	}

	// 先处理其他 provider 的域名
	for _, d := range otherDomains {
		if err := obtainOrRenew(cfg.CertDir, user, d, cfg.PostRenewHooks, renewBefore, emailService); err != nil {
			log.Printf("[错误] 域名 %v 证书处理失败: %v\n建议检查 DNS 配置、Provider 凭证和网络连通性。", d.Names, err)
		}
	}

	// 然后按顺序处理 Hurricane Electric 的域名，避免并发冲突
	log.Printf("[Hurricane Electric] 开始顺序处理 %d 个 Hurricane Electric 域名", len(hurricaneDomains))
	for i, d := range hurricaneDomains {
		log.Printf("[Hurricane Electric] 处理第 %d/%d 个域名: %v", i+1, len(hurricaneDomains), d.Names)
		if err := obtainOrRenew(cfg.CertDir, user, d, cfg.PostRenewHooks, renewBefore, emailService); err != nil {
			log.Printf("[错误] Hurricane Electric 域名 %v 证书处理失败: %v\n建议检查 DNS 配置、Provider 凭证和网络连通性。", d.Names, err)
		}
		// 在 Hurricane Electric 域名之间添加延迟，确保 DNS 记录完全生效
		// if i < len(hurricaneDomains)-1 {
		// 	log.Printf("[Hurricane Electric] 等待 10 秒后处理下一个域名...")
		// 	time.Sleep(10 * time.Second)
		// }
	}
	log.Printf("程序结束: %s", time.Now().Format("2006-01-02 15:04:05"))
}
