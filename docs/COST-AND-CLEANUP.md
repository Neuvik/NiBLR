# Cost and cleanup

## What costs money

| Item | Notes |
|---|---|
| Exits | Small instances (`t3.nano` by default) plus a public IPv4 address each, billed while they exist |
| Hub | One small always-on instance plus its public IP |
| Data transfer | Traffic leaving AWS is billed per GB. Heavy scans or big downloads through the exits are the main cost risk |
| Network setup | The VPC, subnet, gateway and security groups are free |

Rough order of magnitude: **a few dollars per month per always-on exit** (instance plus public IPv4), plus the hub,
plus data transfer. Prices change and vary by region; check the current AWS pricing page and set a **budget alarm**
before you start. Exits can't be made cheaper by stopping them: a stopped instance still bills for its disk and NiBLR
treats a stopped exit as broken and replaces it.

## Built-in safety nets

* `--lease-minutes`: every exit terminates itself after that long, even if the hub is gone.
* The hub's role can only launch small instance types into the NiBLR subnet.
* Everything is tagged `niblr-managed=true` so it is easy to find.

## Full teardown (in this order)

1. **Terminate the hub** (otherwise it recreates exits): `aws ec2 terminate-instances --instance-ids <hub>`
2. **Terminate remaining exits** (tag `niblr-managed=true`, `niblr-env=<env>`).
3. Delete the hub's IAM instance profile, role and policy.
4. Delete the security groups, then the VPC pieces (`niblr init` made them; delete in reverse dependency order).
5. **Verify nothing is left:**

       aws ec2 describe-instances --filters Name=tag:niblr-managed,Values=true \
         Name=instance-state-name,Values=pending,running,stopping,stopped --query 'length(Reservations[].Instances[])'
       aws ec2 describe-volumes --filters Name=tag:niblr-managed,Values=true --query 'length(Volumes)'

   Both should print `0`. Also check for leftover key pairs and IAM roles with `niblr` in the name.

`prototype/aws-go/rig-down.sh` does all of this for the test rig and prints these checks. Use it as the reference.
