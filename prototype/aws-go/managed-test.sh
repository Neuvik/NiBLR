#!/bin/bash
# Managed-mode niblrd on a rig from rig-up.sh. Kills/changes things from outside (admin creds) and checks the hub heals.
set -u
RIG=${RIG:?}; source $RIG/ips.env; source $RIG/ids.env; export AWS_DEFAULT_REGION=us-west-1 AWS_PAGER=""; N=/tmp/niblr-bin
SSH="ssh -i $RIG/key -o BatchMode=yes -o ConnectTimeout=10"
c()   { $SSH ubuntu@$CL "TARGET=$TG python3 /tmp/test.py $* 2>/dev/null"; }
hub() { $SSH ubuntu@$HUB "$@"; }
last() { hub 'sudo journalctl -u niblrd --no-pager -o cat | grep "healthy exits" | tail -1' | sed 's/^[0-9/]* [0-9:]* //'; }
waitpool() { for t in $(seq ${2:-90}); do [[ "$(last)" == "healthy exits: $1" ]] && return 0; sleep 2; done; return 1; }
running() { $N dev exit-list | grep -c -E "running|pending"; }
fail=0; check() { if eval "$2"; then echo "PASS  $1"; else echo "FAIL  $1"; fail=1; fi; }
waitpool "1 2 3" 30

echo "== A. three exits, flows spread across all three addresses"
out=$(c many 90); echo "   $out"; check "3 distinct addresses, zero failures" '[ $(echo "$out" | grep -o "src=[0-9.]*" | sort -u | wc -l) = 3 ] && [[ "$out" != *FAIL* ]]'
declare -A OLD; while read id slot st ip; do OLD[$slot]="$id $ip"; done < <($N dev exit-list)

echo "== B. rotate: kill exit 2 from outside; hub must replace it by itself"
$N dev exit-delete --id ${OLD[2]%% *} >/dev/null; t0=$(date +%s)
fails=0; total=0
while ! [[ "$(last)" == "healthy exits: 1 2 3" && $(( $(date +%s) - t0 )) -gt 20 ]]; do
  o=$(c many 10); total=$((total+10)); n=$(echo "$o" | grep -o "FAIL': [0-9]*" | grep -o "[0-9]*"); fails=$((fails+${n:-0}))
  [ $(( $(date +%s) - t0 )) -gt 240 ] && break
done
echo "   healed after ~$(( $(date +%s) - t0 ))s; flows during rotation: $total attempted, $fails failed"
NEWIP=$($N dev exit-list | awk '$2==2{print $4}'); echo "   slot 2: ${OLD[2]#* } -> $NEWIP"
check "slot 2 has a new address" '[ -n "$NEWIP" ] && [ "$NEWIP" != "${OLD[2]#* }" ]'
check "exactly 3 exits, no duplicates" '[ "$(running)" = 3 ]'
out=$(c many 90); echo "   $out"; check "all three used again, zero failures" '[ $(echo "$out" | grep -o "src=[0-9.]*" | sort -u | wc -l) = 3 ] && [[ "$out" != *FAIL* ]] && [[ "$out" == *"$NEWIP"* ]]'
check "flow failures during rotation stayed small (<=5%)" '[ $((fails*20)) -le $total ]'

echo "== C. interfaces wiped on the hub: rebuilt from tags, no new instances"
before=$($N dev exit-list | awk '{print $1}' | sort | tr '\n' ' ')
hub 'sudo ip link del wg-e1; sudo ip link del wg-e3'; sleep 2
waitpool "2" 20; waitpool "1 2 3" 60
after=$($N dev exit-list | awk '{print $1}' | sort | tr '\n' ' ')
check "same instances, pool restored" '[ "$before" = "$after" ] && [[ "$(last)" == "healthy exits: 1 2 3" ]]'
out=$(c many 60); check "flows fine after rebuild" '[[ "$out" != *FAIL* ]]'

echo "== D. scale down to 2"
hub "sudo mkdir -p /etc/systemd/system/niblrd.service.d; sudo sed -n 's/^ExecStart=//p' /etc/systemd/system/niblrd.service | sed 's/--want 3/--want 2/' | sudo tee /tmp/exec >/dev/null; printf '[Service]\nExecStart=\nExecStart=%s\n' \"\$(cat /tmp/exec)\" | sudo tee /etc/systemd/system/niblrd.service.d/want.conf >/dev/null; sudo systemctl daemon-reload; sudo systemctl restart niblrd"
for t in $(seq 40); do [ "$(running)" = 2 ] && break; sleep 3; done; waitpool "1 2" 30
for t in $(seq 20); do [ "$(hub "ip -o link | grep -c wg-e3")" = 0 ] && break; sleep 2; done  # interface cleanup lags the (immediate) pool exit by up to one reconcile
echo "   exits: $($N dev exit-list | awk '{print $2}' | tr '\n' ' ') pool: $(last)"
check "two exits remain, pool matches, interface gone" '[ "$(running)" = 2 ] && [[ "$(last)" == "healthy exits: 1 2" ]] && [ "$(hub "ip -o link | grep -c wg-e3")" = 0 ]'
out=$(c many 40); check "flows fine at 2 exits" '[[ "$out" != *FAIL* ]]'
echo; [ $fail = 0 ] && echo ALL PASS || echo SOME FAILED
