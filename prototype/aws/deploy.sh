#!/bin/bash
# Configures the rig from prototype/aws: hub, exit1, exit2, client. Run from this directory.
set -euo pipefail
S=${SCRATCH:?set SCRATCH to the key dir}; KEY=$S/niblr-proto; K=$S/wgkeys; umask 077; mkdir -p $K
J=$(terraform output -json ips)
ip_of() { echo "$J" | python3 -c "import json,sys;print(json.load(sys.stdin)['$1']['public'])"; }
HUB=$(ip_of hub); E1=$(ip_of exit1); E2=$(ip_of exit2); CL=$(ip_of client)
sx() { ssh -i $KEY -o StrictHostKeyChecking=accept-new -o BatchMode=yes ubuntu@$1 "sudo bash -s"; }
for n in hub e1 e2 client; do [ -f $K/$n.key ] || { wg genkey | tee $K/$n.key | wg pubkey > $K/$n.pub; }; done
put() { ssh -i $KEY -o BatchMode=yes ubuntu@$1 "sudo tee $2 >/dev/null && sudo chmod ${3:-600} $2"; }

echo "== hub"
put $HUB /etc/wireguard-hub.key < $K/hub.key
put $HUB /usr/local/bin/niblr-health 755 < niblr-health.sh
sx $HUB <<HUBEOF
set -e
sysctl -qw net.ipv4.ip_forward=1 net.ipv4.conf.all.rp_filter=0 net.ipv4.conf.default.rp_filter=0
ip link del wg-op 2>/dev/null || true
ip link add wg-op type wireguard; ip addr add 10.10.9.1/24 dev wg-op
wg set wg-op private-key /etc/wireguard-hub.key listen-port 51000 peer $(cat $K/client.pub) allowed-ips 10.10.9.2/32
ip link set wg-op up; sysctl -qw net.ipv4.conf.wg-op.rp_filter=0
for i in 1 2; do
  ip link del wg-e\$i 2>/dev/null || true
  ip link add wg-e\$i type wireguard; ip addr add 10.20.\$i.1/30 dev wg-e\$i
  [ \$i = 1 ] && P=$(cat $K/e1.pub) || P=$(cat $K/e2.pub)
  wg set wg-e\$i private-key /etc/wireguard-hub.key listen-port 5100\$i peer \$P allowed-ips 0.0.0.0/0
  ip link set wg-e\$i up; sysctl -qw net.ipv4.conf.wg-e\$i.rp_filter=0
  ip route replace default dev wg-e\$i table 10\$i
  ip rule del fwmark \$i table 10\$i priority 100 2>/dev/null || true; ip rule add fwmark \$i table 10\$i priority 100
done
ip route replace unreachable default table 200
ip rule del from 10.10.9.0/24 table 200 priority 200 2>/dev/null || true; ip rule add from 10.10.9.0/24 table 200 priority 200
cat >/etc/systemd/system/niblr-health.service <<'UNIT'
[Unit]
Description=NiBLR exit pool health checker (niblrd seed)
After=network-online.target
[Service]
ExecStart=/bin/bash -c "while true; do /usr/local/bin/niblr-health >> /var/log/niblr-health.log 2>&1; sleep 2; done"
Restart=always
[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload; systemctl enable niblr-health >/dev/null 2>&1; systemctl restart niblr-health
echo hub configured
HUBEOF

for i in 1 2; do
  EXIT_IP=$([ $i = 1 ] && echo $E1 || echo $E2)
  echo "== exit$i"
  put $EXIT_IP /etc/wireguard-exit.key < $K/e$i.key
  sx $EXIT_IP <<EXEOF
set -e
sysctl -qw net.ipv4.ip_forward=1
IFC=\$(ip -o -4 route show default | awk '{print \$5}')
systemctl disable --now wg-quick@wg-hub 2>/dev/null || true; ip link del wg-hub 2>/dev/null || true
umask 077
cat >/etc/wireguard/wg-hub.conf <<WGC
[Interface]
Address = 10.20.$i.2/30
PrivateKey = \$(cat /etc/wireguard-exit.key)
[Peer]
PublicKey = $(cat $K/hub.pub)
Endpoint = $HUB:5100$i
AllowedIPs = 10.20.$i.1/32, 10.10.9.0/24
PersistentKeepalive = 5
WGC
systemctl enable --now wg-quick@wg-hub >/dev/null 2>&1
nft flush ruleset
nft -f - <<NFT
table ip nat {
  chain post {
    type nat hook postrouting priority 100;
    oifname "\$IFC" masquerade
  }
}
NFT
echo exit$i configured on \$IFC
EXEOF
done

echo "== client"
put $CL /etc/wireguard-client.key < $K/client.key
sx $CL <<CLEOF
set -e
ip link del wg-op 2>/dev/null || true
GW=\$(ip -o -4 route show default | awk '{print \$3}'); IFC=\$(ip -o -4 route show default | awk '{print \$5}')
ip link add wg-op type wireguard; ip addr add 10.10.9.2/24 dev wg-op
wg set wg-op private-key /etc/wireguard-client.key peer $(cat $K/hub.pub) endpoint $HUB:51000 allowed-ips 0.0.0.0/0 persistent-keepalive 5
ip link set wg-op up
# keep SSH (operator IP) and the hub endpoint OFF the tunnel, then send everything else through it
ip route replace ${MYIP:?}/32 via \$GW dev \$IFC
ip route replace $HUB/32 via \$GW dev \$IFC
ip route replace 0.0.0.0/1 dev wg-op; ip route replace 128.0.0.0/1 dev wg-op
# dead-man: drop the tunnel (and its routes) after 60 min no matter what
systemctl stop niblr-deadman.timer 2>/dev/null || true
systemd-run --unit niblr-deadman --on-active=60min /sbin/ip link del wg-op >/dev/null
echo client configured
CLEOF
echo "hub=$HUB e1=$E1 e2=$E2 client=$CL"
