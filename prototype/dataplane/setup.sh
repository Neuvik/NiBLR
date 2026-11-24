#!/bin/bash
# Builds: client -- hub -- (wireguard x2) -- exit1/exit2 -- target "internet"
# Hub marks each NEW client flow with an exit id (stored in conntrack), policy-routes
# by mark, so every packet of a flow (both directions) uses the same exit.
source "$(dirname "$0")/lib.sh"
[ "$(id -u)" = 0 ] || { echo "run as root"; exit 1; }
"$(dirname "$0")/teardown.sh" >/dev/null 2>&1 || true

umask 077; mkdir -p "$KEYDIR"
wg genkey | tee "$KEYDIR/hub.key" | wg pubkey > "$KEYDIR/hub.pub"
for i in 1 2; do wg genkey | tee "$KEYDIR/e$i.key" | wg pubkey > "$KEYDIR/e$i.pub"; done

for ns in $NS_CLIENT $NS_HUB $NS_NET "${NS_EXITS[@]}"; do ip netns add $ns; nsx $ns ip link set lo up; done

# client <-> hub
ip link add c-h netns $NS_CLIENT type veth peer name h-c netns $NS_HUB
nsx $NS_CLIENT ip addr add 10.10.9.2/24 dev c-h; nsx $NS_CLIENT ip link set c-h up
nsx $NS_CLIENT ip route add default via 10.10.9.1
nsx $NS_HUB ip addr add 10.10.9.1/24 dev h-c;   nsx $NS_HUB ip link set h-c up

# target "internet": bridge 203.0.113.1/24
nsx $NS_NET ip link add br0 type bridge
nsx $NS_NET ip addr add 203.0.113.1/24 dev br0; nsx $NS_NET ip link set br0 up

for i in 1 2; do
  ex=niblr-e$i
  # underlay hub <-> exit (stands in for the public internet path)
  ip link add h-e$i netns $NS_HUB type veth peer name e$i-h netns $ex
  nsx $NS_HUB ip addr add 172.16.$i.1/30 dev h-e$i; nsx $NS_HUB ip link set h-e$i up
  nsx $ex ip addr add 172.16.$i.2/30 dev e$i-h;     nsx $ex ip link set e$i-h up
  # exit <-> target (the exit's "public" address 203.0.113.1$i)
  ip link add e$i-n netns $ex type veth peer name n-e$i netns $NS_NET
  nsx $ex ip addr add 203.0.113.1$i/24 dev e$i-n; nsx $ex ip link set e$i-n up
  nsx $ex ip route add default via 203.0.113.1
  nsx $NS_NET ip link set n-e$i master br0; nsx $NS_NET ip link set n-e$i up

  # wireguard: hub keeps one interface per exit; the EXIT dials the hub
  nsx $NS_HUB ip link add wg-e$i type wireguard
  nsx $NS_HUB ip addr add 10.20.$i.1/30 dev wg-e$i
  nsx $NS_HUB wg set wg-e$i private-key "$KEYDIR/hub.key" listen-port 5100$i \
      peer "$(cat $KEYDIR/e$i.pub)" allowed-ips 0.0.0.0/0
  nsx $NS_HUB ip link set wg-e$i up

  nsx $ex ip link add wg-hub type wireguard
  nsx $ex ip addr add 10.20.$i.2/30 dev wg-hub
  nsx $ex wg set wg-hub private-key "$KEYDIR/e$i.key" \
      peer "$(cat $KEYDIR/hub.pub)" endpoint 172.16.$i.1:5100$i \
      allowed-ips 10.20.$i.1/32,10.10.9.0/24 persistent-keepalive 2
  nsx $ex ip link set wg-hub up
  nsx $ex ip route add 10.10.9.0/24 dev wg-hub
  nsx $ex sysctl -qw net.ipv4.ip_forward=1
  nsx $ex nft -f - <<NFT
table ip nat {
  chain post {
    type nat hook postrouting priority 100;
    oifname "e$i-n" masquerade
  }
}
NFT

  # hub: one route table per exit, selected by fwmark
  nsx $NS_HUB ip route add default dev wg-e$i table 10$i
  nsx $NS_HUB ip rule add fwmark $i table 10$i priority 100
done

# hub: forwarding, loose rp_filter (replies arrive on the exit's wg iface), fail-closed fallback
nsx $NS_HUB sysctl -qw net.ipv4.ip_forward=1 net.ipv4.conf.all.rp_filter=0 net.ipv4.conf.default.rp_filter=0
for ifc in h-c wg-e1 wg-e2 h-e1 h-e2; do nsx $NS_HUB sysctl -qw net.ipv4.conf.$ifc.rp_filter=0; done
nsx $NS_HUB ip route add unreachable default table 200
nsx $NS_HUB ip rule add from 10.10.9.0/24 table 200 priority 200

# target server
nsx $NS_NET python3 "$(dirname "$0")/target.py" >/tmp/niblr-target.log 2>&1 &
echo $! > /tmp/niblr-target.pid
sleep 0.5
"$(dirname "$0")/healthcheck.sh"
echo "setup done"
