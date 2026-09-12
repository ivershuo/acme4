package main

import (
	"crypto/x509"
	"testing"
	"time"
)

func TestParseRemoteEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		host    string
		port    string
		address string
		sni     string
	}{
		{name: "domain default port", input: "example.com", host: "example.com", port: "443", address: "example.com:443", sni: "example.com"},
		{name: "domain custom port", input: "example.com:8443", host: "example.com", port: "8443", address: "example.com:8443", sni: "example.com"},
		{name: "ipv4 default port", input: "192.0.2.10", host: "192.0.2.10", port: "443", address: "192.0.2.10:443"},
		{name: "ipv4 custom port", input: "192.0.2.10:9443", host: "192.0.2.10", port: "9443", address: "192.0.2.10:9443"},
		{name: "bare ipv6", input: "2001:db8::10", host: "2001:db8::10", port: "443", address: "[2001:db8::10]:443"},
		{name: "bracketed ipv6 default port", input: "[2001:db8::10]", host: "2001:db8::10", port: "443", address: "[2001:db8::10]:443"},
		{name: "bracketed ipv6 custom port", input: "[2001:db8::10]:9443", host: "2001:db8::10", port: "9443", address: "[2001:db8::10]:9443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRemoteEndpoint(tt.input)
			if err != nil {
				t.Fatalf("parseRemoteEndpoint(%q) error = %v", tt.input, err)
			}
			if got.Host != tt.host || got.Port != tt.port || got.Address != tt.address || got.SNI != tt.sni {
				t.Fatalf("parseRemoteEndpoint(%q) = %#v, want host=%q port=%q address=%q sni=%q", tt.input, got, tt.host, tt.port, tt.address, tt.sni)
			}
		})
	}
}

func TestParseRemoteEndpointRejectsAmbiguousOrInvalidInput(t *testing.T) {
	for _, input := range []string{
		"",
		"[2001:db8::1",
		"2001:db8::zz:8443", // malformed IPv6 cannot carry an unbracketed port
		"[2001:db8::1]:0",
		"example.com:65536",
		"example.com:not-a-port",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := parseRemoteEndpoint(input); err == nil {
				t.Fatalf("parseRemoteEndpoint(%q) unexpectedly succeeded", input)
			}
		})
	}
}

func TestCertificateCurrentlyValid(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct {
		name                string
		notBefore, notAfter int
		want                bool
	}{
		{name: "valid", notBefore: -1, notAfter: 1, want: true},
		{name: "not yet valid", notBefore: 1, notAfter: 2, want: false},
		{name: "expired", notBefore: -2, notAfter: -1, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cert := &x509.Certificate{NotBefore: now.Add(time.Duration(tt.notBefore) * time.Hour), NotAfter: now.Add(time.Duration(tt.notAfter) * time.Hour)}
			if got := certificateCurrentlyValid(cert, now); got != tt.want {
				t.Fatalf("certificateCurrentlyValid() = %v, want %v", got, tt.want)
			}
		})
	}
}
