package providers

import (
	"os"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
)

func init() {
	RegisterProvider("cloudflare", newCloudflareProvider)
}

func newCloudflareProvider(domain Domain) (challenge.Provider, error) {
	os.Setenv("CLOUDFLARE_API_TOKEN", domain.Credentials["api_token"])
	return cloudflare.NewDNSProvider()
}
