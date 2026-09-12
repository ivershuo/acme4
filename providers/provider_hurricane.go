package providers

import (
	"fmt"
	"strings"

	"acme4/providers/hurricane"

	"github.com/go-acme/lego/v4/challenge"
)

func init() {
	RegisterProvider("hurricane", newHurricaneProvider)
}

func newHurricaneProvider(domain Domain) (challenge.Provider, error) {
	raw := domain.Credentials["api_key"]
	if raw == "" {
		return nil, fmt.Errorf("hurricane: credentials.api_key is required")
	}

	credentials, err := parseCredentialPairs(raw)
	if err != nil {
		return nil, fmt.Errorf("hurricane: credentials: %w", err)
	}
	config := hurricane.NewDefaultConfig()
	config.Credentials = credentials
	return hurricane.NewDNSProviderConfig(config)
}

func parseCredentialPairs(raw string) (map[string]string, error) {
	result := make(map[string]string)
	for _, pair := range strings.Split(strings.TrimSuffix(raw, ","), ",") {
		pair = strings.TrimSpace(pair)
		separator := ":"
		colonAt, equalsAt := strings.Index(pair, ":"), strings.Index(pair, "=")
		if colonAt < 0 || (equalsAt >= 0 && equalsAt < colonAt) {
			separator = "=" // Compatibility with versions of this project's old README.
		}
		parts := strings.SplitN(pair, separator, 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("incorrect pair %q; expected key:token", pair)
		}
		result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return result, nil
}
