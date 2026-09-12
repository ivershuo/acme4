//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

type operationLock struct {
	file       *os.File
	overlapped windows.Overlapped
}

func acquireOperationLock(accountDir string) (*operationLock, error) {
	path := filepath.Join(accountDir, ".acme4.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("打开操作锁失败: %w", err)
	}
	lock := &operationLock{file: f}
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &lock.overlapped)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, fmt.Errorf("另一个签发任务正在使用账户目录 %s", accountDir)
		}
		return nil, fmt.Errorf("获取操作锁失败: %w", err)
	}
	return lock, nil
}

func (l *operationLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &l.overlapped)
	return errors.Join(err, l.file.Close())
}
