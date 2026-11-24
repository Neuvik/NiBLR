#!/bin/bash
source "$(dirname "$0")/lib.sh"
D=$(dirname "$0"); fail=0
c() { nsx $NS_CLIENT python3 $D/test.py "$@"; }
check() { if eval "$2"; then echo "PASS  $1"; else echo "FAIL  $1"; fail=1; fi; }

echo "== 1. both exits used, every flow completes (symmetry)"
out=$(c many 60); echo "   $out"
check "both exits seen, zero failures" '[[ "$out" == *203.0.113.11* && "$out" == *203.0.113.12* && "$out" != *FAIL* ]]'
echo "   per-exit tunnel counters (rx and tx must both be non-zero on each):"
for i in 1 2; do nsx $NS_HUB ip -s link show wg-e$i | awk 'NR==4||NR==6{printf "   wg-e'$i' %s\n",$1}'; done

echo "== 2. ICMP and UDP work (full L3)"
nsx $NS_CLIENT ping -c2 -W2 203.0.113.1 >/dev/null && p=ok || p=fail
u=$(c udp 2>&1 || true); echo "   ping=$p udp=$u"
check "ping works" '[ $p = ok ]'; check "udp works, source is an exit" '[[ "$u" == src=203.0.113.1[12] ]]'

echo "== 3. draining an exit from the pool never breaks established flows"
for d in 1 2; do
  c long 4 > /tmp/niblr-long.out & lp=$!
  sleep 1; DRAIN=$d $D/healthcheck.sh >/dev/null
  mid=$(c many 20); wait $lp; out=$(cat /tmp/niblr-long.out); echo "   drain e$d: long-flow=$out  new-flows=$mid"
  check "drain e$d: long flow unbroken on a single exit" '[[ "$out" != *BROKEN* ]] && [ $(echo "$out" | grep -o 203.0.113 | wc -l) = 1 ]'
  check "drain e$d: new flows avoid e$d" '[[ "$mid" != *203.0.113.1$d* && "$mid" != *FAIL* ]]'
  $D/healthcheck.sh >/dev/null
done

echo "== 4. exit2 is down: new flows all go via exit1"
$D/healthcheck.sh; out=$(c many 30); echo "   $out"
check "only exit1, zero failures" '[[ "$out" == *203.0.113.11* && "$out" != *203.0.113.12* && "$out" != *FAIL* ]]'

echo "== 5. both exits down: fail closed (nothing leaves, nothing leaks)"
nsx $NS_HUB ip link set wg-e1 down; sleep 1; $D/healthcheck.sh
out=$(c many 3); echo "   $out"; check "all flows fail" '[[ "$out" == *FAIL* && "$out" != *src=* ]]'

echo "== 6. recovery"
nsx $NS_HUB ip link set wg-e1 up; nsx $NS_HUB ip link set wg-e2 up; for t in $(seq 30); do sleep 1; h=$($D/healthcheck.sh); [[ "$h" == *"1 2" ]] && break; done; echo "   recovered after ${t}s: $h"
out=$(c many 40); echo "   $out"
check "both exits back, zero failures" '[[ "$out" == *203.0.113.11* && "$out" == *203.0.113.12* && "$out" != *FAIL* ]]'
exit $fail
