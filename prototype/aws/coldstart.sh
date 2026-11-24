#!/bin/bash
# Repeats: bounce both exit tunnels -> wait until hub says healthy -> time the first flows from the client.
S=${SCRATCH:?}; KEY=$S/niblr-proto; J=$(terraform output -json ips)
ip_of() { echo "$J" | python3 -c "import json,sys;print(json.load(sys.stdin)['$1']['public'])"; }
HUB=$(ip_of hub); E1=$(ip_of exit1); E2=$(ip_of exit2); CL=$(ip_of client); TG=$(ip_of target)
ssh_() { local h=$1; shift; ssh -i $KEY -o BatchMode=yes -o ConnectTimeout=10 ubuntu@$h "$@"; }
cat > $S/probe.py <<PY
import socket,time,sys
for i in range(4):
    t=time.time()
    try:
        s=socket.create_connection(("$TG",8080),timeout=8); f=s.makefile("rw"); f.write("x\n"); f.flush(); r=f.readline().strip(); s.close()
    except Exception as e: r="FAIL "+type(e).__name__
    print(i, round(time.time()-t,2), r)
PY
scp -q -i $KEY -o BatchMode=yes $S/probe.py ubuntu@$CL:/tmp/probe.py
bad=0
for n in $(seq ${1:-25}); do
  ssh_ $E1 'sudo systemctl restart wg-quick@wg-hub' & ssh_ $E2 'sudo systemctl restart wg-quick@wg-hub' & wait
  for t in $(seq 30); do h=$(ssh_ $HUB 'sudo tail -1 /var/log/niblr-health.log'); [[ "$h" == *"healthy exits: 1 2" ]] && break; sleep 1; done
  out=$(ssh_ $CL 'python3 /tmp/probe.py')
  slow=$(echo "$out" | awk '$2>1.0 || /FAIL/' )
  if [ -n "$slow" ]; then bad=$((bad+1)); echo "trial $n (healthy after ${t}s): SLOW/FAIL"; echo "$out" | sed 's/^/    /'; fi
done
echo "trials with a slow/failed first flows: $bad/${1:-25}"
