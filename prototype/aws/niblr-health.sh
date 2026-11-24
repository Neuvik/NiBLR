#!/bin/bash
# Seed of niblrd (runs on the hub). Finds healthy exits and atomically rewrites the
# marking rules: each NEW operator flow gets an exit id stored in conntrack.
# Healthy = recent WireGuard handshake AND an END-TO-END probe works: a ping marked for that exit,
# sourced from the operator subnet, goes out via the exit (NAT) to the internet and back. This proves
# the return route and NAT too, not just the tunnel (a tunnel-only probe missed a dead return route).
# DRAIN="1 2" removes exits from rotation without touching established flows.
OPIF=${OPIF:-wg-op}; EXITS=${EXITS:-"1 2"}
healthy=(); now=$(date +%s)
for i in $EXITS; do
  case " ${DRAIN:-} " in *" $i "*) continue;; esac
  hs=$(wg show wg-e$i latest-handshakes 2>/dev/null | awk '{print $2}')
  if [ "${hs:-0}" -gt 0 ] && [ $((now - hs)) -lt 200 ] && ping -c1 -W2 -m $i -I ${PROBE_SRC:-10.10.9.1} ${PROBE_TARGET:-1.1.1.1} >/dev/null 2>&1; then healthy+=($i); fi
done
n=${#healthy[@]}
if [ "$n" -gt 0 ]; then
  map=""; k=0; for m in "${healthy[@]}"; do map+="$k : $m, "; k=$((k+1)); done
  pick="meta mark set numgen random mod $n map { ${map%, } }"
else pick="drop"; fi   # no healthy exit: fail closed
nft -f - <<NFT
add table inet niblr
add chain inet niblr prerouting { type filter hook prerouting priority -150; policy accept; }
flush chain inet niblr prerouting
add rule inet niblr prerouting iifname "$OPIF" ct state new $pick
add rule inet niblr prerouting iifname "$OPIF" ct state new ct mark set meta mark
add rule inet niblr prerouting iifname "$OPIF" ct state != new meta mark set ct mark
NFT
echo "$(date +%T) healthy exits: ${healthy[*]:-none}"
