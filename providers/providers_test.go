package providers

import (
	"testing"
	"time"

	"github.com/go-acme/lego/v4/challenge"
)

var _ challenge.Provider = (*providerStub)(nil)
var _ challenge.ProviderTimeout = (*providerStub)(nil)

type providerStub struct {
	presentCalls int
	cleanupCalls int
}

func (p *providerStub) Present(domain, token, keyAuth string) error {
	p.presentCalls++
	return nil
}

func (p *providerStub) CleanUp(domain, token, keyAuth string) error {
	p.cleanupCalls++
	return nil
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
