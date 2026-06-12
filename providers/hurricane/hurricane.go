package hurricane

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	EnvIntervalRetries    = envNamespace + "INTERVAL_RETRIES"
	EnvIntervalRetryWait  = envNamespace + "INTERVAL_RETRY_WAIT"
)

const (
	defaultPropagationTimeout = 300 * time.Second
	defaultSequenceInterval   = 120 * time.Second
	defaultIntervalRetries    = 3
	defaultIntervalRetryWait  = 30 * time.Second
)

var _ challenge.ProviderTimeout = (*DNSProvider)(nil)

// Config is used to configure the creation of the DNSProvider.
type Config struct {
	Credentials        map[string]string
	PropagationTimeout time.Duration
	PollingInterval    time.Duration
	SequenceInterval   time.Duration
	IntervalRetries    int
	IntervalRetryWait  time.Duration
	HTTPClient         *http.Client
}

// NewDefaultConfig returns a default configuration for the DNSProvider.
func NewDefaultConfig() *Config {
	return &Config{
		PropagationTimeout: env.GetOrDefaultSecond(EnvPropagationTimeout, defaultPropagationTimeout),
		PollingInterval:    env.GetOrDefaultSecond(EnvPollingInterval, dns01.DefaultPollingInterval),
		SequenceInterval:   env.GetOrDefaultSecond(EnvSequenceInterval, defaultSequenceInterval),
		IntervalRetries:    env.GetOrDefaultInt(EnvIntervalRetries, defaultIntervalRetries),
		IntervalRetryWait:  env.GetOrDefaultSecond(EnvIntervalRetryWait, defaultIntervalRetryWait),
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

	if config.IntervalRetries < 0 {
		config.IntervalRetries = 0
	}

	if config.IntervalRetryWait < 0 {
		config.IntervalRetryWait = 0
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

	start := time.Now()
	log.Printf("[Hurricane Electric] action=present domain=%s fqdn=%s value=%s", domain, info.EffectiveFQDN, info.Value)
	err := d.updateTxtRecord(context.Background(), "present", domain, info.EffectiveFQDN, hostname, info.Value)
	if err != nil {
		log.Printf("[Hurricane Electric] action=present domain=%s fqdn=%s duration=%s result=error diagnostic=%s error=%v", domain, info.EffectiveFQDN, time.Since(start), DiagnosticCodeFromError(err), err)
		return fmt.Errorf("hurricane: %w", err)
	}

	log.Printf("[Hurricane Electric] action=present domain=%s fqdn=%s duration=%s result=ok", domain, info.EffectiveFQDN, time.Since(start))
	return nil
}

// CleanUp updates the TXT record matching the specified parameters.
func (d *DNSProvider) CleanUp(domain, _, keyAuth string) error {
	info := dns01.GetChallengeInfo(domain, keyAuth)
	hostname := dns01.UnFqdn(info.EffectiveFQDN)

	unlock := d.lockHostname(hostname)
	defer unlock()

	start := time.Now()
	log.Printf("[Hurricane Electric] action=cleanup domain=%s fqdn=%s value=.", domain, info.EffectiveFQDN)
	err := d.updateTxtRecord(context.Background(), "cleanup", domain, info.EffectiveFQDN, hostname, ".")
	if err != nil {
		log.Printf("[Hurricane Electric] action=cleanup domain=%s fqdn=%s duration=%s result=error diagnostic=%s error=%v", domain, info.EffectiveFQDN, time.Since(start), DiagnosticCodeFromError(err), err)
		return fmt.Errorf("hurricane: %w", err)
	}

	log.Printf("[Hurricane Electric] action=cleanup domain=%s fqdn=%s duration=%s result=ok", domain, info.EffectiveFQDN, time.Since(start))
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

func (d *DNSProvider) updateTxtRecord(ctx context.Context, action, domain, fqdn, hostname, value string) error {
	var err error
	for attempt := 0; attempt <= d.config.IntervalRetries; attempt++ {
		if attempt > 0 {
			wait := d.intervalRetryWait(attempt)
			log.Printf("[Hurricane Electric] action=%s domain=%s fqdn=%s retry=%d wait=%s reason=interval", action, domain, fqdn, attempt, wait)
			if sleepErr := sleepContext(ctx, wait); sleepErr != nil {
				return sleepErr
			}
		}

		err = d.client.UpdateTxtRecord(ctx, hostname, value)
		if DiagnosticCodeFromError(err) != DiagnosticInterval {
			return err
		}
	}

	return err
}

func (d *DNSProvider) intervalRetryWait(attempt int) time.Duration {
	wait := d.config.IntervalRetryWait
	for i := 1; i < attempt; i++ {
		wait *= 2
	}
	return wait
}

func sleepContext(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func DiagnosticCodeFromError(err error) DiagnosticCode {
	var diagnostic *DiagnosticError
	if errors.As(err, &diagnostic) {
		return diagnostic.Code
	}
	return ""
}
