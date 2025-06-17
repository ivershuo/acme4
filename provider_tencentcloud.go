package main

import (
	"os"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/providers/dns/tencentcloud"
)

func init() {
	RegisterProvider("tencentcloud", newTencentcloudProvider)
}

func newTencentcloudProvider(domain Domain) (challenge.Provider, error) {
	os.Setenv("TENCENTCLOUD_SECRET_ID", domain.Credentials["secret_id"])
	os.Setenv("TENCENTCLOUD_SECRET_KEY", domain.Credentials["secret_key"])
	return tencentcloud.NewDNSProvider()
}
