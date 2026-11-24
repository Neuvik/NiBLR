#!/bin/bash
# End-to-end test of the real niblrd on a rig built by `niblr` (us-west-1). Failures are injected with SSM
# (exits have no inbound ports). Needs: RIG dir with key, ips.env, ids.env; /tmp/niblr-bin; aws CLI.
set -u
RIG=${RIG:?}; source $RIG/ips.env; source $RIG/ids.env; export AWS_DEFAULT_REGION=us-west-1 AWS_PAGER=""
SSH="ssh -i $RIG/key -o BatchMode=yes -o ConnectTimeout=10"
declare -A EIP EID; while read id slot st ip; do EIP[$slot]=$ip; EID[$slot]=$id; done < <(/tmp/niblr-bin dev exit-list)
E1=${EIP[1]}; E2=${EIP[2]}
c()   { $SSH ubuntu@$CL "TARGET=$TG python3 /tmp/test.py $* 2>/dev/null"; }
hub() { $SSH ubuntu@$HUB "$@"; }
last() { hub 'sudo journalctl -u niblrd -n1 --no-pager -o cat' | sed 's/^[0-9/]* [0-9:]* //'; }
ssm() { local id=$1; shift; local cid=$(aws ssm send-command --instance-ids $id --document-name AWS-RunShellScript --parameters "commands=[\"$*\"]" --query Command.CommandId --output text); aws ssm wait command-executed --command-id $cid --instance-id $id 2>/dev/null; }
waitpool() { for t in $(seq ${2:-60}); do [[ "$(last)" == "healthy exits: $1" ]] && return 0; sleep 1; done; return 1; }
setdrain() { hub "sudo mkdir -p /etc/systemd/system/niblrd.service.d; printf '[Service]\nExecStart=\nExecStart=/usr/local/bin/niblrd --operator-iface wg-op --exits 1,2 --drain $1\n' | sudo tee /etc/systemd/system/niblrd.service.d/drain.conf >/dev/null; sudo systemctl daemon-reload; sudo systemctl restart niblrd"; }
cleardrain() { hub 'sudo rm -f /etc/systemd/system/niblrd.service.d/drain.conf; sudo systemctl daemon-reload; sudo systemctl restart niblrd'; waitpool "1 2" 30; }
fail=0; check() { if eval "$2"; then echo "PASS  $1"; else echo "FAIL  $1"; fail=1; fi; }
seen_both() { [[ "$1" == *"$E1"* && "$1" == *"$E2"* ]]; }
waitpool "1 2" 30

echo "== 1. 100 flows: both exits used, every flow completes"
out=$(c many 100); echo "   $out"
check "both exits seen, zero failures" 'seen_both "$out" && [[ "$out" != *FAIL* ]]'

echo "== 2. full L3: ICMP and UDP"
$SSH ubuntu@$CL "ping -c3 -W3 $TG >/dev/null" && p=ok || p=fail; u=$(c udp); echo "   ping=$p udp=$u"
check "ping works" '[ $p = ok ]'; check "udp shows an exit address" '[[ "$u" == "src=$E1" || "$u" == "src=$E2" ]]'

echo "== 3. drain never breaks an established flow"
for d in 1 2; do
  (c long 8 > $RIG/long.out) & lp=$!; sleep 2
  setdrain $d; waitpool "$([ $d = 1 ] && echo 2 || echo 1)" 20
  mid=$(c many 30); wait $lp; lo=$(cat $RIG/long.out); dip=$([ $d = 1 ] && echo $E1 || echo $E2)
  echo "   drain e$d: long=$lo  new=$mid"
  check "drain e$d: long flow unbroken on one exit" '[[ "$lo" != *BROKEN* && "$(echo "$lo" | grep -o "[0-9]*\.[0-9]*\.[0-9]*\.[0-9]*" | sort -u | wc -l)" = 1 ]]'
  check "drain e$d: new flows avoid it" '[[ "$mid" != *"$dip"* && "$mid" != *FAIL* ]]'
  cleardrain
done

echo "== 4. exit2 loses its tunnel: detection time and reroute"
ssm ${EID[2]} "systemctl stop wg-quick@wg-hub"; t0=$(date +%s)
waitpool "1" 40; echo "   noticed after ~$(( $(date +%s) - t0 ))s (incl. SSM latency): $(last)"
out=$(c many 40); echo "   $out"
check "only exit1, zero failures" '[[ "$out" == *"$E1"* && "$out" != *"$E2"* && "$out" != *FAIL* ]]'

echo "== 5. both exits dead: fail closed"
ssm ${EID[1]} "systemctl stop wg-quick@wg-hub"; waitpool "none" 40
out=$(c many 4); echo "   $out"
check "all flows fail, no address ever seen" '[[ "$out" == *FAIL* && "$out" != *.*.*.* ]]'

echo "== 6. recovery"
ssm ${EID[1]} "systemctl start wg-quick@wg-hub"; ssm ${EID[2]} "systemctl start wg-quick@wg-hub"; t0=$(date +%s)
waitpool "1 2" 60; echo "   both back after ~$(( $(date +%s) - t0 ))s"
out=$(c many 60); echo "   $out"; check "both exits back, zero failures" 'seen_both "$out" && [[ "$out" != *FAIL* ]]'

echo "== 7. regression: tunnel up, return route lost"
ssm ${EID[1]} "ip route del 10.10.9.0/24 dev wg-hub"; waitpool "2" 30; echo "   $(last)"
out=$(c many 30); echo "   $out"; check "broken exit pulled from pool, zero failures" '[[ "$out" != *"$E1"* && "$out" != *FAIL* ]]'
ssm ${EID[1]} "ip route add 10.10.9.0/24 dev wg-hub"; waitpool "1 2" 30
out=$(c many 40); echo "   $out"; check "repaired exit rejoins" 'seen_both "$out" && [[ "$out" != *FAIL* ]]'

echo "== 8. large payload (MTU): 30MB download through the tunnels, checksum must match"
want=$($SSH ubuntu@$TG 'cat /tmp/big.sum'); got=$($SSH ubuntu@$CL "curl -s -m 60 http://$TG:8081/big.bin | sha256sum | cut -c1-16"); echo "   want=$want got=$got"
check "30MB arrives intact" '[ "$want" = "$got" ]'
echo "   path MTU probe (DF ping):"; for s in 1372 1392 1400; do $SSH ubuntu@$CL "ping -c1 -W2 -M do -s $s $TG >/dev/null 2>&1" && echo "     payload $s ok" || echo "     payload $s FAILS"; done

echo "== 9. many connections: 3000 short flows, 150 at a time"
ok=$($SSH ubuntu@$CL "seq 3000 | xargs -P150 -I{} curl -s -m 15 -o /dev/null -w '%{http_code}\n' http://$TG:8081/ 2>/dev/null | sort | uniq -c | tr '\n' ' '"); echo "   $ok"
echo "   hub conntrack: $(hub 'cat /proc/sys/net/netfilter/nf_conntrack_count') / $(hub 'cat /proc/sys/net/netfilter/nf_conntrack_max')"
check "no failed connections" '[[ "$ok" == *"3000 200"* ]]'
echo; [ $fail = 0 ] && echo ALL PASS || echo SOME FAILED
