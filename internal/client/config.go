// Package client renders the operator's WireGuard configuration and the instructions for applying it.
//
// Leak-proofing, because operators are not network people and a leak means a real address in a client's logs:
//   - AllowedIPs = 0.0.0.0/0, ::/0  : everything goes in the tunnel, IPv6 too. The hub has no IPv6 route, so
//     IPv6 traffic is dropped there (fail closed) instead of leaving the operator's own address.
//   - DNS goes through the tunnel (resolvers are reached via the exits).
//   - Windows: WireGuard for Windows blocks all traffic outside the tunnel when AllowedIPs covers 0.0.0.0/0
//     and ::/0 (its built-in kill switch); we rely on and document that.
//   - Linux: wg-quick routes everything via the tunnel, and we add firewall rules that REJECT anything not
//     going through it, so a tunnel failure cannot fall back to the normal route.
package client

import (
	"errors"
	"fmt"
	"strings"

	"niblr/internal/enroll"
)

type Params struct {
	PrivateKey string
	Result     enroll.Result
}

func (p Params) check() error {
	r := p.Result
	switch {
	case p.PrivateKey == "":
		return errors.New("no private key")
	case r.Address == "" || r.HubKey == "" || r.WG == "":
		return errors.New("incomplete enrollment result")
	case r.MTU < 1280 || r.MTU > 1500:
		return fmt.Errorf("unreasonable MTU %d", r.MTU)
	}
	return nil
}

func (p Params) common() string {
	r := p.Result
	var b strings.Builder
	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\nAddress = %s\nMTU = %d\n", p.PrivateKey, r.Address, r.MTU)
	if len(r.DNS) > 0 {
		fmt.Fprintf(&b, "DNS = %s\n", strings.Join(r.DNS, ", "))
	}
	return b.String()
}

func (p Params) peer() string {
	return fmt.Sprintf("\n[Peer]\nPublicKey = %s\nEndpoint = %s\nAllowedIPs = 0.0.0.0/0, ::/0\nPersistentKeepalive = 15\n", p.Result.HubKey, p.Result.WG)
}

// Windows returns a tunnel file for WireGuard for Windows (import it, or `wireguard.exe /installtunnelservice`).
func Windows(p Params) (string, error) {
	if err := p.check(); err != nil {
		return "", err
	}
	return p.common() + p.peer(), nil
}

// Linux returns a wg-quick config with a firewall kill switch (the pattern from the wg-quick(8) man page).
func Linux(p Params) (string, error) {
	if err := p.check(); err != nil {
		return "", err
	}
	const up = `PostUp = iptables -I OUTPUT ! -o %i -m mark ! --mark $(wg show %i fwmark) -m addrtype ! --dst-type LOCAL -j REJECT && ip6tables -I OUTPUT ! -o %i -m mark ! --mark $(wg show %i fwmark) -m addrtype ! --dst-type LOCAL -j REJECT
PreDown = iptables -D OUTPUT ! -o %i -m mark ! --mark $(wg show %i fwmark) -m addrtype ! --dst-type LOCAL -j REJECT; ip6tables -D OUTPUT ! -o %i -m mark ! --mark $(wg show %i fwmark) -m addrtype ! --dst-type LOCAL -j REJECT
`
	return p.common() + up + p.peer(), nil
}
