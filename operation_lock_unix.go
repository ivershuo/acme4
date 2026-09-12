//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type operationLock struct {
	file *os.File
}

func acquireOperationLock(accountDir string) (*operationLock, error) {
	path := filepath.Join(accountDir, ".acme4.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("打开操作锁失败: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("另一个签发任务正在使用账户目录 %s", accountDir)
		}
		return nil, fmt.Errorf("获取操作锁失败: %w", err)
	}
	return &operationLock{file: f}, nil
}

func (l *operationLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	return errors.Join(err, l.file.Close())
}
