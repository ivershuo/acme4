package main

import (
	"os"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/providers/dns/porkbun"
)

func init() {
	RegisterProvider("porkbun", newPorkbunProvider)
}

func newPorkbunProvider(domain Domain) (challenge.Provider, error) {
	os.Setenv("PORKBUN_API_KEY", domain.Credentials["api_key"])
	os.Setenv("PORKBUN_SECRET_API_KEY", domain.Credentials["secret_api_key"])
	return porkbun.NewDNSProvider()
}
