package providers

import (
	"fmt"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"log"
)

type Domain struct {
	Names       []string          `yaml:"names"`
	Provider    string            `yaml:"provider"`
	Credentials map[string]string `yaml:"credentials"`
}

type DNSProviderFactory func(domain Domain) (challenge.Provider, error)

var providerRegistry = map[string]DNSProviderFactory{}

// RegisterProvider registers a DNS provider factory by name
func RegisterProvider(name string, factory DNSProviderFactory) {
	providerRegistry[name] = factory
}

// LoggingDNSProvider wraps a challenge.Provider and prints DNS instructions on Present
// Useful for manual intervention if DNS write fails or for debugging

type LoggingDNSProvider struct {
	wrapped challenge.Provider
	domain  string
}

func (l *LoggingDNSProvider) Present(domain, token, keyAuth string) error {
	info := dns01.GetChallengeInfo(domain, keyAuth)
	log.Printf("[手动DNS] 请为域名 %s 添加 TXT 记录：\n  主机: %s\n  类型: TXT\n  值: %s\n", domain, info.FQDN, info.Value)
	return l.wrapped.Present(domain, token, keyAuth)
}

func (l *LoggingDNSProvider) CleanUp(domain, token, keyAuth string) error {
	return l.wrapped.CleanUp(domain, token, keyAuth)
}

func (l *LoggingDNSProvider) Timeout() (timeout, interval int) {
	if t, ok := l.wrapped.(interface{ Timeout() (int, int) }); ok {
		return t.Timeout()
	}
	return 120, 2 // default
}

// GetDNSProvider gets the DNS provider by name, and wraps it with LoggingDNSProvider
func GetDNSProvider(domain Domain) (challenge.Provider, error) {
	factory, ok := providerRegistry[domain.Provider]
	if !ok {
		return nil, fmt.Errorf("unsupported provider: %s", domain.Provider)
	}
	prov, err := factory(domain)
	if err != nil {
		return nil, err
	}
	return &LoggingDNSProvider{wrapped: prov, domain: domain.Names[0]}, nil
}
