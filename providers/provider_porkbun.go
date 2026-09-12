package providers

import (
	"fmt"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/providers/dns/porkbun"
)

func init() {
	RegisterProvider("porkbun", newPorkbunProvider)
}

func newPorkbunProvider(domain Domain) (challenge.Provider, error) {
	apiKey, secretAPIKey := domain.Credentials["api_key"], domain.Credentials["secret_api_key"]
	if apiKey == "" || secretAPIKey == "" {
		return nil, fmt.Errorf("porkbun: credentials.api_key and credentials.secret_api_key are required")
	}
	config := porkbun.NewDefaultConfig()
	config.APIKey = apiKey
	config.SecretAPIKey = secretAPIKey
	return porkbun.NewDNSProviderConfig(config)
}
