package main

import (
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"acme4/providers"
)

var dnsLabelPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)

// validateConfig performs all checks which can be done without contacting a
// CA, DNS service, or creating any files. It intentionally leaves directory
// creation to run so -check-config remains read-only.
func validateConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("configuration is nil")
	}
	var problems []string
	add := func(format string, args ...interface{}) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if strings.TrimSpace(cfg.Email) == "" {
		add("email: is required")
	} else if parsed, err := mail.ParseAddress(strings.TrimSpace(cfg.Email)); err != nil || parsed.Address != strings.TrimSpace(cfg.Email) {
		add("email: must be a valid email address")
	}

	certDir := strings.TrimSpace(cfg.CertDir)
	accountDir := strings.TrimSpace(cfg.AccountDir)
	if certDir == "" {
		add("cert_dir: is required")
	}
	if accountDir == "" {
		add("account_dir: is required")
	}
	if certDir != "" {
		validateDirectory(add, "cert_dir", certDir)
	}
	if accountDir != "" {
		validateDirectory(add, "account_dir", accountDir)
	}
	if certDir != "" && accountDir != "" {
		certAbs, certErr := filepath.Abs(filepath.Clean(certDir))
		accountAbs, accountErr := filepath.Abs(filepath.Clean(accountDir))
		if certErr == nil && accountErr == nil && samePath(certAbs, accountAbs) {
			add("cert_dir and account_dir: must be different directories")
		}
	}

	if cfg.RenewBefore < 0 {
		add("renew_before: must be zero (default) or a positive number of days")
	}
	if cfg.RenewBefore > 3650 {
		add("renew_before: must not exceed 3650 days")
	}
	if cfg.ACMEDirectoryURL != "" {
		u, err := url.ParseRequestURI(strings.TrimSpace(cfg.ACMEDirectoryURL))
		if err != nil || u.Scheme == "" || u.Host == "" {
			add("acme_directory_url: must be an absolute URL with a host")
		}
	}

	for i, resolver := range cfg.DNSResolvers {
		if strings.TrimSpace(resolver) == "" {
			add("dns_resolvers[%d]: must not be empty", i)
		}
	}
	if normalized, err := normalizeResolvers(cfg.DNSResolvers); err != nil {
		add("dns_resolvers: %v", err)
	} else {
		// Keep the normalized form for the actual lego configuration. This is
		// still a read-only operation with respect to the filesystem.
		cfg.DNSResolvers = normalized
	}

	if len(cfg.Domains) == 0 {
		add("domains: must contain at least one certificate entry")
	}
	seenOutputs := make(map[string]int)
	seenNameSets := make(map[string]int)
	for i := range cfg.Domains {
		domain := &cfg.Domains[i]
		prefix := fmt.Sprintf("domains[%d]", i)
		if len(domain.Names) == 0 {
			add("%s.names: must contain at least one domain", prefix)
		} else {
			seenNames := make(map[string]struct{}, len(domain.Names))
			for j, name := range domain.Names {
				normalized, err := normalizeDomainName(name)
				if err != nil {
					add("%s.names[%d]: %v", prefix, j, err)
					continue
				}
				if _, exists := seenNames[normalized]; exists {
					add("%s.names[%d]: duplicate domain %q", prefix, j, normalized)
					continue
				}
				seenNames[normalized] = struct{}{}
				domain.Names[j] = normalized
			}
			if len(seenNames) == len(domain.Names) {
				nameSet := canonicalNameSet(domain.Names)
				if previous, exists := seenNameSets[nameSet]; exists {
					add("%s.names: normalized name set duplicates domains[%d].names regardless of order", prefix, previous)
				} else {
					seenNameSets[nameSet] = i
				}
			}
		}
		providerName := strings.ToLower(strings.TrimSpace(domain.Provider))
		if providerName == "" {
			add("%s.provider: is required", prefix)
		} else if !providers.IsSupported(providerName) {
			add("%s.provider: unsupported provider %q for names %v", prefix, domain.Provider, domain.Names)
		} else {
			domain.Provider = providerName
			for key, value := range domain.Credentials {
				domain.Credentials[key] = strings.TrimSpace(value)
			}
			for _, problem := range validateProviderCredentials(providerName, domain.Credentials) {
				add("%s: %s", prefix, problem)
			}
		}

		if len(domain.Names) > 0 && certDir != "" {
			first, err := normalizeDomainName(domain.Names[0])
			if err == nil {
				certAbs, absErr := filepath.Abs(filepath.Clean(certDir))
				if absErr == nil {
					output := filepath.Clean(filepath.Join(certAbs, first+".crt"))
					if previous, exists := seenOutputs[output]; exists {
						add("%s.names[0]: output path %q conflicts with domains[%d].names[0]", prefix, output, previous)
					} else {
						seenOutputs[output] = i
					}
				}
			}
		}
	}

	if cfg.EmailNotification != nil && cfg.EmailNotification.Enabled {
		notify := cfg.EmailNotification
		if strings.TrimSpace(notify.ResendAPIKey) == "" {
			add("email_notification.resend_api_key: is required when notifications are enabled")
		}
		if strings.TrimSpace(notify.FromEmail) == "" {
			add("email_notification.from_email: is required when notifications are enabled")
		} else if parsed, err := mail.ParseAddress(strings.TrimSpace(notify.FromEmail)); err != nil || parsed.Address != strings.TrimSpace(notify.FromEmail) {
			add("email_notification.from_email: must be a valid email address")
		}
		if len(notify.ToEmails) == 0 {
			add("email_notification.to_emails: must contain at least one recipient when notifications are enabled")
		}
		for i, recipient := range notify.ToEmails {
			recipient = strings.TrimSpace(recipient)
			if parsed, err := mail.ParseAddress(recipient); err != nil || parsed.Address != recipient {
				add("email_notification.to_emails[%d]: must be a valid email address", i)
			}
			notify.ToEmails[i] = recipient
		}
	}

	if len(cfg.PostRenewHooks) > 0 {
		for i, hook := range cfg.PostRenewHooks {
			if strings.TrimSpace(hook) == "" {
				add("post_renew_hooks[%d]: must not be empty", i)
			}
		}
	}
	if len(cfg.PostRenewHooks) > 0 && len(cfg.Hooks) > 0 {
		add("post_renew_hooks and hooks: are mutually exclusive")
	}
	seenHookIDs := make(map[string]struct{}, len(cfg.Hooks))
	for i := range cfg.Hooks {
		hook := &cfg.Hooks[i]
		prefix := fmt.Sprintf("hooks[%d]", i)
		hook.ID = strings.TrimSpace(hook.ID)
		hook.Command = strings.TrimSpace(hook.Command)
		if hook.ID == "" {
			add("%s.id: is required", prefix)
		} else if _, exists := seenHookIDs[hook.ID]; exists {
			add("%s.id: duplicate hook id %q", prefix, hook.ID)
		} else {
			seenHookIDs[hook.ID] = struct{}{}
		}
		if hook.Command == "" {
			add("%s.command: is required", prefix)
		} else if _, err := replaceHookValue(hook.Command, "example.com", "/cert", "/key"); err != nil {
			add("%s.command: %v", prefix, err)
		}
		for argIndex, arg := range hook.Args {
			if _, err := replaceHookValue(arg, "example.com", "/cert", "/key"); err != nil {
				add("%s.args[%d]: %v", prefix, argIndex, err)
			}
		}
		if hook.Timeout != "" {
			if timeout, err := time.ParseDuration(hook.Timeout); err != nil || timeout <= 0 {
				add("%s.timeout: must be a positive duration", prefix)
			}
		}
		if hook.EnvFile != "" {
			if _, err := hookEnvironment(hook.EnvFile); err != nil {
				add("%s.env_file: %v", prefix, err)
			}
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("configuration validation failed: %s", strings.Join(problems, "; "))
	}
	return nil
}

func validateDirectory(add func(string, ...interface{}), field, path string) {
	if strings.ContainsRune(path, '\x00') {
		add("%s: contains an invalid NUL character", field)
		return
	}
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		add("%s: path exists but is not a directory", field)
	} else if err != nil && !os.IsNotExist(err) {
		add("%s: cannot inspect path: %v", field, err)
	}
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	if aInfo, err := os.Stat(a); err == nil {
		if bInfo, err := os.Stat(b); err == nil {
			return os.SameFile(aInfo, bInfo)
		}
	}
	return false
}

func normalizeDomainName(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, ".")
	if value == "" {
		return "", fmt.Errorf("must not be empty")
	}
	if strings.ContainsAny(value, `/\\`) || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("contains a path separator or NUL")
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return "", fmt.Errorf("contains whitespace")
	}
	wildcard := strings.HasPrefix(value, "*.")
	if wildcard {
		value = strings.TrimPrefix(value, "*.")
		if value == "" || strings.Contains(value, "*") {
			return "", fmt.Errorf("wildcard must be exactly the left-most label")
		}
	} else if strings.Contains(value, "*") {
		return "", fmt.Errorf("wildcard must be exactly the left-most label")
	}
	if len(value) > 253 {
		return "", fmt.Errorf("is longer than 253 characters")
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || !dnsLabelPattern.MatchString(label) {
			return "", fmt.Errorf("contains invalid DNS label %q", label)
		}
	}
	if wildcard {
		return "*." + value, nil
	}
	return value, nil
}

func validateProviderCredentials(provider string, credentials map[string]string) []string {
	required := map[string][]string{
		"cloudflare":   {"api_token"},
		"hurricane":    {"api_key"},
		"porkbun":      {"api_key", "secret_api_key"},
		"tencentcloud": {"secret_id", "secret_key"},
	}
	var problems []string
	for _, key := range required[provider] {
		if strings.TrimSpace(credentials[key]) == "" {
			problems = append(problems, fmt.Sprintf("credentials.%s: is required", key))
		}
	}
	if provider == "hurricane" && strings.TrimSpace(credentials["api_key"]) != "" {
		if err := validateHurricaneCredentialPairs(credentials["api_key"]); err != nil {
			problems = append(problems, "credentials.api_key: "+err.Error())
		}
	}
	return problems
}

func validateHurricaneCredentialPairs(raw string) error {
	pairs := strings.Split(strings.TrimSuffix(strings.TrimSpace(raw), ","), ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		colonAt, equalsAt := strings.IndexByte(pair, ':'), strings.IndexByte(pair, '=')
		separatorAt := colonAt
		if separatorAt < 0 || (equalsAt >= 0 && equalsAt < separatorAt) {
			separatorAt = equalsAt
		}
		if separatorAt <= 0 || separatorAt == len(pair)-1 || strings.TrimSpace(pair[:separatorAt]) == "" || strings.TrimSpace(pair[separatorAt+1:]) == "" {
			return fmt.Errorf("invalid token mapping %q; expected key:token", pair)
		}
	}
	return nil
}
