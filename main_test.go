package main

import "testing"

func TestExpandHookCommand(t *testing.T) {
	got, err := expandHookCommand(
		"/hooks/tencent-upload-cert --domain {domain} --cert {cert_path} --key {key_path}",
		"example.com",
		"/tmp/example.com.crt",
		"/tmp/example.com.key",
	)
	if err != nil {
		t.Fatalf("expandHookCommand() error = %v", err)
	}

	want := "/hooks/tencent-upload-cert --domain example.com --cert /tmp/example.com.crt --key /tmp/example.com.key"
	if got != want {
		t.Fatalf("expandHookCommand() = %q, want %q", got, want)
	}
}

func TestExpandHookCommandRejectsUnknownPlaceholder(t *testing.T) {
	_, err := expandHookCommand(
		"/hooks/tencent-upload-cert --domain {domain} --project {project_id}",
		"example.com",
		"/tmp/example.com.crt",
		"/tmp/example.com.key",
	)
	if err == nil {
		t.Fatal("expandHookCommand() expected error for unknown placeholder")
	}
}
