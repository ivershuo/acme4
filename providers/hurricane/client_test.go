package hurricane

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestHTTPClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func TestUpdateTxtRecord_TokenLookup(t *testing.T) {
	tests := []struct {
		name        string
		hostname    string
		credentials map[string]string
		expectToken string
		expectError bool
	}{
		{
			name:     "Domain token match",
			hostname: "_acme-challenge.example.com",
			credentials: map[string]string{
				"example.com": "domain-token",
			},
			expectToken: "domain-token",
		},
		{
			name:     "Hostname token fallback",
			hostname: "_acme-challenge.example.com",
			credentials: map[string]string{
				"_acme-challenge.example.com": "specific-token",
			},
			expectToken: "specific-token",
		},
		{
			name:     "Domain token takes precedence",
			hostname: "_acme-challenge.example.com",
			credentials: map[string]string{
				"example.com":                 "domain-token",
				"_acme-challenge.example.com": "specific-token",
			},
			expectToken: "domain-token",
		},
		{
			name:     "No token found",
			hostname: "_acme-challenge.example.com",
			credentials: map[string]string{
				"other.com": "token",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.credentials)

			var mu sync.Mutex
			var gotToken string

			client.HTTPClient = newTestHTTPClient(func(r *http.Request) (*http.Response, error) {
				if err := r.ParseForm(); err != nil {
					t.Fatalf("parse form: %v", err)
				}

				mu.Lock()
				gotToken = r.Form.Get("password")
				mu.Unlock()

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("good")),
					Header:     make(http.Header),
				}, nil
			})

			err := client.UpdateTxtRecord(context.Background(), tt.hostname, "test-value")
			if (err != nil) != tt.expectError {
				t.Fatalf("UpdateTxtRecord() error = %v, expectError %v", err, tt.expectError)
			}

			if tt.expectError {
				return
			}

			mu.Lock()
			defer mu.Unlock()
			if gotToken != tt.expectToken {
				t.Fatalf("expected token %q, got %q", tt.expectToken, gotToken)
			}
		})
	}
}
