package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acme4/providers"
)

// Keep the fixture construction in one place while preserving the project's
// providers.Domain type in the test's Config value.
func makeTestConfig(certDir, accountDir string) Config {
	return Config{
		Email:      "admin@example.com",
		CertDir:    certDir,
		AccountDir: accountDir,
		Domains: []providers.Domain{{
			Names:       []string{"Example.COM", "*.Example.COM."},
			Provider:    "cloudflare",
			Credentials: map[string]string{"api_token": "token"},
		}},
	}
}

func TestValidateConfigNormalizesNamesAndResolvers(t *testing.T) {
	cfg := makeTestConfig(filepath.Join(t.TempDir(), "certs"), filepath.Join(t.TempDir(), "accounts"))
	cfg.DNSResolvers = []string{"127.0.0.1", "::1", "[2001:db8::1]:5353", "resolver.example:5300"}
	if err := validateConfig(&cfg); err != nil {
		t.Fatalf("validateConfig() error = %v", err)
	}
	if got, want := cfg.Domains[0].Names, []string{"example.com", "*.example.com"}; !equalStrings(got, want) {
		t.Fatalf("normalized names = %#v, want %#v", got, want)
	}
	wantResolvers := []string{"127.0.0.1:53", "[::1]:53", "[2001:db8::1]:5353", "resolver.example:5300"}
	if !equalStrings(cfg.DNSResolvers, wantResolvers) {
		t.Fatalf("normalized resolvers = %#v, want %#v", cfg.DNSResolvers, wantResolvers)
	}
}

func TestValidateConfigRejectsSemanticErrors(t *testing.T) {
	base := makeTestConfig(filepath.Join(t.TempDir(), "certs"), filepath.Join(t.TempDir(), "accounts"))
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"empty names", func(c *Config) { c.Domains[0].Names = nil }, "domains[0].names"},
		{"invalid path domain", func(c *Config) { c.Domains[0].Names = []string{"../example.com"} }, "path separator"},
		{"missing credential", func(c *Config) { c.Domains[0].Credentials = nil }, "credentials.api_token"},
		{"unknown provider", func(c *Config) { c.Domains[0].Provider = "does-not-exist" }, "unsupported provider"},
		{"negative renew window", func(c *Config) { c.RenewBefore = -1 }, "renew_before"},
		{"invalid resolver", func(c *Config) { c.DNSResolvers = []string{"127.0.0.1:nope"} }, "dns_resolvers"},
		{"mutually exclusive hooks", func(c *Config) {
			c.PostRenewHooks = []string{"reload"}
			c.Hooks = []HookConfig{{ID: "reload", Command: "true"}}
		}, "mutually exclusive"},
		{"invalid structured hook", func(c *Config) {
			c.Hooks = []HookConfig{{ID: "reload", Command: "true", Timeout: "0s"}}
		}, "hooks[0].timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			cfg.Domains = append([]providers.Domain(nil), base.Domains...)
			cfg.Domains[0].Credentials = map[string]string{"api_token": "token"}
			tt.mutate(&cfg)
			if err := validateConfig(&cfg); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateConfig() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateConfigRejectsOutputPathCollision(t *testing.T) {
	dir := t.TempDir()
	cfg := makeTestConfig(filepath.Join(dir, "certs"), filepath.Join(dir, "accounts"))
	cfg.Domains = append(cfg.Domains, providers.Domain{
		Names:       []string{"example.com"},
		Provider:    "cloudflare",
		Credentials: map[string]string{"api_token": "token"},
	})
	if err := validateConfig(&cfg); err == nil || !strings.Contains(err.Error(), "output path") {
		t.Fatalf("validateConfig() error = %v, want output path conflict", err)
	}
}

func TestValidateConfigRejectsOrderIndependentNameSetCollision(t *testing.T) {
	dir := t.TempDir()
	cfg := makeTestConfig(filepath.Join(dir, "certs"), filepath.Join(dir, "accounts"))
	cfg.Domains[0].Names = []string{"b.example.com", "a.example.com"}
	cfg.Domains = append(cfg.Domains, providers.Domain{
		Names:       []string{"a.example.com", "b.example.com"},
		Provider:    "cloudflare",
		Credentials: map[string]string{"api_token": "token"},
	})
	err := validateConfig(&cfg)
	if err == nil || !strings.Contains(err.Error(), "name set duplicates") {
		t.Fatalf("validateConfig() error = %v, want order-independent name set conflict", err)
	}
}

func TestLoadConfigStrictYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	contents := "email: admin@example.com\ncert_dir: " + filepath.Join(dir, "certs") + "\naccount_dir: " + filepath.Join(dir, "accounts") + "\nunknown_option: true\ndomains:\n  - names: [example.com]\n    provider: cloudflare\n    credentials:\n      api_token: token\n"
	if err := os.WriteFile(configPath, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(configPath); err == nil || !strings.Contains(err.Error(), "unknown_option") {
		t.Fatalf("loadConfig() error = %v, want unknown YAML field", err)
	}
}

func TestCheckConfigDoesNotCreateDirectories(t *testing.T) {
	dir := t.TempDir()
	certDir := filepath.Join(dir, "new-certs")
	accountDir := filepath.Join(dir, "new-accounts")
	configPath := filepath.Join(dir, "config.yaml")
	cfg := makeTestConfig(certDir, accountDir)
	contents := "email: " + cfg.Email + "\ncert_dir: " + certDir + "\naccount_dir: " + accountDir + "\ndomains:\n  - names: [example.com]\n    provider: cloudflare\n    credentials:\n      api_token: token\n"
	if err := os.WriteFile(configPath, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkConfig(configPath); err != nil {
		t.Fatalf("checkConfig() error = %v", err)
	}
	for _, path := range []string{certDir, accountDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("checkConfig created %s", path)
		}
	}
}

func TestLoadConfigCredentialsFile(t *testing.T) {
	dir := t.TempDir()
	credentialPath := filepath.Join(dir, "cloudflare.yaml")
	if err := os.WriteFile(credentialPath, []byte("api_token: secret-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.yaml")
	contents := "email: admin@example.com\ncert_dir: " + filepath.Join(dir, "certs") + "\naccount_dir: " + filepath.Join(dir, "accounts") + "\ndomains:\n  - names: [example.com]\n    provider: cloudflare\n    credentials_file: cloudflare.yaml\n"
	if err := os.WriteFile(configPath, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Domains[0].Credentials["api_token"] != "secret-value" {
		t.Fatal("credential file was not loaded")
	}
}

func TestLoadConfigRejectsCredentialFileWithBroadPermissions(t *testing.T) {
	dir := t.TempDir()
	credentialPath := filepath.Join(dir, "cloudflare.yaml")
	if err := os.WriteFile(credentialPath, []byte("api_token: do-not-log-me\n"), 0644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.yaml")
	contents := "email: admin@example.com\ncert_dir: " + filepath.Join(dir, "certs") + "\naccount_dir: " + filepath.Join(dir, "accounts") + "\ndomains:\n  - names: [example.com]\n    provider: cloudflare\n    credentials_file: cloudflare.yaml\n"
	if err := os.WriteFile(configPath, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := loadConfig(configPath)
	if err == nil || strings.Contains(err.Error(), "do-not-log-me") {
		t.Fatalf("unexpected credential file error: %v", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSampleConfigurationParsesAndValidates(t *testing.T) {
	cfg, err := loadConfig("config.sample.yaml")
	if err != nil {
		t.Fatalf("config.sample.yaml is invalid: %v", err)
	}
	if len(cfg.PostRenewHooks) != 0 || len(cfg.Hooks) != 0 {
		t.Fatal("config.sample.yaml must not execute deployment hooks by default")
	}
	if cfg.EmailNotification != nil && cfg.EmailNotification.Enabled {
		t.Fatal("config.sample.yaml must not send email with placeholder credentials by default")
	}
}
