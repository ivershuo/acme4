package providers

import (
	"fmt"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/providers/dns/tencentcloud"
)

func init() {
	RegisterProvider("tencentcloud", newTencentcloudProvider)
}

func newTencentcloudProvider(domain Domain) (challenge.Provider, error) {
	secretID, secretKey := domain.Credentials["secret_id"], domain.Credentials["secret_key"]
	if secretID == "" || secretKey == "" {
		return nil, fmt.Errorf("tencentcloud: credentials.secret_id and credentials.secret_key are required")
	}
	config := tencentcloud.NewDefaultConfig()
	config.SecretID = secretID
	config.SecretKey = secretKey
	config.SessionToken = domain.Credentials["session_token"]
	config.Region = domain.Credentials["region"]
	return tencentcloud.NewDNSProviderConfig(config)
}
