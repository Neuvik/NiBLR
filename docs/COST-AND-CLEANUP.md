# Cost and cleanup

## What costs money

* Exits are small instances (`t3.nano` by default) with a public IPv4 address each, billed while they exist.
* The hub is one small always-on instance with its Elastic IP.
* Data transfer out of AWS is billed per gigabyte. Heavy scans or large downloads through the exits are the main cost
  risk.
* The VPC, subnet, internet gateway, and security groups have no charge.

We have not recorded current prices here. They change and vary by region, so check the AWS pricing page for your region
and create a budget alarm before you start. Stopping an exit does not save money. A stopped instance still bills for its
disk, and NiBLR treats a stopped exit as broken and replaces it.

## Safety nets

* `--lease-minutes` makes each exit shut itself down and terminate after the lease, even if the hub is gone. It is off
  by default. Without it, a dead hub leaves its exits running and billing.
* The hub's role can launch only small instance types into the NiBLR subnet.
* Exits, their disks and network interfaces, and the network that `niblr init` creates carry the tag
  `niblr-managed=true`, so you can find them in the console or CLI. The hub, its IAM role, and its Elastic IP are not
  tagged unless you tag them.

## Full teardown

Work in this order.

1. Terminate the hub. Otherwise it recreates exits as you delete them.

       aws ec2 terminate-instances --instance-ids <hub-instance-id>

2. Terminate the remaining exits. List them with `niblr dev exit-list --env <env> --region <region>`, and delete each
   with `niblr dev exit-delete --env <env> --region <region> --id <instance-id>`. Alternatively, terminate every
   instance that has the tags `niblr-managed=true` and your `niblr-env`.
3. Release the hub's Elastic IP and delete its IAM resources. Remove the role from the instance profile, delete the
   inline policy, then delete the instance profile and the role.
4. Wait for all instances to finish terminating, because a security group cannot be deleted while instances use it. Then
   delete the network in this order: the two security groups, the subnet, the internet gateway (detach it from the VPC
   first), and finally the VPC. The route table is removed with the VPC.
5. Verify that nothing is left.

       aws ec2 describe-instances --filters Name=tag:niblr-managed,Values=true \
         Name=instance-state-name,Values=pending,running,stopping,stopped \
         --query 'length(Reservations[].Instances[])'
       aws ec2 describe-volumes --filters Name=tag:niblr-managed,Values=true --query 'length(Volumes)'

   Both commands should print `0`. These filters only see tagged resources, so also check that the hub instance and its
   Elastic IP are gone. List key pairs and IAM roles whose names contain `niblr` as well.

`prototype/aws-go/rig-down.sh` performs steps 1 to 3 for the test rig and runs similar checks. It leaves the network
from `niblr init` in place. It is written for the test rig and deletes things by name, so read it before running it
anywhere else.
