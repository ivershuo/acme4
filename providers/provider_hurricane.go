package providers

import (
	"os"

	"acme4/providers/hurricane"

	"github.com/go-acme/lego/v4/challenge"
)

func init() {
	RegisterProvider("hurricane", newHurricaneProvider)
}

func newHurricaneProvider(domain Domain) (challenge.Provider, error) {
	os.Setenv("HURRICANE_TOKENS", domain.Credentials["api_key"])
	return hurricane.NewDNSProvider()
}
