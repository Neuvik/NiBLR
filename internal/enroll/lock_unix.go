//go:build !windows

package enroll

import (
	"os"
	"syscall"
)

func flock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX) }
func funlock(f *os.File)     { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
