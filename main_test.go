package main

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"acme4/providers"
	"acme4/providers/hurricane"
)

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

func TestBoolDefault(t *testing.T) {
	if !boolDefault(nil, true) {
		t.Fatal("nil should use default true")
	}
	if boolDefault(nil, false) {
		t.Fatal("nil should use default false")
	}

	value := false
	if boolDefault(&value, true) {
		t.Fatal("explicit false should override default true")
	}
}

func TestShouldSendExpiryWarning(t *testing.T) {
	tests := []struct {
		name            string
		remaining       time.Duration
		renewBeforeDays int
		want            bool
	}{
		{
			name:            "seven days before renewal window",
			remaining:       37 * 24 * time.Hour,
			renewBeforeDays: 30,
			want:            true,
		},
		{
			name:            "inside broad warning window but not boundary",
			remaining:       35 * 24 * time.Hour,
			renewBeforeDays: 30,
			want:            false,
		},
		{
			name:            "one day before renewal window",
			remaining:       31 * 24 * time.Hour,
			renewBeforeDays: 30,
			want:            true,
		},
		{
			name:            "expired",
			remaining:       -time.Hour,
			renewBeforeDays: 30,
			want:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSendExpiryWarning(tt.remaining, tt.renewBeforeDays); got != tt.want {
				t.Fatalf("shouldSendExpiryWarning() = %t, want %t", got, tt.want)
			}
		})
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

func TestLogHurricaneSharedChallengeWarnings(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(previous)

	logHurricaneSharedChallengeWarnings([]providers.Domain{
		{
			Names:    []string{"example.com", "*.example.com"},
			Provider: "hurricane",
		},
	})

	got := buf.String()
	if !strings.Contains(got, "共享 TXT 记录 _acme-challenge.example.com") {
		t.Fatalf("expected shared challenge warning, got %q", got)
	}
	if !strings.Contains(got, "顺序写入、验证、清理") {
		t.Fatalf("expected sequential Hurricane warning, got %q", got)
	}
}

func TestHurricaneFailureAdvice(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "interval",
			err:  fmt.Errorf("wrapped: %w", &hurricane.DiagnosticError{Code: hurricane.DiagnosticInterval, Message: "interval"}),
			want: "动态 DNS 更新触发限流",
		},
		{
			name: "badauth",
			err:  fmt.Errorf("wrapped: %w", &hurricane.DiagnosticError{Code: hurricane.DiagnosticBadAuth, Message: "badauth"}),
			want: "token 不匹配",
		},
		{
			name: "nohost",
			err:  fmt.Errorf("wrapped: %w", &hurricane.DiagnosticError{Code: hurricane.DiagnosticNoHost, Message: "nohost"}),
			want: "未在账号中作为动态记录存在",
		},
		{
			name: "propagation",
			err:  fmt.Errorf("timeout waiting for DNS record propagation"),
			want: "传播检查失败",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hurricaneFailureAdvice(tt.err, []string{"example.com", "*.example.com"})
			if !strings.Contains(got, tt.want) {
				t.Fatalf("hurricaneFailureAdvice() = %q, want substring %q", got, tt.want)
			}
		})
	}
}
