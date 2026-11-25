#!/bin/bash
# Builds a rig in us-west-1 for managed-mode niblrd: hub (scoped IAM role), client, target. Exits are made by niblrd itself.
# Usage: RIG=<dir> WANT=3 ./rig-up.sh     Requires: aws CLI (admin), /tmp/niblr-bin, /tmp/niblrd-linux
set -euo pipefail
DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd); N=/tmp/niblr-bin
RIG=${RIG:?}; WANT=${WANT:-3}; R=us-west-1; export AWS_DEFAULT_REGION=$R AWS_PAGER=""; umask 077; mkdir -p $RIG; cd $RIG
MYIP=$(curl -s https://checkip.amazonaws.com); ACCT=$(aws sts get-caller-identity --query Account --output text)
$N init --env dev --region $R | tee net.txt
VPC=$(awk '/vpc /{print $2}' net.txt); SUBNET=$(awk '/subnet /{print $2}' net.txt); EXITSG=$(awk '/exit-sg/{print $2}' net.txt); HUBSG=$(awk '/hub-sg/{print $2}' net.txt)
AMI=$(aws ssm get-parameter --name /aws/service/canonical/ubuntu/server/24.04/stable/current/amd64/hvm/ebs-gp3/ami-id --query Parameter.Value --output text)
[ -f key ] || ssh-keygen -q -t ed25519 -N "" -f key
aws ec2 import-key-pair --key-name niblr-dev-rig --public-key-material fileb://key.pub --tag-specifications 'ResourceType=key-pair,Tags=[{Key=niblr-managed,Value=true}]' >/dev/null
cat > trust.json <<'J'
{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}
J
sed -e "s/REGION/$R/g" -e "s/ACCOUNT/$ACCT/g" -e "s/SUBNET_ID/$SUBNET/g" -e "s/SG_ID/$EXITSG/g" -e "s/\"ENV\"/\"dev\"/g" $DIR/hub-policy.json.tmpl > hub-policy.json
for role in niblr-dev-hub niblr-dev-ssm; do
  aws iam create-role --role-name $role --assume-role-policy-document file://trust.json --tags Key=niblr-managed,Value=true >/dev/null
  aws iam attach-role-policy --role-name $role --policy-arn arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore
  aws iam create-instance-profile --instance-profile-name $role >/dev/null; aws iam add-role-to-instance-profile --instance-profile-name $role --role-name $role
done
aws iam put-role-policy --role-name niblr-dev-hub --policy-name niblr-hub --policy-document file://hub-policy.json
T='ResourceType=security-group,Tags=[{Key=niblr-managed,Value=true},{Key=niblr-env,Value=dev}]'
RIGSG=$(aws ec2 create-security-group --group-name niblr-dev-rig --description "NiBLR dev rig: ssh from operator" --vpc-id $VPC --tag-specifications "$T" --query GroupId --output text)
TGSG=$(aws ec2 create-security-group --group-name niblr-dev-target --description "NiBLR dev test target" --vpc-id $VPC --tag-specifications "$T" --query GroupId --output text)
aws ec2 authorize-security-group-ingress --group-id $RIGSG --protocol tcp --port 22 --cidr $MYIP/32 >/dev/null
aws ec2 authorize-security-group-ingress --group-id $TGSG --ip-permissions 'IpProtocol=tcp,FromPort=8080,ToPort=8081,IpRanges=[{CidrIp=0.0.0.0/0}]' 'IpProtocol=udp,FromPort=9999,ToPort=9999,IpRanges=[{CidrIp=0.0.0.0/0}]' 'IpProtocol=icmp,FromPort=-1,ToPort=-1,IpRanges=[{CidrIp=0.0.0.0/0}]' >/dev/null
printf '#!/bin/bash\nshutdown -P +240\n' > ud.sh; sleep 10
launch() { n=$1; prof=$2; shift 2
  aws ec2 run-instances --image-id $AMI --instance-type t3.nano --key-name niblr-dev-rig --iam-instance-profile Name=$prof \
   --network-interfaces "DeviceIndex=0,SubnetId=$SUBNET,AssociatePublicIpAddress=true,Groups=$(echo $* | tr ' ' ',')" \
   --metadata-options HttpTokens=required --instance-initiated-shutdown-behavior terminate --user-data file://ud.sh \
   --tag-specifications "ResourceType=instance,Tags=[{Key=niblr-managed,Value=true},{Key=niblr-env,Value=dev-rig},{Key=niblr-role,Value=$n},{Key=Name,Value=niblr-dev-$n}]" "ResourceType=volume,Tags=[{Key=niblr-managed,Value=true},{Key=niblr-env,Value=dev-rig}]" \
   --query 'Instances[0].InstanceId' --output text; }
hub=$(launch hub niblr-dev-hub $HUBSG $RIGSG); client=$(launch client niblr-dev-ssm $RIGSG); target=$(launch target niblr-dev-ssm $TGSG $RIGSG)
printf 'RIGSG=%s\nTGSG=%s\nhub=%s\nclient=%s\ntarget=%s\nAMI=%s\nSUBNET=%s\nEXITSG=%s\n' $RIGSG $TGSG $hub $client $target $AMI $SUBNET $EXITSG > ids.env
aws ec2 wait instance-running --instance-ids $hub $client $target
ip_of() { aws ec2 describe-instances --instance-ids $1 --query 'Reservations[0].Instances[0].PublicIpAddress' --output text; }
HUB=$(ip_of $hub); CL=$(ip_of $client); TG=$(ip_of $target); printf 'HUB=%s\nCL=%s\nTG=%s\n' $HUB $CL $TG > ips.env
SSH="ssh -i key -o StrictHostKeyChecking=accept-new -o BatchMode=yes -o ConnectTimeout=8"; SCP="scp -q -i key -o BatchMode=yes"
for h in $HUB $CL $TG; do for t in $(seq 40); do $SSH ubuntu@$h true 2>/dev/null && break; sleep 5; done; done
for h in $HUB $CL $TG; do $SSH ubuntu@$h 'sudo apt-get update -qq >/dev/null 2>&1; sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq wireguard nftables conntrack >/dev/null 2>&1' & done; wait
read HK HP < <($N dev genkey)
# hub
$SCP /tmp/niblrd-linux ubuntu@$HUB:/tmp/niblrd; echo $HK | $SSH ubuntu@$HUB 'sudo mkdir -p /etc/niblr && sudo tee /etc/niblr/hub.key >/dev/null && sudo chmod 600 /etc/niblr/hub.key && sudo install -m755 /tmp/niblrd /usr/local/bin/niblrd'
$SSH ubuntu@$HUB "sudo bash -s" <<EOT
set -e
cat >/etc/systemd/system/niblrd.service <<UNIT
[Unit]
Description=NiBLR hub daemon
After=network-online.target
StartLimitIntervalSec=0
[Service]
Environment=AWS_REGION=$R
ExecStart=/usr/local/bin/niblrd --operator-iface wg-op --want $WANT --env dev --region $R --ami $AMI --subnet $SUBNET --exit-sg $EXITSG --hub-ip $HUB --lease-minutes 240
Restart=always
[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload; systemctl enable --now niblrd >/dev/null 2>&1; echo hub ready
EOT
# client: real operator machine. Only needs wireguard + iptables; joins with `niblr join`.
$SSH ubuntu@$CL 'sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq iptables dnsutils tcpdump >/dev/null 2>&1; true'
$SCP /tmp/niblr-bin ubuntu@$CL:/tmp/niblr; $SSH ubuntu@$CL 'sudo install -m755 /tmp/niblr /usr/local/bin/niblr'
$SCP $DIR/../dataplane/target.py ubuntu@$TG:/tmp/target.py
$SSH ubuntu@$TG 'sudo systemd-run --unit niblr-target python3 /tmp/target.py >/dev/null 2>&1; head -c 30000000 /dev/urandom > /tmp/big.bin; sha256sum /tmp/big.bin | cut -c1-16 > /tmp/big.sum; sudo systemd-run --unit niblr-big --working-directory=/tmp python3 -m http.server 8081 >/dev/null 2>&1; echo target ready'
echo RIG UP
