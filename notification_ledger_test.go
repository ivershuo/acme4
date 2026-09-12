package main

import (
	"testing"
	"time"
)

func TestExpiryNotificationLevelUsesCrossedThresholds(t *testing.T) {
	tests := []struct {
		remaining time.Duration
		want      string
	}{
		{38 * 24 * time.Hour, ""},
		{36 * 24 * time.Hour, "renewal-window-7d"},
		{32 * 24 * time.Hour, "renewal-window-3d"},
		{30*time.Hour + 30*24*time.Hour, "renewal-window-3d"},
		{6 * 24 * time.Hour, "expiry-7d"},
		{2 * 24 * time.Hour, "expiry-3d"},
		{12 * time.Hour, "expiry-1d"},
		{-time.Hour, "expired"},
	}
	for _, test := range tests {
		if got := expiryNotificationLevel(test.remaining, 30); got != test.want {
			t.Fatalf("remaining %s: got %q want %q", test.remaining, got, test.want)
		}
	}
}

func TestFailureNotificationDailyLimitAndCategoryChange(t *testing.T) {
	dir := t.TempDir()
	names := []string{"example.com"}
	now := time.Now()
	ledger, due := failureNotificationDue(dir, names, "validation", now)
	if !due {
		t.Fatal("first failure should notify")
	}
	if err := markFailureNotification(dir, names, ledger, "validation", now); err != nil {
		t.Fatal(err)
	}
	if _, due := failureNotificationDue(dir, names, "validation", now.Add(6*time.Hour)); due {
		t.Fatal("same failure category should be limited for 24 hours")
	}
	if _, due := failureNotificationDue(dir, names, "deployment", now.Add(6*time.Hour)); !due {
		t.Fatal("changed failure category should notify immediately")
	}
	if _, due := failureNotificationDue(dir, names, "validation", now.Add(25*time.Hour)); !due {
		t.Fatal("same category should notify again after 24 hours")
	}
}

func TestPruneNotificationHistoryRemovesOnlyExpiredEntries(t *testing.T) {
	now := time.Now()
	ledger := notificationLedger{Sent: map[string]time.Time{
		"expired": now.Add(-notificationHistoryRetention - time.Hour),
		"recent":  now.Add(-notificationHistoryRetention + time.Hour),
	}}
	pruneNotificationHistory(&ledger, now)
	if _, exists := ledger.Sent["expired"]; exists {
		t.Fatal("expired notification history was not pruned")
	}
	if _, exists := ledger.Sent["recent"]; !exists {
		t.Fatal("recent notification history was pruned")
	}
}
