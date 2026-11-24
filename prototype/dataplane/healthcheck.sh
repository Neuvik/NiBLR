#!/bin/bash
# The seed of niblrd: find healthy exits and atomically rewrite the hub's marking rules.
# Healthy = recent WireGuard handshake AND a probe answered through that exit's tunnel.
source "$(dirname "$0")/lib.sh"
healthy=()
now=$(date +%s)
for i in 1 2; do
  case " ${DRAIN:-} " in *" $i "*) continue;; esac   # drained: leave pool, keep tunnel up
  hs=$(nsx $NS_HUB wg show wg-e$i latest-handshakes | awk '{print $2}')
  if [ "${hs:-0}" -gt 0 ] && [ $((now - hs)) -lt 200 ] && \
     nsx $NS_HUB ping -c1 -W1 -I wg-e$i 10.20.$i.2 >/dev/null 2>&1; then healthy+=($i); fi
done
n=${#healthy[@]}
if [ "$n" -gt 0 ]; then
  map=""; k=0
  for m in "${healthy[@]}"; do map+="$k : $m, "; k=$((k+1)); done
  pick="meta mark set numgen random mod $n map { ${map%, } }"
else
  pick="drop"   # no healthy exit: fail closed
fi
nsx $NS_HUB nft -f - <<NFT
table inet niblr {
  chain prerouting { type filter hook prerouting priority -150; policy accept; }
}
flush chain inet niblr prerouting
table inet niblr {
  chain prerouting {
    iifname "h-c" ct state new $pick
    iifname "h-c" ct state new ct mark set meta mark
    iifname "h-c" ct state != new meta mark set ct mark
  }
}
NFT
echo "healthy exits: ${healthy[*]:-none}"
