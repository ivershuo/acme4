package providers

import (
	"bytes"
	"errors"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/challenge"
)

var _ challenge.Provider = (*providerStub)(nil)
var _ challenge.ProviderTimeout = (*providerStub)(nil)

type providerStub struct {
	presentCalls int
	cleanupCalls int
	cleanupErr   error
}

type recordProviderStub struct{ records map[string]bool }

func (p *recordProviderStub) Present(_, token, _ string) error {
	p.records[token] = true
	return nil
}

func (p *recordProviderStub) CleanUp(_, token, _ string) error {
	delete(p.records, token)
	return nil
}

func TestParseCredentialPairsSupportsCurrentAndLegacySeparators(t *testing.T) {
	got, err := parseCredentialPairs("example.com:domain-token,_acme-challenge.example.com=record-token")
	if err != nil {
		t.Fatalf("parseCredentialPairs() error = %v", err)
	}
	if got["example.com"] != "domain-token" || got["_acme-challenge.example.com"] != "record-token" {
		t.Fatalf("parseCredentialPairs() = %#v", got)
	}
}

func TestCloudflareFactoryDoesNotMutateCredentialEnvironment(t *testing.T) {
	t.Setenv("CLOUDFLARE_DNS_API_TOKEN", "environment-token")
	_, err := newCloudflareProvider(Domain{Credentials: map[string]string{"api_token": "configured-token"}})
	if err != nil {
		t.Fatalf("newCloudflareProvider() error = %v", err)
	}
	if got := os.Getenv("CLOUDFLARE_DNS_API_TOKEN"); got != "environment-token" {
		t.Fatalf("environment credential changed to %q", got)
	}
}

func TestCloudflareFactoryRequiresToken(t *testing.T) {
	if _, err := newCloudflareProvider(Domain{}); err == nil {
		t.Fatal("newCloudflareProvider() expected missing token error")
	}
}

func TestProviderFactoriesDoNotMutateCredentialEnvironment(t *testing.T) {
	t.Setenv("TENCENTCLOUD_SECRET_ID", "original-id")
	t.Setenv("TENCENTCLOUD_SECRET_KEY", "original-key")
	if _, err := newTencentcloudProvider(Domain{Credentials: map[string]string{"secret_id": "configured-id", "secret_key": "configured-key"}}); err != nil {
		t.Fatalf("newTencentcloudProvider() error = %v", err)
	}
	if os.Getenv("TENCENTCLOUD_SECRET_ID") != "original-id" || os.Getenv("TENCENTCLOUD_SECRET_KEY") != "original-key" {
		t.Fatal("TencentCloud factory changed credential environment")
	}

	t.Setenv("PORKBUN_API_KEY", "original-api")
	t.Setenv("PORKBUN_SECRET_API_KEY", "original-secret")
	if _, err := newPorkbunProvider(Domain{Credentials: map[string]string{"api_key": "configured-api", "secret_api_key": "configured-secret"}}); err != nil {
		t.Fatalf("newPorkbunProvider() error = %v", err)
	}
	if os.Getenv("PORKBUN_API_KEY") != "original-api" || os.Getenv("PORKBUN_SECRET_API_KEY") != "original-secret" {
		t.Fatal("Porkbun factory changed credential environment")
	}
}

func TestProviderFactoriesInitializeConcurrentlyWithoutCredentialCrosstalk(t *testing.T) {
	domains := []Domain{
		{Provider: "cloudflare", Credentials: map[string]string{"api_token": "cf-one"}},
		{Provider: "cloudflare", Credentials: map[string]string{"api_token": "cf-two"}},
		{Provider: "hurricane", Credentials: map[string]string{"api_key": "example.com:he-one"}},
		{Provider: "hurricane", Credentials: map[string]string{"api_key": "example.net:he-two"}},
		{Provider: "porkbun", Credentials: map[string]string{"api_key": "pb-one", "secret_api_key": "pbs-one"}},
		{Provider: "tencentcloud", Credentials: map[string]string{"secret_id": "tc-one", "secret_key": "tcs-one"}},
	}
	var wg sync.WaitGroup
	errorsCh := make(chan error, len(domains))
	for _, domain := range domains {
		domain := domain
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := providerRegistry[domain.Provider](domain)
			errorsCh <- err
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent provider initialization: %v", err)
		}
	}
}

func (p *providerStub) Present(domain, token, keyAuth string) error {
	p.presentCalls++
	return nil
}

func (p *providerStub) CleanUp(domain, token, keyAuth string) error {
	p.cleanupCalls++
	return p.cleanupErr
}

func TestLoggingDNSProviderLabelsCleanupWarnings(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	want := errors.New("delete failed")
	provider := &LoggingDNSProvider{wrapped: &providerStub{cleanupErr: want}}
	if err := provider.CleanUp("example.com", "token", "key"); !errors.Is(err, want) {
		t.Fatalf("cleanup error = %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("[清理警告]")) {
		t.Fatalf("cleanup warning not labeled: %s", output.String())
	}
}

func TestLoggingProviderPreservesPerChallengeCleanupIdentity(t *testing.T) {
	t.Setenv("LEGO_DISABLE_CNAME_SUPPORT", "true")
	wrapped := &recordProviderStub{records: map[string]bool{}}
	provider := &LoggingDNSProvider{wrapped: wrapped}
	if err := provider.Present("example.com", "root-token", "root-key"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Present("example.com", "wildcard-token", "wildcard-key"); err != nil {
		t.Fatal(err)
	}
	if err := provider.CleanUp("example.com", "root-token", "root-key"); err != nil {
		t.Fatal(err)
	}
	if wrapped.records["root-token"] || !wrapped.records["wildcard-token"] {
		t.Fatalf("cleanup removed wrong TXT identity: %#v", wrapped.records)
	}
}

func (p *providerStub) Timeout() (time.Duration, time.Duration) {
	return 5 * time.Minute, 7 * time.Second
}

func (p *providerStub) Sequential() time.Duration {
	return 11 * time.Second
}

func TestLoggingDNSProviderDelegatesWrappedProvider(t *testing.T) {
	wrapped := &providerStub{}
	provider := &LoggingDNSProvider{wrapped: wrapped}

	if err := provider.Present("example.com", "", "key-auth"); err != nil {
		t.Fatalf("Present() error: %v", err)
	}
	if err := provider.CleanUp("example.com", "", "key-auth"); err != nil {
		t.Fatalf("CleanUp() error: %v", err)
	}

	if wrapped.presentCalls != 1 {
		t.Fatalf("expected 1 Present call, got %d", wrapped.presentCalls)
	}
	if wrapped.cleanupCalls != 1 {
		t.Fatalf("expected 1 CleanUp call, got %d", wrapped.cleanupCalls)
	}
}

func TestLoggingDNSProviderPreservesOptionalCapabilities(t *testing.T) {
	wrapped := &providerStub{}
	provider := &loggingDNSProviderTimeoutSequential{
		LoggingDNSProvider: &LoggingDNSProvider{wrapped: wrapped},
		timeoutProvider:    wrapped,
		sequentialProvider: wrapped,
	}

	timeoutProvider, ok := any(provider).(challenge.ProviderTimeout)
	if !ok {
		t.Fatal("wrapped logging provider should implement challenge.ProviderTimeout")
	}

	timeout, interval := timeoutProvider.Timeout()
	if timeout != 5*time.Minute || interval != 7*time.Second {
		t.Fatalf("unexpected timeout values: timeout=%s interval=%s", timeout, interval)
	}

	seqProvider, ok := any(provider).(interface{ Sequential() time.Duration })
	if !ok {
		t.Fatal("wrapped logging provider should expose Sequential()")
	}

	if got := seqProvider.Sequential(); got != 11*time.Second {
		t.Fatalf("unexpected sequential interval: %s", got)
	}
}
