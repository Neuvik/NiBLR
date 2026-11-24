// Package dataplane renders the hub's nftables ruleset. Rendering is pure (string in, string out)
// so it can be unit-tested without root; applying it is done by the daemon via `nft -f -`.
package dataplane

import (
	"fmt"
	"sort"
	"strings"
)

// Table and chain names owned by NiBLR.
const (
	Table = "niblr"
	Chain = "prerouting"
)

// RenderMarkRules returns an nft script that atomically (one transaction) replaces the marking rules.
//
// Each NEW flow arriving on opIface is given a mark equal to one healthy exit id (chosen at random)
// and the mark is saved in conntrack; later packets of the flow restore it. Because `ip rule fwmark`
// selects the exit's route table, every packet of a flow, in both directions, uses the same exit.
// With no healthy exit, new flows are dropped: the pool fails closed rather than leaking.
func RenderMarkRules(opIface string, healthy []int) string {
	ids := append([]int(nil), healthy...)
	sort.Ints(ids)

	var pick string
	if len(ids) == 0 {
		pick = "drop"
	} else {
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = fmt.Sprintf("%d : %d", i, id)
		}
		pick = fmt.Sprintf("meta mark set numgen random mod %d map { %s }", len(ids), strings.Join(parts, ", "))
	}

	// Flat `add` statements: older nft (Ubuntu 24.04) rejects nested one-liners.
	return fmt.Sprintf(`add table inet %[1]s
add chain inet %[1]s %[2]s { type filter hook prerouting priority -150; policy accept; }
flush chain inet %[1]s %[2]s
add rule inet %[1]s %[2]s iifname "%[3]s" ct state new %[4]s
add rule inet %[1]s %[2]s iifname "%[3]s" ct state new ct mark set meta mark
add rule inet %[1]s %[2]s iifname "%[3]s" ct state != new meta mark set ct mark
`, Table, Chain, opIface, pick)
}
