#!/bin/bash
# End-to-end test of the AWS rig. Run from prototype/aws with SCRATCH set. Failures are injected over SSH.
S=${SCRATCH:?}; KEY=$S/niblr-proto; J=$(terraform output -json ips)
ip_of() { echo "$J" | python3 -c "import json,sys;print(json.load(sys.stdin)['$1']['public'])"; }
HUB=$(ip_of hub); E1=$(ip_of exit1); E2=$(ip_of exit2); CL=$(ip_of client); TG=$(ip_of target)
ssh_() { local h=$1; shift; ssh -i $KEY -o BatchMode=yes -o ConnectTimeout=10 ubuntu@$h "$@"; }
scp -q -i $KEY -o BatchMode=yes ../dataplane/test.py ubuntu@$CL:/tmp/test.py
c() { ssh_ $CL "TARGET=$TG python3 /tmp/test.py $* 2>/dev/null"; }
hub() { ssh_ $HUB "$@"; }
fail=0; check() { if eval "$2"; then echo "PASS  $1"; else echo "FAIL  $1"; fail=1; fi; }
EXIT_IPS="$E1 $E2"
seen_both() { [[ "$1" == *"$E1"* && "$1" == *"$E2"* ]]; }

echo "== 1. 100 flows: both exits used, every flow completes"
out=$(c many 100); echo "   $out"
check "both exits seen, zero failures" 'seen_both "$out" && [[ "$out" != *FAIL* ]]'
hub 'for i in 1 2; do echo "   wg-e$i: $(sudo wg show wg-e$i transfer | awk "{print \"rx=\"\$2\" tx=\"\$3}")"; done'

echo "== 2. full L3: ICMP and UDP through the exits"
ssh_ $CL "ping -c3 -W3 $TG >/dev/null" && p=ok || p=fail
u=$(c udp); echo "   ping=$p udp=$u"
check "ping works" '[ $p = ok ]'; check "udp works and shows an exit address" '[[ "$u" == "src=$E1" || "$u" == "src=$E2" ]]'

echo "== 3. draining an exit never breaks an established flow"
for d in 1 2; do
  hub 'sudo systemctl stop niblr-health'
  hub 'sudo /usr/local/bin/niblr-health >/dev/null'
  (c long 8 > $S/niblr-long.out) & lp=$!; sleep 2
  hub "sudo DRAIN=$d /usr/local/bin/niblr-health >/dev/null"
  mid=$(c many 30); wait $lp; lo=$(cat $S/niblr-long.out)
  dip=$([ $d = 1 ] && echo $E1 || echo $E2)
  echo "   drain e$d: long-flow=$lo"; echo "   drain e$d: new flows=$mid"
  check "drain e$d: long flow unbroken on one exit" '[[ "$lo" != *BROKEN* && "$(echo "$lo" | grep -o "[0-9]*\.[0-9]*\.[0-9]*\.[0-9]*" | sort -u | wc -l)" = 1 ]]'
  check "drain e$d: new flows avoid it" '[[ "$mid" != *"$dip"* && "$mid" != *FAIL* ]]'
  hub 'sudo systemctl start niblr-health'; sleep 3
done

echo "== 4. exit2 dies (tunnel dropped on the exit): detection time and reroute"
ssh_ $E2 'sudo systemctl stop wg-quick@wg-hub'; t0=$(date +%s)
for t in $(seq 40); do h=$(hub 'sudo tail -1 /var/log/niblr-health.log'); [[ "$h" == *"healthy exits: 1" ]] && break; sleep 1; done
echo "   hub noticed after ~$(( $(date +%s) - t0 ))s: $h"
out=$(c many 40); echo "   $out"
check "only exit1, zero failures" '[[ "$out" == *"$E1"* && "$out" != *"$E2"* && "$out" != *FAIL* ]]'

echo "== 5. both exits dead: fail closed, nothing leaks"
ssh_ $E1 'sudo systemctl stop wg-quick@wg-hub'
for t in $(seq 40); do h=$(hub 'sudo tail -1 /var/log/niblr-health.log'); [[ "$h" == *"none" ]] && break; sleep 1; done
out=$(c many 4); echo "   $out"
check "all flows fail, no address ever seen" '[[ "$out" == *FAIL* && "$out" != *.*.*.* ]]'

echo "== 6. recovery"
ssh_ $E1 'sudo systemctl start wg-quick@wg-hub'; ssh_ $E2 'sudo systemctl start wg-quick@wg-hub'; t0=$(date +%s)
for t in $(seq 60); do h=$(hub 'sudo tail -1 /var/log/niblr-health.log'); [[ "$h" == *"healthy exits: 1 2" ]] && break; sleep 1; done
echo "   both back after ~$(( $(date +%s) - t0 ))s"
out=$(c many 60); echo "   $out"
check "both exits back, zero failures" 'seen_both "$out" && [[ "$out" != *FAIL* ]]'

echo "== 7. regression: exit1 keeps its tunnel but loses the return route (the bug the old probe missed)"
ssh_ $E1 'sudo ip route del 10.10.9.0/24 dev wg-hub'
for t in $(seq 20); do h=$(hub 'sudo tail -1 /var/log/niblr-health.log'); [[ "$h" == *"healthy exits: 2" ]] && break; sleep 1; done
echo "   hub saw: $h"; out=$(c many 30); echo "   $out"
check "broken exit pulled from pool, zero failures" '[[ "$h" == *"healthy exits: 2" && "$out" != *FAIL* && "$out" != *"$E1"* ]]'
ssh_ $E1 'sudo ip route add 10.10.9.0/24 dev wg-hub'
for t in $(seq 20); do h=$(hub 'sudo tail -1 /var/log/niblr-health.log'); [[ "$h" == *"healthy exits: 1 2" ]] && break; sleep 1; done
out=$(c many 40); echo "   after repair: $out"
check "repaired exit rejoins, zero failures" 'seen_both "$out" && [[ "$out" != *FAIL* ]]'
exit $fail
