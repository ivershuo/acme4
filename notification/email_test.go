package notification

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/resend/resend-go/v2"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestBuildSuccessTextUsesDayBasedRemainingAndHookSummary(t *testing.T) {
	service := &EmailService{}
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	expiry := now.Add(89*24*time.Hour + 13*time.Hour)
	remaining := expiry.Sub(now)

	got := service.buildSuccessText(NotificationData{
		Domains:     []string{"example.com", "*.example.com"},
		Timestamp:   now,
		CertExpiry:  &expiry,
		Remaining:   &remaining,
		HookSummary: "后续命令执行完成：成功 1 个，失败 0 个。",
	})

	if !strings.Contains(got, "新证书有效期: 约 90 天") {
		t.Fatalf("expected day-based remaining duration, got %q", got)
	}
	if !strings.Contains(got, "后续命令执行完成：成功 1 个，失败 0 个。") {
		t.Fatalf("expected hook summary, got %q", got)
	}
	if strings.Contains(got, "h") {
		t.Fatalf("remaining duration should not use raw Go hour format, got %q", got)
	}
}

func TestEmailRequestTimeout(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	client := resend.NewCustomClient(&http.Client{Timeout: 20 * time.Millisecond, Transport: transport}, "test-key")
	service := &EmailService{client: client, fromEmail: "from@example.com", toEmails: []string{"to@example.com"}, enabled: true, notifyOnFailure: true}
	started := time.Now()
	err := service.SendFailureNotification(NotificationData{Domains: []string{"example.com"}, Error: "failure", Timestamp: time.Now()})
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("expected bounded email request, duration=%s err=%v", time.Since(started), err)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestFormatFromAddressEscapesDisplayName(t *testing.T) {
	formatted := formatFromAddress("Ops, Team\r\nBcc: attacker@example.com", "from@example.com")
	if strings.ContainsAny(formatted, "\r\n") {
		t.Fatalf("formatted From address contains a header newline: %q", formatted)
	}
	parsed, err := mail.ParseAddress(formatted)
	if err != nil {
		t.Fatalf("formatted From address is invalid: %v", err)
	}
	if parsed.Address != "from@example.com" {
		t.Fatalf("formatted From address = %q, want from@example.com", parsed.Address)
	}
}

func TestBuildFailureHTMLEscapesDynamicContentAndIncludesAdvice(t *testing.T) {
	service := &EmailService{}

	got := service.buildFailureHTML(NotificationData{
		Domains:   []string{"<bad.example>"},
		Timestamp: time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC),
		Error:     "<script>alert(1)</script>",
		Advice:    "检查 _acme-challenge TXT 记录。",
	})

	if strings.Contains(got, "<script>alert(1)</script>") || strings.Contains(got, "<bad.example>") {
		t.Fatalf("expected dynamic HTML content to be escaped, got %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("expected escaped error content, got %q", got)
	}
	if !strings.Contains(got, "诊断建议") || !strings.Contains(got, "检查 _acme-challenge TXT 记录。") {
		t.Fatalf("expected diagnostic advice, got %q", got)
	}
}

func TestNotificationSwitchesSkipSend(t *testing.T) {
	service := &EmailService{
		enabled:         true,
		notifyOnSuccess: false,
		notifyOnFailure: false,
		notifyOnExpiry:  false,
	}

	if err := service.SendSuccessNotification(NotificationData{}); err != nil {
		t.Fatalf("SendSuccessNotification() error = %v", err)
	}
	if err := service.SendFailureNotification(NotificationData{}); err != nil {
		t.Fatalf("SendFailureNotification() error = %v", err)
	}
	if err := service.SendExpiryWarningNotification(NotificationData{}); err != nil {
		t.Fatalf("SendExpiryWarningNotification() error = %v", err)
	}
}

func TestFormatDurationForEmail(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{name: "expired", in: -time.Hour, want: "已过期"},
		{name: "one minute", in: time.Minute, want: "1 分钟"},
		{name: "round up minutes", in: 61 * time.Second, want: "2 分钟"},
		{name: "hours and minutes", in: 23*time.Hour + 59*time.Minute, want: "23 小时 59 分钟"},
		{name: "one day with hours", in: 47 * time.Hour, want: "1 天 23 小时"},
		{name: "round large durations to days", in: 89*24*time.Hour + 13*time.Hour, want: "约 90 天"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatDurationForEmail(tt.in); got != tt.want {
				t.Fatalf("formatDurationForEmail() = %q, want %q", got, tt.want)
			}
		})
	}
}
