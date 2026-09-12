//go:build windows

package main

import "os/exec"

func configureHookCommand(_ *exec.Cmd) {}

func killHookProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
