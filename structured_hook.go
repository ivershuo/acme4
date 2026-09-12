package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultHookTimeout = 120 * time.Second
	maxHookOutput      = 64 * 1024
)

type HookConfig struct {
	ID      string   `yaml:"id"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Timeout string   `yaml:"timeout"`
	EnvFile string   `yaml:"env_file"`
}

type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	remaining int
	truncated bool
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	original := len(p)
	if len(p) > w.remaining {
		p = p[:max(w.remaining, 0)]
		w.truncated = true
	}
	if len(p) > 0 {
		_, _ = w.buf.Write(p)
		w.remaining -= len(p)
	}
	return original, nil
}

func (w *limitedBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.truncated {
		return w.buf.String() + "\n[输出已截断]"
	}
	return w.buf.String()
}

func runStructuredHooks(hooks []HookConfig, domain, certPath, keyPath string) (string, error) {
	if len(hooks) == 0 {
		return "未配置后续命令；证书文件已更新。", nil
	}
	successes := 0
	var failures []error
	for _, hook := range hooks {
		if err := runStructuredHook(hook, domain, certPath, keyPath); err != nil {
			failures = append(failures, fmt.Errorf("hook %s: %w", hook.ID, err))
			logHookResult(hook.ID, "failed", err)
			continue
		}
		successes++
		logHookResult(hook.ID, "success", nil)
	}
	summary := fmt.Sprintf("结构化 hook 执行完成：成功 %d 个，失败 %d 个。", successes, len(failures))
	return summary, errors.Join(failures...)
}

func runStructuredHook(hook HookConfig, domain, certPath, keyPath string) error {
	timeout := defaultHookTimeout
	if hook.Timeout != "" {
		parsed, err := time.ParseDuration(hook.Timeout)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("invalid timeout %q", hook.Timeout)
		}
		timeout = parsed
	}
	command, err := replaceHookValue(hook.Command, domain, certPath, keyPath)
	if err != nil {
		return err
	}
	args := make([]string, len(hook.Args))
	for i, arg := range hook.Args {
		args[i], err = replaceHookValue(arg, domain, certPath, keyPath)
		if err != nil {
			return fmt.Errorf("args[%d]: %w", i, err)
		}
	}
	environment, err := hookEnvironment(hook.EnvFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.Command(command, args...)
	cmd.Env = environment
	configureHookCommand(cmd)
	output := &limitedBuffer{remaining: maxHookOutput}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		killHookProcessGroup(cmd)
		<-done
		return fmt.Errorf("timed out after %s; output: %s", timeout, redactHookOutput(output.String(), environment))
	}
	if err != nil {
		return fmt.Errorf("execution failed: %w; output: %s", err, redactHookOutput(output.String(), environment))
	}
	return nil
}

func redactHookOutput(output string, environment []string) string {
	for _, item := range environment {
		_, value, ok := strings.Cut(item, "=")
		if ok && value != "" {
			output = strings.ReplaceAll(output, value, "[REDACTED]")
		}
	}
	return output
}

func replaceHookValue(value, domain, certPath, keyPath string) (string, error) {
	replaced := strings.NewReplacer("{domain}", domain, "{cert_path}", certPath, "{key_path}", keyPath).Replace(value)
	if placeholder := hookPlaceholderPattern.FindString(replaced); placeholder != "" {
		return "", fmt.Errorf("unsupported hook placeholder: %s", placeholder)
	}
	return replaced, nil
}

func hookEnvironment(path string) ([]string, error) {
	// Structured hooks inherit only a minimal execution environment.
	values := []string{"PATH=" + os.Getenv("PATH")}
	if tmp := os.Getenv("TMPDIR"); tmp != "" {
		values = append(values, "TMPDIR="+tmp)
	}
	if path == "" {
		return values, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read env_file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("env_file must be a regular file inaccessible to group/others")
	}
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read env_file: %w", err)
	}
	for lineNumber, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" || strings.ContainsAny(key, " \t\r\n") {
			return nil, fmt.Errorf("env_file line %d is not KEY=VALUE", lineNumber+1)
		}
		values = append(values, key+"="+value)
	}
	return values, nil
}

func logHookResult(id, result string, err error) {
	if err != nil {
		log.Printf("[hook] id=%s result=%s error=%v", id, result, err)
		return
	}
	log.Printf("[hook] id=%s result=%s", id, result)
}
