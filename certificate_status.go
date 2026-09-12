package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

type certificateStatus struct {
	NeedsRenew bool
	Reason     string
	Remaining  time.Duration
	NotAfter   time.Time
}

func inspectCertificatePair(certPath, keyPath string, configuredNames []string, renewBeforeDays int) (certificateStatus, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		if os.IsNotExist(err) {
			return certificateStatus{NeedsRenew: true, Reason: "certificate_missing"}, nil
		}
		return certificateStatus{}, fmt.Errorf("read certificate: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return certificateStatus{NeedsRenew: true, Reason: "certificate_invalid"}, nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return certificateStatus{NeedsRenew: true, Reason: "certificate_invalid"}, nil
	}
	status := certificateStatus{Remaining: time.Until(cert.NotAfter), NotAfter: cert.NotAfter}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			status.NeedsRenew, status.Reason = true, "private_key_missing"
			return status, nil
		}
		return certificateStatus{}, fmt.Errorf("read private key: %w", err)
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		status.NeedsRenew, status.Reason = true, "certificate_key_mismatch"
		return status, nil
	}
	if !sameNormalizedNames(configuredNames, cert.DNSNames) {
		status.NeedsRenew, status.Reason = true, "san_changed"
		return status, nil
	}
	if status.Remaining < time.Duration(renewBeforeDays)*24*time.Hour {
		status.NeedsRenew, status.Reason = true, "renewal_window"
		return status, nil
	}
	status.Reason = "valid"
	return status, nil
}

func sameNormalizedNames(left, right []string) bool {
	normalize := func(values []string) []string {
		result := make([]string, 0, len(values))
		for _, value := range values {
			result = append(result, strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), ".")))
		}
		slices.Sort(result)
		return slices.Compact(result)
	}
	return slices.Equal(normalize(left), normalize(right))
}
