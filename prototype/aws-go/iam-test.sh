#!/bin/bash
# Checks the hub's scoped IAM role from the hub itself, using --dry-run (nothing is created or deleted).
# Needs a rig from rig-up.sh with aws-cli installed on the hub (sudo snap install aws-cli --classic).
set -u; RIG=${RIG:?}; source $RIG/ips.env; source $RIG/ids.env
OTHER=$(aws --region us-west-1 ec2 describe-subnets --filters Name=isDefault,Values=true --query 'Subnets[0].SubnetId' --output text 2>/dev/null)
DV=$(aws --region us-west-1 ec2 describe-vpcs --filters Name=is-default,Values=true --query 'Vpcs[0].VpcId' --output text)
OTHER=$(aws --region us-west-1 ec2 describe-subnets --filters Name=vpc-id,Values=$DV --query 'Subnets[0].SubnetId' --output text)
DEFSG=$(aws --region us-west-1 ec2 describe-security-groups --filters Name=vpc-id,Values=$DV Name=group-name,Values=default --query 'SecurityGroups[0].GroupId' --output text)
ssh -i $RIG/key -o BatchMode=yes ubuntu@$HUB "sudo snap install aws-cli --classic >/dev/null 2>&1; true"
sed -e "s/@AMI@/$AMI/" -e "s/@SUBNET@/$SUBNET/" -e "s/@EXITSG@/$EXITSG/" -e "s/@OTHER@/$OTHER/" -e "s/@DEFSG@/$DEFSG/" -e "s/@CLIENT@/$client/" -e "s/@HUBID@/$hub/" <<'REMOTE' | ssh -i $RIG/key -o BatchMode=yes ubuntu@$HUB 'bash -s'
export AWS_DEFAULT_REGION=us-west-1 AWS_PAGER=""
AMI=@AMI@; SUBNET=@SUBNET@; EXITSG=@EXITSG@; OTHER=@OTHER@; DEFSG=@DEFSG@; CLIENT=@CLIENT@; HUBID=@HUBID@
EXITID=$(aws ec2 describe-instances --filters Name=tag:niblr-env,Values=dev Name=instance-state-name,Values=running --query 'Reservations[0].Instances[0].InstanceId' --output text)
TAGS='ResourceType=instance,Tags=[{Key=niblr-managed,Value=true},{Key=niblr-env,Value=dev}]'
t() { # name expect(ALLOW|DENY) cmd...
  n=$1; e=$2; shift 2; o=$("$@" --dry-run 2>&1); if echo "$o" | grep -q DryRunOperation; then r=ALLOW; elif echo "$o" | grep -qE "UnauthorizedOperation|AccessDenied"; then r=DENY; else r="OTHER: $(echo $o | cut -c1-120)"; fi
  [ "$r" = "$e" ] && echo "PASS  $n -> $r" || echo "FAIL  $n -> $r (wanted $e)"; }
RUN="aws ec2 run-instances --image-id $AMI --instance-type t3.nano --metadata-options HttpTokens=required"
t "launch a valid tagged exit into NiBLR subnet/SG" ALLOW $RUN --network-interfaces "DeviceIndex=0,SubnetId=$SUBNET,Groups=$EXITSG" --tag-specifications "$TAGS"
t "launch into the account's DEFAULT VPC subnet" DENY $RUN --network-interfaces "DeviceIndex=0,SubnetId=$OTHER,Groups=$DEFSG" --tag-specifications "$TAGS"
t "launch with NiBLR subnet but a foreign security group" DENY $RUN --network-interfaces "DeviceIndex=0,SubnetId=$SUBNET,Groups=$DEFSG" --tag-specifications "$TAGS"
t "launch untagged" DENY $RUN --network-interfaces "DeviceIndex=0,SubnetId=$SUBNET,Groups=$EXITSG"
t "launch tagged for another env" DENY $RUN --network-interfaces "DeviceIndex=0,SubnetId=$SUBNET,Groups=$EXITSG" --tag-specifications "ResourceType=instance,Tags=[{Key=niblr-managed,Value=true},{Key=niblr-env,Value=prod}]"
t "launch a big instance type" DENY aws ec2 run-instances --image-id $AMI --instance-type m5.4xlarge --metadata-options HttpTokens=required --network-interfaces "DeviceIndex=0,SubnetId=$SUBNET,Groups=$EXITSG" --tag-specifications "$TAGS"
t "launch asking for IMDSv1 (Ubuntu AMI defaults to v2, so omitting it is fine)" DENY aws ec2 run-instances --image-id $AMI --instance-type t3.nano --metadata-options HttpTokens=optional --network-interfaces "DeviceIndex=0,SubnetId=$SUBNET,Groups=$EXITSG" --tag-specifications "$TAGS"
t "terminate an exit of its own env" ALLOW aws ec2 terminate-instances --instance-ids $EXITID
t "terminate the client (other env tag)" DENY aws ec2 terminate-instances --instance-ids $CLIENT
t "terminate itself (hub, other env tag)" DENY aws ec2 terminate-instances --instance-ids $HUBID
t "tag the client as niblr-managed=dev (takeover attempt)" DENY aws ec2 create-tags --resources $CLIENT --tags Key=niblr-env,Value=dev
t "tag an exit after creation" DENY aws ec2 create-tags --resources $EXITID --tags Key=x,Value=y
t "stop an exit of its own env" ALLOW aws ec2 stop-instances --instance-ids $EXITID
t "create a VPC" DENY aws ec2 create-vpc --cidr-block 10.99.0.0/16
t "open a security group to the world" DENY aws ec2 authorize-security-group-ingress --group-id $EXITSG --protocol tcp --port 22 --cidr 0.0.0.0/0
o=$(aws iam list-roles 2>&1); echo "$o" | grep -q AccessDenied && echo "PASS  IAM: list roles -> DENY" || echo "FAIL  IAM: list roles"
t "describe instances (needed)" ALLOW aws ec2 describe-instances
REMOTE
