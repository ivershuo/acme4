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
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"gopkg.in/yaml.v2"

	"acme4/providers"
)

const renewBeforeDefault = 30 // 默认提前30天续期

type Config struct {
	Email          string             `yaml:"email"`
	Domains        []providers.Domain `yaml:"domains"`
	CertDir        string             `yaml:"cert_dir"`
	AccountDir     string             `yaml:"account_dir"`
	PostRenewHooks []string           `yaml:"post_renew_hooks"`
	RenewBefore    int                `yaml:"renew_before"` // 证书到期前多少天续期
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

func obtainOrRenew(certDir string, user *MyUser, domain providers.Domain, hooks []string, renewBeforeDays int) error {
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
			return err
		}
		_ = os.WriteFile(certPath, certs.Certificate, 0600)
		_ = os.WriteFile(keyPath, certs.PrivateKey, 0600)
		log.Printf("证书 %v 已更新\n", domain.Names)
		runPostRenewHooks(hooks)
	} else {
		log.Printf("证书 %v 有效，无需续期，剩余 %v，到期时间 %v\n", domain.Names, remain, notAfter.Format("2006-01-02 15:04:05"))
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
	_ = ensureDir(cfg.CertDir)
	_ = ensureDir(cfg.AccountDir)

	user, err := loadOrCreateUser(cfg.Email, cfg.AccountDir)
	if err != nil {
		log.Fatalf("[致命] 账户初始化失败: %v\n请检查邮箱配置和账户目录权限。", err)
	}
	renewBefore := cfg.RenewBefore
	if renewBefore <= 0 {
		renewBefore = renewBeforeDefault
	}
	for _, d := range cfg.Domains {
		if err := obtainOrRenew(cfg.CertDir, user, d, cfg.PostRenewHooks, renewBefore); err != nil {
			log.Printf("[错误] 域名 %v 证书处理失败: %v\n建议检查 DNS 配置、Provider 凭证和网络连通性。", d.Names, err)
		}
	}
	log.Printf("程序结束: %s", time.Now().Format("2006-01-02 15:04:05"))
}
