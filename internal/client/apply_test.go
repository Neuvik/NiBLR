package client

import (
	"errors"
	"os"
	"strings"
	"testing"
)

type env struct {
	files map[string]string
	modes map[string]os.FileMode
	ran   []string
	admin bool
	has   bool
	fail  string
}

func (e *env) applier(goos string) Applier {
	return Applier{GOOS: goos, Dir: "/d", IsAdmin: func() bool { return e.admin },
		Has:    func(string) bool { return e.has },
		Exists: func(p string) bool { _, ok := e.files[p]; return ok },
		Write:  func(p string, b []byte, m os.FileMode) error { e.files[p] = string(b); e.modes[p] = m; return nil },
		Remove: func(p string) error { delete(e.files, p); return nil },
		Run: func(n string, a ...string) (string, error) {
			c := n + " " + strings.Join(a, " ")
			e.ran = append(e.ran, c)
			if e.fail != "" && strings.Contains(c, e.fail) {
				return "RTNETLINK: boom\nmore", errors.New("exit 1")
			}
			return "", nil
		}}
}
func newEnv() *env {
	return &env{files: map[string]string{}, modes: map[string]os.FileMode{}, admin: true, has: true}
}

func TestLinuxJoinWritesPrivateFileAndBringsTunnelUp(t *testing.T) {
	e := newEnv()
	if err := e.applier("linux").Join(params()); err != nil {
		t.Fatal(err)
	}
	if e.modes["/d/niblr.conf"] != 0o600 {
		t.Fatalf("key file mode %v", e.modes["/d/niblr.conf"])
	}
	if len(e.ran) != 1 || e.ran[0] != "wg-quick up /d/niblr.conf" {
		t.Fatalf("%v", e.ran)
	}
}

func TestWindowsJoinInstallsTunnelService(t *testing.T) {
	e := newEnv()
	if err := e.applier("windows").Join(params()); err != nil {
		t.Fatal(err)
	}
	if e.ran[0] != "wireguard.exe /installtunnelservice /d/niblr.conf" {
		t.Fatalf("%v", e.ran)
	}
	if strings.Contains(e.files["/d/niblr.conf"], "iptables") {
		t.Fatal("windows got the linux config")
	}
}

func TestFailuresAreExplainedInPlainLanguageAndDoNothing(t *testing.T) {
	cases := []struct {
		goos  string
		setup func(*env)
		want  string
	}{
		{"linux", func(e *env) { e.admin = false }, "sudo"},
		{"windows", func(e *env) { e.admin = false }, "Run as administrator"},
		{"linux", func(e *env) { e.has = false }, "isn't installed"},
		{"windows", func(e *env) { e.has = false }, "WireGuard for Windows"},
		{"linux", func(e *env) { e.files["/d/niblr.conf"] = "x" }, "already joined"},
	}
	for _, c := range cases {
		e := newEnv()
		c.setup(e)
		before := len(e.files)
		err := e.applier(c.goos).Join(params())
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want mention of %q", c.goos, err, c.want)
		}
		if len(e.ran) != 0 || len(e.files) != before {
			t.Errorf("%s: must not run or write anything on refusal", c.goos)
		}
	}
}

func TestStartFailureDeletesTheKeyFile(t *testing.T) {
	e := newEnv()
	e.fail = "wg-quick up"
	err := e.applier("linux").Join(params())
	if err == nil || !strings.Contains(err.Error(), "RTNETLINK: boom") || strings.Contains(err.Error(), "more") {
		t.Fatalf("want first line of tool output only, got %v", err)
	}
	if len(e.files) != 0 {
		t.Fatal("private key left on disk after failed start")
	}
}

func TestLeaveIsSafeWhenNotJoinedAndRemovesKey(t *testing.T) {
	e := newEnv()
	if err := e.applier("linux").Leave(); err != nil || len(e.ran) != 0 {
		t.Fatalf("%v %v", err, e.ran)
	}
	e.applier("linux").Join(params())
	e.ran = nil
	if err := e.applier("linux").Leave(); err != nil {
		t.Fatal(err)
	}
	if len(e.files) != 0 || e.ran[0] != "wg-quick down /d/niblr.conf" {
		t.Fatalf("%v %v", e.files, e.ran)
	}
}
