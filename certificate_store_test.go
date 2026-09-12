package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPendingDeploymentRetriesOnlyFailedHooks(t *testing.T) {
	dir := t.TempDir()
	cert, key := testCertificatePair(t, "example.com")
	if err := savePendingCertificate(dir, []string{"example.com"}, cert, key); err != nil {
		t.Fatal(err)
	}
	countPath, gatePath := filepath.Join(dir, "count"), filepath.Join(dir, "gate")
	hooks := []string{"printf x >> " + countPath, "test -f " + gatePath}
	pending, _, err := deployPendingCertificate(dir, []string{"example.com"}, hooks, nil)
	if !pending || err == nil {
		t.Fatalf("first deploy pending=%t err=%v", pending, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "example.com.crt")); err != nil {
		t.Fatal("compatibility certificate was not published before reload-style hooks")
	}
	if err := os.WriteFile(gatePath, []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	pending, _, err = deployPendingCertificate(dir, []string{"example.com"}, hooks, nil)
	if !pending || err != nil {
		t.Fatalf("retry pending=%t err=%v", pending, err)
	}
	count, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(count) != "x" {
		t.Fatalf("successful hook was repeated: count=%q", count)
	}
	if _, err := os.Stat(filepath.Join(dir, "example.com.crt")); err != nil {
		t.Fatalf("compatibility certificate not published: %v", err)
	}
}

func TestStructuredHookPreservesArgumentBoundaries(t *testing.T) {
	hook := HookConfig{
		ID:      "argument-test",
		Command: "/bin/sh",
		Args:    []string{"-c", `test "$1" = 'a b;$HOME'`, "--", "a b;$HOME"},
		Timeout: "2s",
	}
	if err := runStructuredHook(hook, "example.com", "/tmp/cert with space", "/tmp/key"); err != nil {
		t.Fatal(err)
	}
}

func TestStructuredHookTimeoutAndOutputLimit(t *testing.T) {
	err := runStructuredHook(HookConfig{ID: "timeout", Command: "/bin/sh", Args: []string{"-c", "sleep 1"}, Timeout: "20ms"}, "example.com", "cert", "key")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error=%v", err)
	}
	buffer := &limitedBuffer{remaining: 4}
	_, _ = buffer.Write([]byte("123456"))
	if got := buffer.String(); got != "1234\n[输出已截断]" {
		t.Fatalf("limited output=%q", got)
	}
}

func TestStructuredHookRedactsEnvironmentFileValues(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "hook.env")
	if err := os.WriteFile(envPath, []byte("SECRET_VALUE=super-secret-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := runStructuredHook(HookConfig{ID: "redaction", Command: "/bin/sh", Args: []string{"-c", "echo $SECRET_VALUE; exit 1"}, EnvFile: envPath}, "example.com", "cert", "key")
	if err == nil || strings.Contains(err.Error(), "super-secret-value") || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("secret was not redacted: %v", err)
	}
}

func TestHookEnvironmentUsesLiteralSimplifiedValues(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "hook.env")
	if err := os.WriteFile(envPath, []byte("QUOTED=\"literal\"\nLEADING= value \n"), 0600); err != nil {
		t.Fatal(err)
	}
	values, err := hookEnvironment(envPath)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(values, "\n")
	if !strings.Contains(joined, "QUOTED=\"literal\"") {
		t.Fatalf("dotenv quotes should remain literal: %q", joined)
	}
	if !strings.Contains(joined, "LEADING= value") {
		t.Fatalf("leading value whitespace should remain after line trimming: %q", joined)
	}
}

func TestHookConfigurationChangeRedeploysCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	cert, key := testCertificatePair(t, "example.com")
	if err := savePendingCertificate(dir, []string{"example.com"}, cert, key); err != nil {
		t.Fatal(err)
	}
	if _, _, err := deployPendingCertificate(dir, []string{"example.com"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "configured-hook-ran")
	hook := HookConfig{ID: "new-deploy-step", Command: "/usr/bin/touch", Args: []string{marker}}
	pending, _, err := deployPendingCertificate(dir, []string{"example.com"}, nil, []HookConfig{hook})
	if !pending || err != nil {
		t.Fatalf("configuration-change deploy pending=%t err=%v", pending, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("new hook did not run: %v", err)
	}
	pending, _, err = deployPendingCertificate(dir, []string{"example.com"}, nil, []HookConfig{hook})
	if pending || err != nil {
		t.Fatalf("unchanged hook should not redeploy: pending=%t err=%v", pending, err)
	}
}
