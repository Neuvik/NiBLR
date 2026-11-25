package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Applier installs the tunnel on this machine. OS access is injected so the flow is testable.
type Applier struct {
	GOOS    string
	Dir     string      // where the config file is kept
	IsAdmin func() bool // running as root / Administrator?
	Run     func(name string, args ...string) (string, error)
	Has     func(name string) bool // is this program installed?
	Exists  func(path string) bool
	Write   func(path string, data []byte, mode os.FileMode) error
	Remove  func(path string) error
}

const tunnel = "niblr"

func (a Applier) confPath() string { return filepath.Join(a.Dir, tunnel+".conf") }

func (a Applier) wgTool() string {
	if a.GOOS == "windows" {
		return "wireguard.exe"
	}
	return "wg-quick"
}

// Join writes the config and starts the tunnel. Every failure is explained for a non-technical person.
func (a Applier) Join(p Params) error {
	var conf string
	var err error
	if a.GOOS == "windows" {
		conf, err = Windows(p)
	} else {
		conf, err = Linux(p)
	}
	if err != nil {
		return err
	}
	if !a.IsAdmin() {
		if a.GOOS == "windows" {
			return errors.New("NiBLR needs to run as Administrator. Close this window, right-click PowerShell, choose \"Run as administrator\", and try again")
		}
		return errors.New("NiBLR needs administrator rights to set up the network. Run the same command again with \"sudo\" in front of it")
	}
	if !a.Has(a.wgTool()) {
		if a.GOOS == "windows" {
			return errors.New("WireGuard isn't installed. Install \"WireGuard for Windows\" from https://www.wireguard.com/install/ and run this again")
		}
		return errors.New("WireGuard isn't installed. Install it with:  sudo apt install wireguard   (or your system's equivalent), then run this again")
	}
	if a.Exists(a.confPath()) {
		return errors.New("you're already joined. To use a new invite, run \"niblr leave\" first")
	}
	if err := a.Write(a.confPath(), []byte(conf), 0o600); err != nil {
		return fmt.Errorf("couldn't save the connection settings: %w", err)
	}
	var out string
	if a.GOOS == "windows" {
		out, err = a.Run("wireguard.exe", "/installtunnelservice", a.confPath())
	} else {
		out, err = a.Run("wg-quick", "up", a.confPath())
	}
	if err != nil {
		_ = a.Remove(a.confPath()) // never leave a half-configured machine with a key on disk
		return fmt.Errorf("couldn't start the connection: %s", strings.TrimSpace(firstLine(out, err)))
	}
	return nil
}

// Leave stops the tunnel and deletes the saved key. Safe to run when not joined.
func (a Applier) Leave() error {
	if !a.IsAdmin() {
		return errors.New("NiBLR needs administrator rights to turn off the connection (use sudo / \"Run as administrator\")")
	}
	var err error
	if a.GOOS == "windows" {
		_, err = a.Run("wireguard.exe", "/uninstalltunnelservice", tunnel)
	} else if a.Exists(a.confPath()) {
		_, err = a.Run("wg-quick", "down", a.confPath())
	}
	if rerr := a.Remove(a.confPath()); rerr != nil && !os.IsNotExist(rerr) && err == nil {
		err = rerr
	}
	return err
}

func (a Applier) Joined() bool { return a.Exists(a.confPath()) }

func firstLine(out string, err error) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return err.Error()
}
