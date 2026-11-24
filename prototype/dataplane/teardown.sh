#!/bin/bash
source "$(dirname "$0")/lib.sh"
pkill -f "[p]ython3 .*dataplane/target.py|[p]ython3 ./target.py" 2>/dev/null || true
rm -f /tmp/niblr-target.pid
for ns in $NS_CLIENT $NS_HUB $NS_NET "${NS_EXITS[@]}"; do ip netns del $ns 2>/dev/null || true; done
