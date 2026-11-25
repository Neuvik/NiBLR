package hub

import (
	"context"
	"fmt"
	"strings"

	"niblr/internal/boot"
)

// Base is the hub's fixed plumbing: forwarding, the operator WireGuard interface, and the fail-closed
// fallback route for operator traffic that carries no exit mark.
type Base struct {
	R       Runner
	KeyFile string
	OpIface string
	OpPort  int
	ConnMax int
}

// EnsureBase is idempotent: safe at every start and after a hub reboot.
func (b Base) EnsureBase(ctx context.Context) error {
	run := func(argv ...string) (string, error) {
		out, err := b.R.Run(ctx, argv...)
		if err != nil {
			return out, fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
		}
		return out, nil
	}
	sys := []string{"net.ipv4.ip_forward=1", "net.ipv4.conf.all.rp_filter=0", "net.ipv4.conf.default.rp_filter=0"}
	if _, err := run(append([]string{"sysctl", "-qw"}, sys...)...); err != nil {
		return err
	}
	if b.ConnMax > 0 { // best effort: the module may not be loaded yet
		_, _ = b.R.Run(ctx, "sysctl", "-qw", fmt.Sprintf("net.netfilter.nf_conntrack_max=%d", b.ConnMax))
	}
	ifs, err := run("wg", "show", "interfaces")
	if err != nil {
		return err
	}
	have := false
	for _, f := range strings.Fields(ifs) {
		if f == b.OpIface {
			have = true
		}
	}
	if !have {
		if _, err := run("ip", "link", "add", b.OpIface, "type", "wireguard"); err != nil {
			return err
		}
		if _, err := run("ip", "addr", "add", boot.HubOpAddr+"/24", "dev", b.OpIface); err != nil {
			return err
		}
	}
	for _, c := range [][]string{
		{"wg", "set", b.OpIface, "private-key", b.KeyFile, "listen-port", fmt.Sprint(b.OpPort)},
		{"ip", "link", "set", b.OpIface, "up"},
		{"sysctl", "-qw", "net.ipv4.conf." + b.OpIface + ".rp_filter=0"},
		{"ip", "route", "replace", "unreachable", "default", "table", "200"},
	} {
		if _, err := run(c...); err != nil {
			return err
		}
	}
	rules, err := run("ip", "rule", "show")
	if err != nil {
		return err
	}
	if !strings.Contains(rules, "from "+boot.OperatorNet+" lookup 200") {
		if _, err := run("ip", "rule", "add", "from", boot.OperatorNet, "table", "200", "priority", "200"); err != nil {
			return err
		}
	}
	return nil
}

// AddOperator registers an operator's public key on the operator interface.
func (b Base) AddOperator(ctx context.Context, pub, ip string) error {
	_, err := b.R.Run(ctx, "wg", "set", b.OpIface, "peer", pub, "allowed-ips", ip+"/32")
	return err
}
