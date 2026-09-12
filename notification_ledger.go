package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"acme4/notification"
)

type notificationLedger struct {
	Sent                map[string]time.Time `json:"sent,omitempty"`
	LastFailureCategory string               `json:"last_failure_category,omitempty"`
	LastFailureAt       time.Time            `json:"last_failure_at,omitempty"`
}

const notificationHistoryRetention = 400 * 24 * time.Hour

func notificationLedgerPath(certDir string, names []string) string {
	return filepath.Join(certificateStateDir(certDir, names), "notifications.json")
}

func loadNotificationLedger(certDir string, names []string) (notificationLedger, error) {
	ledger := notificationLedger{Sent: map[string]time.Time{}}
	data, err := os.ReadFile(notificationLedgerPath(certDir, names))
	if os.IsNotExist(err) {
		return ledger, nil
	}
	if err != nil {
		return ledger, err
	}
	if err := json.Unmarshal(data, &ledger); err != nil {
		return ledger, err
	}
	if ledger.Sent == nil {
		ledger.Sent = map[string]time.Time{}
	}
	return ledger, nil
}

func saveNotificationLedger(certDir string, names []string, ledger notificationLedger) error {
	pruneNotificationHistory(&ledger, time.Now())
	dir := certificateStateDir(certDir, names)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return err
	}
	temp, err := writeTempFile(dir, ".notifications-*", append(data, '\n'))
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	return replaceFile(temp, notificationLedgerPath(certDir, names))
}

func pruneNotificationHistory(ledger *notificationLedger, now time.Time) {
	if ledger == nil {
		return
	}
	cutoff := now.Add(-notificationHistoryRetention)
	for key, sentAt := range ledger.Sent {
		if sentAt.Before(cutoff) {
			delete(ledger.Sent, key)
		}
	}
}

func expiryNotificationLevel(remaining time.Duration, renewBeforeDays int) string {
	days := remaining.Hours() / 24
	switch {
	case remaining <= 0:
		return "expired"
	case days <= 1:
		return "expiry-1d"
	case days <= 3:
		return "expiry-3d"
	case days <= 7:
		return "expiry-7d"
	case days <= float64(renewBeforeDays+1) && days > float64(renewBeforeDays):
		return "renewal-window-1d"
	case days <= float64(renewBeforeDays+3) && days > float64(renewBeforeDays):
		return "renewal-window-3d"
	case days <= float64(renewBeforeDays+7) && days > float64(renewBeforeDays):
		return "renewal-window-7d"
	default:
		return ""
	}
}

func sendExpiryNotificationOnce(service *notification.EmailService, certDir string, names []string, status certificateStatus, renewBeforeDays int) {
	if service == nil || !service.IsEnabled() || status.NotAfter.IsZero() {
		return
	}
	level := expiryNotificationLevel(status.Remaining, renewBeforeDays)
	if level == "" {
		return
	}
	certPEM, err := os.ReadFile(filepath.Join(certDir, names[0]+".crt"))
	if err != nil {
		return
	}
	fingerprint := sha256.Sum256(certPEM)
	eventKey := hex.EncodeToString(fingerprint[:8]) + ":" + level
	ledger, err := loadNotificationLedger(certDir, names)
	if err != nil {
		log.Printf("[警告] 到期通知状态读取失败，将继续尝试发送: %v", err)
		ledger = notificationLedger{Sent: map[string]time.Time{}}
	}
	if _, sent := ledger.Sent[eventKey]; sent {
		return
	}
	data := notification.NotificationData{Domains: names, Success: true, Timestamp: time.Now(), CertExpiry: &status.NotAfter, Remaining: &status.Remaining}
	if err := service.SendExpiryWarningNotification(data); err != nil {
		log.Printf("[警告] 到期通知发送失败，将在后续运行重试: %v", err)
		return // unconfirmed delivery remains eligible for retry
	}
	ledger.Sent[eventKey] = time.Now()
	if err := saveNotificationLedger(certDir, names, ledger); err != nil {
		log.Printf("[警告] 到期通知去重状态保存失败: %v", err)
	}
}

func failureCategory(err error) string {
	if err == nil {
		return "unknown"
	}
	text := err.Error()
	if before, _, ok := strings.Cut(text, ":"); ok {
		text = before
	}
	return strings.TrimSpace(text)
}

func failureNotificationDue(certDir string, names []string, category string, now time.Time) (notificationLedger, bool) {
	ledger, err := loadNotificationLedger(certDir, names)
	if err != nil {
		return ledger, true
	}
	due := ledger.LastFailureCategory != category || ledger.LastFailureAt.IsZero() || now.Sub(ledger.LastFailureAt) >= 24*time.Hour
	return ledger, due
}

func markFailureNotification(certDir string, names []string, ledger notificationLedger, category string, now time.Time) error {
	ledger.LastFailureCategory, ledger.LastFailureAt = category, now
	if err := saveNotificationLedger(certDir, names, ledger); err != nil {
		return fmt.Errorf("save failure notification state: %w", err)
	}
	return nil
}

func sendRenewalSuccessOnce(service *notification.EmailService, certDir string, names []string, status certificateStatus, hookSummary, version string) {
	if service == nil || !service.IsEnabled() || version == "" {
		return
	}
	ledger, err := loadNotificationLedger(certDir, names)
	if err != nil {
		log.Printf("[警告] 成功通知状态读取失败，将继续尝试发送: %v", err)
		ledger = notificationLedger{Sent: map[string]time.Time{}}
	}
	key := "success:" + version
	if _, sent := ledger.Sent[key]; sent {
		return
	}
	data := notification.NotificationData{Domains: names, Success: true, Timestamp: time.Now(), CertExpiry: &status.NotAfter, Remaining: &status.Remaining, HookSummary: hookSummary}
	if err := service.SendSuccessNotification(data); err != nil {
		log.Printf("[警告] 成功通知发送失败，将在后续运行重试: %v", err)
		return
	}
	ledger.Sent[key] = time.Now()
	ledger.LastFailureCategory = ""
	ledger.LastFailureAt = time.Time{}
	if err := saveNotificationLedger(certDir, names, ledger); err != nil {
		log.Printf("[警告] 成功通知去重状态保存失败: %v", err)
	}
}
