// Package version reports the NiBLR build version. Release builds set Version, Commit, and Date with
// -ldflags (see .github/workflows/release.yml). A plain `go build` still reports something useful.
package version

import (
	"fmt"
	"runtime/debug"
)

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// String returns a one-line description, for example "niblr v0.1.0 (3f2a1bc, 2026-11-25)".
func String(program string) string {
	v, c, d := Version, Commit, Date
	if c == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			for _, s := range bi.Settings {
				switch s.Key {
				case "vcs.revision":
					if len(s.Value) >= 7 {
						c = s.Value[:7]
					}
				case "vcs.time":
					d = s.Value
				}
			}
		}
	}
	switch {
	case c != "" && d != "":
		return fmt.Sprintf("%s %s (%s, %s)", program, v, c, d)
	case c != "":
		return fmt.Sprintf("%s %s (%s)", program, v, c)
	}
	return fmt.Sprintf("%s %s", program, v)
}
