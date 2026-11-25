//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// `net session` only succeeds for an elevated Administrator.
func isAdmin() bool { return exec.Command("net", "session").Run() == nil }
func configDir() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return filepath.Join(pd, "NiBLR")
}
