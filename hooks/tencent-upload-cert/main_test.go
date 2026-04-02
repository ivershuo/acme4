package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigExplicitPaths(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--cert", "/tmp/example.com.crt",
		"--key", "/tmp/example.com.key",
		"--secret-id", "sid",
		"--secret-key", "skey",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}

	if cfg.CertPath != "/tmp/example.com.crt" || cfg.KeyPath != "/tmp/example.com.key" {
		t.Fatalf("unexpected explicit paths: %+v", cfg)
	}
	if cfg.Alias != "example.com.crt" {
		t.Fatalf("unexpected alias: %q", cfg.Alias)
	}
}

func TestParseConfigDomainMode(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--domain", "example.com",
		"--cert-dir", "/tmp/certs",
		"--secret-id", "sid",
		"--secret-key", "skey",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}

	if cfg.Domain != "example.com" || cfg.CertDir != "/tmp/certs" {
		t.Fatalf("unexpected domain mode config: %+v", cfg)
	}
	if cfg.Alias != "example.com" {
		t.Fatalf("unexpected alias: %q", cfg.Alias)
	}
}

func TestParseConfigRejectsUnknownCombination(t *testing.T) {
	_, err := parseConfig([]string{
		"--cert", "/tmp/example.com.crt",
		"--key", "/tmp/example.com.key",
		"--domain", "example.com",
		"--cert-dir", "/tmp/certs",
		"--secret-id", "sid",
		"--secret-key", "skey",
	})
	if err == nil {
		t.Fatal("parseConfig() expected error for mixed input modes")
	}
}

func TestResolveCertificateMaterialWithDomain(t *testing.T) {
	certPEM, keyPEM := mustCreateTestKeyPair(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "example.com.crt"), []byte(certPEM), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "example.com.key"), []byte(keyPEM), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	gotCert, gotKey, err := resolveCertificateMaterial(Config{
		Domain:  "example.com",
		CertDir: dir,
	})
	if err != nil {
		t.Fatalf("resolveCertificateMaterial() error = %v", err)
	}

	if gotCert != certPEM || gotKey != keyPEM {
		t.Fatal("resolved certificate material does not match expected files")
	}
}
