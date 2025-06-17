package main

import (
	"fmt"
	"github.com/go-acme/lego/v4/challenge"
)

type DNSProviderFactory func(domain Domain) (challenge.Provider, error)

var providerRegistry = map[string]DNSProviderFactory{}

// RegisterProvider registers a DNS provider factory by name
func RegisterProvider(name string, factory DNSProviderFactory) {
	providerRegistry[name] = factory
}

// GetDNSProvider gets the DNS provider by name
func GetDNSProvider(domain Domain) (challenge.Provider, error) {
	factory, ok := providerRegistry[domain.Provider]
	if !ok {
		return nil, fmt.Errorf("unsupported provider: %s", domain.Provider)
	}
	return factory(domain)
}
