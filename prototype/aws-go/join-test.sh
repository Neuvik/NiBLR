#!/bin/bash
# Orchestrates the operator-experience test on a rig from rig-up.sh.
set -u; RIG=${RIG:?}; source $RIG/ips.env; export AWS_DEFAULT_REGION=us-west-1 AWS_PAGER=""; N=/tmp/niblr-bin
SSH="ssh -i $RIG/key -o BatchMode=yes -o ConnectTimeout=10"; DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
inv() { $SSH ubuntu@$HUB "sudo niblrd invite --name $1 --hub-ip $HUB" | grep '^niblr1:'; }
for t in $(seq 30); do $SSH ubuntu@$HUB 'sudo ss -ltn | grep -q :8443' && break; sleep 2; done
for t in $(seq 60); do [ "$($N dev exit-list | grep -c running)" = 3 ] && break; sleep 3; done
INV1=$(inv op1); INV2=$(inv op2); INV3=$(inv op3)
# a copy of invite 3 whose certificate pin is wrong (a man-in-the-middle stand-in)
BADPIN=$(python3 - "$INV3" <<'PY'
import sys,base64,json
s=sys.argv[1][7:]; d=json.loads(base64.urlsafe_b64decode(s+"="*(-len(s)%4))); d["f"]="0"*64
print("niblr1:"+base64.urlsafe_b64encode(json.dumps(d).encode()).decode().rstrip("="))
PY
)
REALIP=$($SSH ubuntu@$CL 'curl -s -m 6 https://checkip.amazonaws.com' | tr -d '\n')
EXITS=$($N dev exit-list | awk '{print $4}' | tr '\n' ' ')
echo "client real address: $REALIP   exits: $EXITS"
for t in $(seq 40); do $SSH ubuntu@$HUB 'sudo journalctl -u niblrd --no-pager -o cat | tail -1' | grep -q "1 2 3" && break; sleep 3; done
scp -q -i $RIG/key $DIR/client-test.sh ubuntu@$CL:/tmp/client-test.sh
$SSH ubuntu@$CL "sudo env INV1='$INV1' INV2='$INV2' BADPIN='$BADPIN' HUBIP=$HUB REALIP=$REALIP EXITS='$EXITS' nohup bash /tmp/client-test.sh >/dev/null 2>&1 &"
echo "client test started; waiting (SSH to the client is cut off while joined)..."
sleep 60
for t in $(seq 60); do out=$($SSH ubuntu@$CL 'cat /tmp/join-test.out 2>/dev/null' 2>/dev/null) && [[ "$out" == *DONE* ]] && break; sleep 10; done
echo "$out" | grep -v '^DONE'
