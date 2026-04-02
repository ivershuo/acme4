package hurricane

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/challenge/dns01"
)

func TestDNSProviderSerializesUpdatesForSameHostname(t *testing.T) {
	started := make(chan string, 2)
	releaseFirst := make(chan struct{})
	finishedFirst := make(chan struct{})

	var mu sync.Mutex
	var inFlight int
	maxInFlight := 0

	client := newTestHTTPClient(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}

		hostname := r.Form.Get("hostname")

		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()

		started <- hostname

		if hostname != "_acme-challenge.example.com" {
			t.Fatalf("unexpected hostname %q", hostname)
		}

		select {
		case <-releaseFirst:
		default:
			<-releaseFirst
			close(finishedFirst)
		}

		mu.Lock()
		inFlight--
		mu.Unlock()

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("good")),
			Header:     make(http.Header),
		}, nil
	})

	provider, err := NewDNSProviderConfig(&Config{
		Credentials:      map[string]string{"example.com": "token"},
		HTTPClient:       client,
		SequenceInterval: time.Second,
	})
	if err != nil {
		t.Fatalf("NewDNSProviderConfig(): %v", err)
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- provider.Present("example.com", "", "key-auth-1")
	}()

	firstHostname := <-started
	if firstHostname != "_acme-challenge.example.com" {
		t.Fatalf("unexpected first hostname %q", firstHostname)
	}

	go func() {
		errCh <- provider.Present("example.com", "", "key-auth-2")
	}()

	select {
	case hostname := <-started:
		t.Fatalf("second update reached transport before first finished: %s", hostname)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseFirst)
	<-finishedFirst

	secondHostname := <-started
	if secondHostname != "_acme-challenge.example.com" {
		t.Fatalf("unexpected second hostname %q", secondHostname)
	}

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("Present() error: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if maxInFlight != 1 {
		t.Fatalf("expected at most one in-flight update, got %d", maxInFlight)
	}
}

func TestDNSProviderSameHostnameLifecycleOrder(t *testing.T) {
	var mu sync.Mutex
	var got []string

	client := newTestHTTPClient(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}

		mu.Lock()
		got = append(got, r.Form.Get("hostname")+"="+r.Form.Get("txt"))
		mu.Unlock()

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("good")),
			Header:     make(http.Header),
		}, nil
	})

	provider, err := NewDNSProviderConfig(&Config{
		Credentials:      map[string]string{"example.com": "token"},
		HTTPClient:       client,
		SequenceInterval: time.Second,
	})
	if err != nil {
		t.Fatalf("NewDNSProviderConfig(): %v", err)
	}

	info1 := dns01.GetChallengeInfo("example.com", "key-auth-1")
	info2 := dns01.GetChallengeInfo("example.com", "key-auth-2")

	if err := provider.Present("example.com", "", "key-auth-1"); err != nil {
		t.Fatalf("Present(example.com): %v", err)
	}
	if err := provider.CleanUp("example.com", "", "key-auth-1"); err != nil {
		t.Fatalf("CleanUp(example.com): %v", err)
	}
	if err := provider.Present("example.com", "", "key-auth-2"); err != nil {
		t.Fatalf("Present(example.com key-auth-2): %v", err)
	}
	if err := provider.CleanUp("example.com", "", "key-auth-2"); err != nil {
		t.Fatalf("CleanUp(example.com key-auth-2): %v", err)
	}

	want := []string{
		"_acme-challenge.example.com=" + info1.Value,
		"_acme-challenge.example.com=.",
		"_acme-challenge.example.com=" + info2.Value,
		"_acme-challenge.example.com=.",
	}

	mu.Lock()
	defer mu.Unlock()

	if len(got) != len(want) {
		t.Fatalf("expected %d updates, got %d: %v", len(want), len(got), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("update %d mismatch: got %q want %q", i, got[i], want[i])
		}
	}
}
