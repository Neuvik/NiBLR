#!/bin/bash
# Tears down a rig from rig-up.sh and verifies nothing is left. Hub goes first so niblrd cannot recreate exits.
set -u; RIG=${RIG:?}; source $RIG/ids.env; export AWS_DEFAULT_REGION=us-west-1 AWS_PAGER=""; N=/tmp/niblr-bin
aws ec2 terminate-instances --instance-ids $hub >/dev/null; aws ec2 wait instance-terminated --instance-ids $hub
for i in 1 2 3; do for id in $($N dev exit-list | awk '{print $1}'); do $N dev exit-delete --id $id >/dev/null; done; sleep 3; done
aws ec2 terminate-instances --instance-ids $client $target >/dev/null
for id in $(aws ec2 describe-instances --filters Name=tag:niblr-managed,Values=true Name=instance-state-name,Values=pending,running,shutting-down,stopping,stopped --query 'Reservations[].Instances[].InstanceId' --output text); do aws ec2 wait instance-terminated --instance-ids $id; done
aws ec2 delete-security-group --group-id $RIGSG >/dev/null; aws ec2 delete-security-group --group-id $TGSG >/dev/null
aws ec2 delete-key-pair --key-name niblr-dev-rig >/dev/null
for r in niblr-dev-hub niblr-dev-ssm; do
  aws iam remove-role-from-instance-profile --instance-profile-name $r --role-name $r; aws iam delete-instance-profile --instance-profile-name $r
  aws iam detach-role-policy --role-name $r --policy-arn arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore
  aws iam delete-role-policy --role-name $r --policy-name niblr-hub 2>/dev/null; aws iam delete-role --role-name $r
done
echo "--- verify (all should be 0)"
echo "instances: $(aws ec2 describe-instances --filters Name=tag:niblr-managed,Values=true Name=instance-state-name,Values=pending,running,stopping,stopped --query 'length(Reservations[].Instances[])')"
echo "volumes:   $(aws ec2 describe-volumes --filters Name=tag:niblr-managed,Values=true --query 'length(Volumes)')"
echo "keypairs:  $(aws ec2 describe-key-pairs --query 'KeyPairs[?KeyName==`niblr-dev-rig`]|length(@)')"
echo "roles:     $(aws iam list-roles --query 'Roles[?contains(RoleName,`niblr`)]|length(@)')  profiles: $(aws iam list-instance-profiles --query 'InstanceProfiles[?contains(InstanceProfileName,`niblr`)]|length(@)')"
echo "sgs left (network only): $(aws ec2 describe-security-groups --filters Name=tag:niblr-managed,Values=true --query 'SecurityGroups[].GroupName' --output text)"
