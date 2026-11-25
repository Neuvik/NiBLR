#!/bin/bash
# Runs ON the operator machine (as root, in the background): the kill switch cuts SSH, so results go to a file.
# env: INV1 INV2 BADPIN HUBIP REALIP EXITS("ip ip ip") TGPRIV (private address of a host in the same VPC)
exec >/tmp/join-test.out 2>&1
fail=0; check() { if eval "$2"; then echo "PASS  $1"; else echo "FAIL  $1"; fail=1; fi; }
# dead-man: whatever happens, restore the machine after 25 minutes
systemd-run --unit niblr-deadman --on-active=25min /bin/sh -c '/usr/local/bin/niblr leave; iptables -F OUTPUT; ip6tables -F OUTPUT' >/dev/null 2>&1
myip() { curl -s -m 6 https://checkip.amazonaws.com | tr -d '\n'; }
IFC=$(ip -o -4 route show default | awk '{print $5}')

echo "== 0. before joining: we are visible at our own address"
b=$(myip); echo "   $b"; check "baseline is the machine's real address" '[ "$b" = "$REALIP" ]'

echo "== 1. error messages for bad invites (nothing may be changed)"
o=$(niblr join garbage 2>&1); echo "   $o"; check "garbage invite: plain message" '[[ "$o" == *"doesn'"'"'t look like a NiBLR invite"* ]]'
o=$(niblr join "$BADPIN" 2>&1); echo "   $o"; check "wrong server identity refused" '[[ "$o" == *"right NiBLR server"* ]]'
check "bad attempts left no config" '[ ! -e /etc/niblr/niblr.conf ]'

echo "== 2. join"
o=$(niblr join "$INV1" 2>&1); echo "   $o"; check "joined" '[[ "$o" == *"Joined"* ]]'
check "key file is private (0600)" '[ "$(stat -c %a /etc/niblr/niblr.conf)" = 600 ]'
check "kill-switch rules installed" 'iptables -S OUTPUT | grep -q REJECT && ip6tables -S OUTPUT | grep -q REJECT'

echo "== 3. traffic leaves from exit addresses, never our own"
seen=""; for i in $(seq 25); do seen="$seen $(myip)"; sleep 0.3; done; u=$(echo $seen | tr ' ' '\n' | sort | uniq -c | tr '\n' ' '); echo "   $u"
check "never our real address" '[[ "$seen" != *"$REALIP"* ]]'
n=$(echo $seen | tr ' ' '\n' | grep -c . ); check "every request answered" '[ $n -ge 24 ]'
d=$(echo $seen | tr ' ' '\n' | sort -u | grep -c .); echo "   distinct addresses: $d"; check "more than one exit address used" '[ $d -ge 2 ]'
for a in $(echo $seen | tr ' ' '\n' | sort -u); do check "address $a is one of the exits" '[[ " $EXITS " == *" $a "* ]]'; done
st=$(niblr status 2>&1); echo "   status: $st"; check "status reports connected" '[[ "$st" == Connected* ]]'

echo "== 3b. private and metadata destinations are blocked at the hub; public ones still work"
pub=$(myip); check "public traffic still works" '[ -n "$pub" ]'
check "private VPC address is unreachable through the tunnel" '! ping -c2 -W2 "$TGPRIV" >/dev/null 2>&1'
mc=$(curl -s -m 4 -o /dev/null -w '%{http_code}' http://169.254.169.254/latest/meta-data/ 2>/dev/null); echo "   metadata address answered: $mc"
check "cloud metadata address is unreachable through the tunnel" '[ "$mc" = 000 ]'
check "RFC1918 range 192.168.1.1 is unreachable through the tunnel" '! ping -c1 -W2 192.168.1.1 >/dev/null 2>&1'

echo "== 4. DNS resolves through the tunnel; nothing but WireGuard leaves the physical interface"
timeout 25 tcpdump -i $IFC -nn -Q out -l 'not arp and not (udp and dst host '$HUBIP' and dst port 51000)' > /tmp/leak.txt 2>/dev/null & TD=$!
sleep 1; for h in example.com wikipedia.org github.com; do getent hosts $h >/dev/null && echo "   resolved $h" || echo "   FAILED to resolve $h"; curl -s -m 6 -o /dev/null https://$h; done
dig +short +time=3 +tries=1 example.com @9.9.9.9 | head -1 | sed 's/^/   dig via tunnel: /'
sleep 3; kill $TD 2>/dev/null; wait $TD 2>/dev/null; l=$(grep -c . /tmp/leak.txt); echo "   non-WireGuard packets on $IFC: $l"; head -3 /tmp/leak.txt
check "no DNS or other traffic left the physical interface outside the tunnel" '[ "$l" = 0 ]'

echo "== 5. IPv6"
check "IPv6 default route points into the tunnel" 'ip -6 route show default 2>/dev/null | grep -q niblr || ! test -e /proc/net/if_inet6 || [ -z "$(ip -6 addr show scope global 2>/dev/null)" ]'
o=$(curl -6 -s -m 5 https://ipv6.icanhazip.com 2>&1); check "no IPv6 reachability that bypasses the tunnel" '[ -z "$o" ]'

echo "== 6. tunnel interface forced down: traffic must STOP, not fall back to our real address"
ip link set niblr down 2>/dev/null
seen=""; for i in 1 2 3 4; do seen="$seen $(curl -s -m 4 https://checkip.amazonaws.com | tr -d '\n')"; done; echo "   answers while down: [$seen]"
check "no answer and never our real address while the tunnel is down" '[[ "$seen" != *"$REALIP"* ]] && [ -z "$(echo $seen)" ]'
# `ip link set up` does NOT bring back wg-quick's routes; a real toggle is down+up
wg-quick down /etc/niblr/niblr.conf >/dev/null 2>&1; wg-quick up /etc/niblr/niblr.conf >/dev/null 2>&1; sleep 4
r=$(myip); echo "   after wg-quick down/up: $r"; check "toggling the tunnel restores service" '[ -n "$r" ] && [ "$r" != "$REALIP" ]'

echo "== 7. hub unreachable (its WireGuard port blocked): same rule"
iptables -I OUTPUT 1 -d $HUBIP -p udp --dport 51000 -j DROP
seen=""; for i in 1 2 3 4; do seen="$seen $(curl -s -m 4 https://checkip.amazonaws.com | tr -d '\n')"; done; echo "   answers while hub unreachable: [$seen]"
check "no answer and never our real address while the hub is unreachable" '[[ "$seen" != *"$REALIP"* ]] && [ -z "$(echo $seen)" ]'
iptables -D OUTPUT -d $HUBIP -p udp --dport 51000 -j DROP
for t in $(seq 30); do r=$(myip); [ -n "$r" ] && break; sleep 1; done; echo "   recovered after ~${t}s: $r"
check "recovers by itself, via an exit" '[ -n "$r" ] && [ "$r" != "$REALIP" ]'

echo "== 8. joining twice, and re-using an invite"
o=$(niblr join "$INV2" 2>&1); echo "   $o"; check "second join refused politely" '[[ "$o" == *"already joined"* ]]'

echo "== 9. leave"
o=$(niblr leave 2>&1); echo "   $o"; sleep 2
a=$(myip); echo "   address after leaving: $a"; check "normal connection restored" '[ "$a" = "$REALIP" ]'
check "kill-switch rules removed" '! iptables -S OUTPUT | grep -q REJECT'
check "key file deleted" '[ ! -e /etc/niblr/niblr.conf ]'
o=$(niblr join "$INV1" 2>&1); echo "   reuse of used invite: $o"; check "a used invite cannot be reused (even after leaving)" '[[ "$o" == *"already used or has expired"* ]]'
echo; [ $fail = 0 ] && echo ALL PASS || echo SOME FAILED
echo DONE
