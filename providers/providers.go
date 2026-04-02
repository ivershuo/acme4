package providers

import (
	"fmt"
	"log"
	"time"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
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

type loggingDNSProviderTimeout struct {
	*LoggingDNSProvider
	timeoutProvider challenge.ProviderTimeout
}

func (l *loggingDNSProviderTimeout) Timeout() (timeout, interval time.Duration) {
	return l.timeoutProvider.Timeout()
}

type loggingDNSProviderSequential struct {
	*LoggingDNSProvider
	sequentialProvider interface{ Sequential() time.Duration }
}

func (l *loggingDNSProviderSequential) Sequential() time.Duration {
	return l.sequentialProvider.Sequential()
}

type loggingDNSProviderTimeoutSequential struct {
	*LoggingDNSProvider
	timeoutProvider    challenge.ProviderTimeout
	sequentialProvider interface{ Sequential() time.Duration }
}

func (l *loggingDNSProviderTimeoutSequential) Timeout() (timeout, interval time.Duration) {
	return l.timeoutProvider.Timeout()
}

func (l *loggingDNSProviderTimeoutSequential) Sequential() time.Duration {
	return l.sequentialProvider.Sequential()
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

	loggingProvider := &LoggingDNSProvider{wrapped: prov, domain: domain.Names[0]}
	timeoutProvider, hasTimeout := prov.(challenge.ProviderTimeout)
	sequentialProvider, hasSequential := prov.(interface{ Sequential() time.Duration })

	switch {
	case hasTimeout && hasSequential:
		return &loggingDNSProviderTimeoutSequential{
			LoggingDNSProvider: loggingProvider,
			timeoutProvider:    timeoutProvider,
			sequentialProvider: sequentialProvider,
		}, nil
	case hasTimeout:
		return &loggingDNSProviderTimeout{
			LoggingDNSProvider: loggingProvider,
			timeoutProvider:    timeoutProvider,
		}, nil
	case hasSequential:
		return &loggingDNSProviderSequential{
			LoggingDNSProvider: loggingProvider,
			sequentialProvider: sequentialProvider,
		}, nil
	default:
		return loggingProvider, nil
	}
}
