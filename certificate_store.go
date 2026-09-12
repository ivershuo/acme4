package main

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type pendingVersion struct {
	Version   string          `json:"version"`
	CertPath  string          `json:"cert_path"`
	KeyPath   string          `json:"key_path"`
	Completed map[string]bool `json:"completed_hooks"`
}

type certificateDeploymentState struct {
	CurrentVersion  string          `json:"current_version,omitempty"`
	PreviousVersion string          `json:"previous_version,omitempty"`
	CompletedHooks  map[string]bool `json:"completed_hooks,omitempty"`
	Pending         *pendingVersion `json:"pending,omitempty"`
}

type hookTask struct {
	id  string
	run func(certPath, keyPath string) error
}

func certificateStateDir(certDir string, names []string) string {
	normalized := append([]string(nil), names...)
	for i := range normalized {
		normalized[i] = strings.ToLower(strings.TrimSuffix(normalized[i], "."))
	}
	sort.Strings(normalized)
	sum := sha256.Sum256([]byte(strings.Join(normalized, "\x00")))
	return filepath.Join(certDir, ".acme4", hex.EncodeToString(sum[:8]))
}

func stageCertificateVersion(certDir string, names []string, certPEM, keyPEM []byte) (*pendingVersion, error) {
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return nil, fmt.Errorf("certificate/key validation: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, errors.New("certificate PEM is invalid")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	if !sameNormalizedNames(names, cert.DNSNames) {
		return nil, fmt.Errorf("issued certificate SANs %v do not match configured names %v", cert.DNSNames, names)
	}
	sum := sha256.Sum256(cert.Raw)
	version := hex.EncodeToString(sum[:12])
	versionDir := filepath.Join(certificateStateDir(certDir, names), "versions", version)
	if err := os.MkdirAll(versionDir, 0700); err != nil {
		return nil, fmt.Errorf("create certificate version directory: %w", err)
	}
	certPath, keyPath := filepath.Join(versionDir, "certificate.pem"), filepath.Join(versionDir, "private_key.pem")
	if err := saveCertificatePair(certPath, keyPath, certPEM, keyPEM); err != nil {
		return nil, err
	}
	return &pendingVersion{Version: version, CertPath: certPath, KeyPath: keyPath, Completed: map[string]bool{}}, nil
}

func statePath(certDir string, names []string) string {
	return filepath.Join(certificateStateDir(certDir, names), "state.json")
}

func loadDeploymentState(certDir string, names []string) (certificateDeploymentState, error) {
	var state certificateDeploymentState
	data, err := os.ReadFile(statePath(certDir, names))
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("decode deployment state: %w", err)
	}
	return state, nil
}

func saveDeploymentState(certDir string, names []string, state certificateDeploymentState) error {
	dir := certificateStateDir(certDir, names)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temp, err := writeTempFile(dir, ".state-*", append(data, '\n'))
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	return replaceFile(temp, statePath(certDir, names))
}

func configuredHookTasks(legacy []string, structured []HookConfig, domain string) []hookTask {
	var tasks []hookTask
	for index, command := range legacy {
		command := command
		sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", index, command)))
		id := "legacy-" + hex.EncodeToString(sum[:8])
		tasks = append(tasks, hookTask{id: id, run: func(certPath, keyPath string) error {
			_, err := runPostRenewHooks([]string{command}, domain, certPath, keyPath)
			return err
		}})
	}
	for _, configured := range structured {
		configured := configured
		tasks = append(tasks, hookTask{id: configured.ID, run: func(certPath, keyPath string) error {
			return runStructuredHook(configured, domain, certPath, keyPath)
		}})
	}
	return tasks
}

func savePendingCertificate(certDir string, names []string, certPEM, keyPEM []byte) error {
	pending, err := stageCertificateVersion(certDir, names, certPEM, keyPEM)
	if err != nil {
		return err
	}
	state, err := loadDeploymentState(certDir, names)
	if err != nil {
		return err
	}
	state.Pending = pending
	return saveDeploymentState(certDir, names, state)
}

func deployPendingCertificate(certDir string, names []string, legacy []string, structured []HookConfig) (bool, string, error) {
	state, err := loadDeploymentState(certDir, names)
	if err != nil {
		return false, "", err
	}
	tasks := configuredHookTasks(legacy, structured, names[0])
	if state.Pending == nil && state.CurrentVersion != "" {
		missing := false
		for _, task := range tasks {
			if !state.CompletedHooks[task.id] {
				missing = true
				break
			}
		}
		if missing {
			versionDir := filepath.Join(certificateStateDir(certDir, names), "versions", state.CurrentVersion)
			completed := make(map[string]bool, len(state.CompletedHooks))
			for id, done := range state.CompletedHooks {
				completed[id] = done
			}
			state.Pending = &pendingVersion{Version: state.CurrentVersion, CertPath: filepath.Join(versionDir, "certificate.pem"), KeyPath: filepath.Join(versionDir, "private_key.pem"), Completed: completed}
			if err := saveDeploymentState(certDir, names, state); err != nil {
				return true, "", err
			}
		}
	}
	if state.Pending == nil {
		return false, "", nil
	}
	pending := state.Pending
	if pending.Completed == nil {
		pending.Completed = map[string]bool{}
	}
	// Publish the complete, validated pair before hooks so compatibility hooks
	// such as `nginx -s reload` observe the pending version at their fixed paths.
	// The previous immutable version remains available until deployment commits.
	certPEM, err := os.ReadFile(pending.CertPath)
	if err != nil {
		return true, "", err
	}
	keyPEM, err := os.ReadFile(pending.KeyPath)
	if err != nil {
		return true, "", err
	}
	certPath, keyPath := certPaths(certDir, names)
	if err := saveCertificatePair(certPath, keyPath, certPEM, keyPEM); err != nil {
		return true, "", fmt.Errorf("publish compatibility paths: %w", err)
	}
	var failures []error
	for _, task := range tasks {
		if pending.Completed[task.id] {
			continue
		}
		if err := saveDeploymentState(certDir, names, state); err != nil {
			return true, "", fmt.Errorf("record pending hook %s: %w", task.id, err)
		}
		if err := task.run(pending.CertPath, pending.KeyPath); err != nil {
			failures = append(failures, fmt.Errorf("hook %s: %w", task.id, err))
			continue
		}
		pending.Completed[task.id] = true
		if err := saveDeploymentState(certDir, names, state); err != nil {
			return true, "", fmt.Errorf("record completed hook %s: %w", task.id, err)
		}
	}
	if len(failures) > 0 {
		return true, fmt.Sprintf("待部署版本 %s 尚有 %d 个 hook 失败", pending.Version, len(failures)), errors.Join(failures...)
	}
	state.PreviousVersion = state.CurrentVersion
	state.CurrentVersion = pending.Version
	state.CompletedHooks = pending.Completed
	state.Pending = nil
	if err := saveDeploymentState(certDir, names, state); err != nil {
		return true, "", fmt.Errorf("commit deployed version: %w", err)
	}
	if err := cleanupUnreferencedVersions(certDir, names, state); err != nil {
		log.Printf("[清理警告] certificate=%v old_versions=%v", names, err)
	}
	return true, fmt.Sprintf("证书版本 %s 已完成部署", pending.Version), nil
}

func cleanupUnreferencedVersions(certDir string, names []string, state certificateDeploymentState) error {
	versionsDir := filepath.Join(certificateStateDir(certDir, names), "versions")
	entries, err := os.ReadDir(versionsDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	referenced := map[string]bool{state.CurrentVersion: true, state.PreviousVersion: true}
	if state.Pending != nil {
		referenced[state.Pending.Version] = true
	}
	for _, entry := range entries {
		if !entry.IsDir() || referenced[entry.Name()] {
			continue
		}
		target := filepath.Join(versionsDir, entry.Name())
		relative, relErr := filepath.Rel(versionsDir, target)
		if relErr != nil || relative != entry.Name() {
			return fmt.Errorf("refuse to clean unexpected version path %q", target)
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
	}
	return nil
}
