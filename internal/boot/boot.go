// Package boot renders the pieces that configure machines: the exit's cloud-init and the hub-side
// commands that add or remove an exit. Everything here is pure (no I/O) so it can be unit tested.
package boot

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

const (
	OperatorNet  = "10.10.9.0/24"
	HubOpAddr    = "10.10.9.1"
	ExitPortBase = 51100 // exit slot N: hub listens on ExitPortBase+N
)

// GenKey returns a WireGuard (X25519) private/public key pair, base64 encoded.
func GenKey() (priv, pub string, err error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(k.Bytes()), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// PublicKey derives the WireGuard public key from a base64 private key.
func PublicKey(priv string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(priv))
	if err != nil {
		return "", fmt.Errorf("private key is not base64: %w", err)
	}
	k, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", fmt.Errorf("not a valid WireGuard key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// Link addressing for exit slot N (1..250): a /30 whose .1 is the hub and .2 is the exit.
func HubAddr(slot int) string  { return fmt.Sprintf("10.20.%d.1", slot) }
func ExitAddr(slot int) string { return fmt.Sprintf("10.20.%d.2", slot) }
func Iface(slot int) string    { return fmt.Sprintf("wg-e%d", slot) }
func Port(slot int) int        { return ExitPortBase + slot }

func checkSlot(slot int) error {
	if slot < 1 || slot > 250 {
		return fmt.Errorf("slot %d out of range 1..250", slot)
	}
	return nil
}

// ExitParams configures one exit.
type ExitParams struct {
	Slot         int
	PrivateKey   string // exit's WireGuard private key
	HubPublic    string // hub's WireGuard public key
	HubEndpoint  string // hub public IP
	LeaseMinutes int    // if >0 the exit shuts itself down (and so is terminated) after this long
}

// ExitCloudInit renders cloud-config for an exit: it dials the hub (so the exit's own public IP may
// change), forwards, and masquerades out its public interface. wg-quick restarts on failure with the
// systemd start limit lifted, so a flapping network can never leave it permanently dead.
func ExitCloudInit(p ExitParams) (string, error) {
	if err := checkSlot(p.Slot); err != nil {
		return "", err
	}
	if p.PrivateKey == "" || p.HubPublic == "" || p.HubEndpoint == "" {
		return "", fmt.Errorf("exit config needs private key, hub public key and hub endpoint")
	}
	s := strconv.Itoa(p.Slot)
	return `#cloud-config
package_update: true
packages: [wireguard, nftables]
write_files:
  - path: /etc/sysctl.d/99-niblr.conf
    content: |
      net.ipv4.ip_forward=1
  - path: /etc/wireguard/wg-hub.conf
    permissions: '0600'
    content: |
      [Interface]
      Address = ` + ExitAddr(p.Slot) + `/30
      PrivateKey = ` + p.PrivateKey + `
      [Peer]
      PublicKey = ` + p.HubPublic + `
      Endpoint = ` + p.HubEndpoint + `:` + strconv.Itoa(Port(p.Slot)) + `
      AllowedIPs = ` + HubAddr(p.Slot) + `/32, ` + OperatorNet + `
      PersistentKeepalive = 5
  - path: /etc/systemd/system/wg-quick@wg-hub.service.d/niblr.conf
    content: |
      [Unit]
      StartLimitIntervalSec=0
      [Service]
      Restart=on-failure
      RestartSec=2
  - path: /usr/local/sbin/niblr-exit-nat
    permissions: '0755'
    content: |
      #!/bin/sh
      IFC=$(ip -o -4 route show default | awk '{print $5}')
      nft flush ruleset
      nft add table ip nat
      nft 'add chain ip nat post { type nat hook postrouting priority 100; }'
      nft add rule ip nat post oifname "$IFC" masquerade
  - path: /etc/systemd/system/niblr-exit-nat.service
    content: |
      [Unit]
      Description=NiBLR exit masquerade
      After=network-online.target
      Wants=network-online.target
      [Service]
      Type=oneshot
      RemainAfterExit=yes
      ExecStart=/usr/local/sbin/niblr-exit-nat
      [Install]
      WantedBy=multi-user.target
runcmd:
  - sysctl --system
  - systemctl daemon-reload
  - systemctl enable --now niblr-exit-nat.service
  - systemctl enable --now wg-quick@wg-hub.service
  # slot ` + s + `: a lease — if this box is shut down from inside it is terminated, never left stopped
` + lease(p.LeaseMinutes), nil
}

// Cmd is one argv to run on the hub (no shell involved, so nothing to escape).
type Cmd []string

// HubExitUp returns the commands that make an exit's interface and route exist on the hub. The exit id
// must only be added to the pool AFTER these succeed (ordering invariant).
// hubKeyFile is a root-only file holding the hub's private key.
func HubExitUp(slot int, exitPublic, hubKeyFile string) ([]Cmd, error) {
	if err := checkSlot(slot); err != nil {
		return nil, err
	}
	if exitPublic == "" {
		return nil, fmt.Errorf("exit public key required")
	}
	i, t := Iface(slot), strconv.Itoa(1000+slot)
	return []Cmd{
		{"ip", "link", "del", i}, // tolerated failure: clears stale state from a previous exit in this slot
		{"ip", "link", "add", i, "type", "wireguard"},
		{"ip", "addr", "add", HubAddr(slot) + "/30", "dev", i},
		{"wg", "set", i, "private-key", hubKeyFile, "listen-port", strconv.Itoa(Port(slot)), "peer", exitPublic, "allowed-ips", "0.0.0.0/0"},
		{"ip", "link", "set", i, "up"},
		{"sysctl", "-qw", "net.ipv4.conf." + i + ".rp_filter=0"},
		{"ip", "route", "replace", "default", "dev", i, "table", t},
		{"ip", "rule", "add", "fwmark", strconv.Itoa(slot), "table", t, "priority", "100"},
	}, nil
}

// HubExitDown removes an exit. The id must first be removed from the pool; connections still marked with
// it are purged so the id can be reused without ever inheriting old flows.
func HubExitDown(slot int) ([]Cmd, error) {
	if err := checkSlot(slot); err != nil {
		return nil, err
	}
	t := strconv.Itoa(1000 + slot)
	return []Cmd{
		{"ip", "rule", "del", "fwmark", strconv.Itoa(slot), "table", t, "priority", "100"},
		{"conntrack", "-D", "--mark", strconv.Itoa(slot)},
		{"ip", "route", "flush", "table", t},
		{"ip", "link", "del", Iface(slot)},
	}, nil
}

func lease(min int) string {
	if min <= 0 {
		return ""
	}
	return fmt.Sprintf("  - shutdown -P +%d\n", min)
}
