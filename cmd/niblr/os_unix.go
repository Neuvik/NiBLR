//go:build !windows

package main

import "os"

func isAdmin() bool     { return os.Geteuid() == 0 }
func configDir() string { return "/etc/niblr" }
