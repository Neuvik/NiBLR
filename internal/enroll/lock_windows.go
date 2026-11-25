//go:build windows

package enroll

import "os"

// The enrollment server only ever runs on the (Linux) hub; this exists so the operator client, which
// shares the package, builds on Windows. In-process locking (Server.mu) still applies.
func flock(*os.File) error { return nil }
func funlock(*os.File)     {}
