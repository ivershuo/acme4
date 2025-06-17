package main

import (
	"os"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/providers/dns/hurricane"
)

func init() {
	RegisterProvider("hurricane", newHurricaneProvider)
}

func newHurricaneProvider(domain Domain) (challenge.Provider, error) {
	os.Setenv("HE_API_KEY", domain.Credentials["api_key"])
	return hurricane.NewDNSProvider()
}
