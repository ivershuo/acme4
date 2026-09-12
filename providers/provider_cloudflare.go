package providers

import (
	"fmt"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
)

func init() {
	RegisterProvider("cloudflare", newCloudflareProvider)
}

func newCloudflareProvider(domain Domain) (challenge.Provider, error) {
	dnsToken := domain.Credentials["api_token"]
	if dnsToken == "" {
		return nil, fmt.Errorf("cloudflare: credentials.api_token is required")
	}

	config := cloudflare.NewDefaultConfig()
	config.AuthToken = dnsToken
	// A separate Zone Read token is optional. When omitted, use the DNS token,
	// which must carry both Zone:Read and DNS:Edit for the validation zone.
	config.ZoneToken = domain.Credentials["zone_api_token"]
	if config.ZoneToken == "" {
		config.ZoneToken = dnsToken
	}

	return cloudflare.NewDNSProviderConfig(config)
}
