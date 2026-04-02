package hurricane

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/platform/config/env"
)

// Environment variables names.
const (
	envNamespace = "HURRICANE_"

	EnvTokens = envNamespace + "TOKENS"

	EnvPropagationTimeout = envNamespace + "PROPAGATION_TIMEOUT"
	EnvPollingInterval    = envNamespace + "POLLING_INTERVAL"
	EnvHTTPTimeout        = envNamespace + "HTTP_TIMEOUT"
	EnvSequenceInterval   = envNamespace + "SEQUENCE_INTERVAL"
)

var _ challenge.ProviderTimeout = (*DNSProvider)(nil)

// Config is used to configure the creation of the DNSProvider.
type Config struct {
	Credentials        map[string]string
	PropagationTimeout time.Duration
	PollingInterval    time.Duration
	SequenceInterval   time.Duration
	HTTPClient         *http.Client
}

// NewDefaultConfig returns a default configuration for the DNSProvider.
func NewDefaultConfig() *Config {
	return &Config{
		PropagationTimeout: env.GetOrDefaultSecond(EnvPropagationTimeout, 300*time.Second),
		PollingInterval:    env.GetOrDefaultSecond(EnvPollingInterval, dns01.DefaultPollingInterval),
		SequenceInterval:   env.GetOrDefaultSecond(EnvSequenceInterval, dns01.DefaultPropagationTimeout),
		HTTPClient: &http.Client{
			Timeout: env.GetOrDefaultSecond(EnvHTTPTimeout, 30*time.Second),
		},
	}
}

// DNSProvider implements the challenge.Provider interface.
type DNSProvider struct {
	config *Config
	client *Client

	hostLocks sync.Map
}

// NewDNSProvider returns a DNSProvider instance configured for Hurricane Electric.
func NewDNSProvider() (*DNSProvider, error) {
	config := NewDefaultConfig()

	values, err := env.Get(EnvTokens)
	if err != nil {
		return nil, fmt.Errorf("hurricane: %w", err)
	}

	credentials, err := env.ParsePairs(values[EnvTokens])
	if err != nil {
		return nil, fmt.Errorf("hurricane: credentials: %w", err)
	}

	config.Credentials = credentials

	return NewDNSProviderConfig(config)
}

func NewDNSProviderConfig(config *Config) (*DNSProvider, error) {
	if config == nil {
		return nil, errors.New("hurricane: the configuration of the DNS provider is nil")
	}

	if len(config.Credentials) == 0 {
		return nil, errors.New("hurricane: credentials missing")
	}

	client := NewClient(config.Credentials)

	if config.HTTPClient != nil {
		client.HTTPClient = config.HTTPClient
	}

	// client.HTTPClient = clientdebug.Wrap(client.HTTPClient) // Removed dependency

	return &DNSProvider{config: config, client: client}, nil
}

// Present updates a TXT record to fulfill the dns-01 challenge.
func (d *DNSProvider) Present(domain, _, keyAuth string) error {
	info := dns01.GetChallengeInfo(domain, keyAuth)
	hostname := dns01.UnFqdn(info.EffectiveFQDN)

	unlock := d.lockHostname(hostname)
	defer unlock()

	err := d.client.UpdateTxtRecord(context.Background(), hostname, info.Value)
	if err != nil {
		return fmt.Errorf("hurricane: %w", err)
	}

	return nil
}

// CleanUp updates the TXT record matching the specified parameters.
func (d *DNSProvider) CleanUp(domain, _, keyAuth string) error {
	info := dns01.GetChallengeInfo(domain, keyAuth)
	hostname := dns01.UnFqdn(info.EffectiveFQDN)

	unlock := d.lockHostname(hostname)
	defer unlock()

	err := d.client.UpdateTxtRecord(context.Background(), hostname, ".")
	if err != nil {
		return fmt.Errorf("hurricane: %w", err)
	}

	return nil
}

// Timeout returns the timeout and interval to use when checking for DNS propagation.
// Adjusting here to cope with spikes in propagation times.
func (d *DNSProvider) Timeout() (timeout, interval time.Duration) {
	return d.config.PropagationTimeout, d.config.PollingInterval
}

// Sequential All DNS challenges for this provider will be resolved sequentially.
// Returns the interval between each iteration.
func (d *DNSProvider) Sequential() time.Duration {
	return d.config.SequenceInterval
}

func (d *DNSProvider) lockHostname(hostname string) func() {
	lock, _ := d.hostLocks.LoadOrStore(hostname, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()

	return mu.Unlock
}
