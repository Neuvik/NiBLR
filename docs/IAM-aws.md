# AWS: the hub's IAM role for a shared account

This policy was tested against a real account with `prototype/aws-go/iam-test.sh`.

The account may also hold resources that are not NiBLR's. The rule is that the hub can create only inside NiBLR's own
network and can touch only instances that carry NiBLR's tags. A compromised hub must not be able to stop or delete
anyone else's resources.

## Preconditions

`niblr init` creates the network using the admin's own credentials, not the hub's. It creates a dedicated VPC, subnet,
internet gateway, and two security groups, all tagged `niblr-managed=true` and `niblr-env=<env>`. Do not use the default
VPC, because the resource-level permissions below are scoped to this subnet and these security groups.

`niblr init` does not create the following. Set them up by hand.

* The hub's IAM role and instance profile, with the policy below. `prototype/aws-go/rig-up.sh` shows the commands.
* A permissions boundary on the hub role, so it can never be escalated past this policy. This is recommended.
* A budget alarm. The number of exits is capped only by the `--want` flag, and quotas are shared.

## Policy

    {
      "Version": "2012-10-17",
      "Statement": [
        { "Sid": "ReadOnlyDiscovery", "Effect": "Allow",
          "Action": ["ec2:DescribeInstances", "ec2:DescribeTags", "ec2:DescribeSubnets", "ec2:DescribeSecurityGroups", "ec2:DescribeImages"],
          "Resource": "*" },
        { "Sid": "LaunchOnlyIntoNiblrNetwork", "Effect": "Allow", "Action": "ec2:RunInstances",
          "Resource": [
            "arn:aws:ec2:REGION:ACCOUNT:subnet/SUBNET_ID",
            "arn:aws:ec2:REGION:ACCOUNT:security-group/SG_ID",
            "arn:aws:ec2:REGION::image/*",
            "arn:aws:ec2:REGION:ACCOUNT:network-interface/*",
            "arn:aws:ec2:REGION:ACCOUNT:volume/*" ] },
        { "Sid": "InstancesMustBeTaggedAndSmall", "Effect": "Allow", "Action": "ec2:RunInstances",
          "Resource": "arn:aws:ec2:REGION:ACCOUNT:instance/*",
          "Condition": {
            "StringEquals": {
              "aws:RequestTag/niblr-managed": "true",
              "aws:RequestTag/niblr-env": "ENV",
              "ec2:MetadataHttpTokens": "required" },
            "StringLike": { "ec2:InstanceType": ["t3.nano", "t3.micro", "t4g.nano", "t4g.micro"] } } },
        { "Sid": "TagOnlyAtCreate", "Effect": "Allow", "Action": "ec2:CreateTags",
          "Resource": "arn:aws:ec2:REGION:ACCOUNT:*/*",
          "Condition": { "StringEquals": { "ec2:CreateAction": "RunInstances" } } },
        { "Sid": "OnlyTouchOwnInstances", "Effect": "Allow",
          "Action": ["ec2:TerminateInstances", "ec2:StopInstances"],
          "Resource": "arn:aws:ec2:REGION:ACCOUNT:instance/*",
          "Condition": { "StringEquals": { "ec2:ResourceTag/niblr-managed": "true", "ec2:ResourceTag/niblr-env": "ENV" } } }
      ]
    }

Deliberately absent: `iam:PassRole` (exits get no instance profile), `ec2:CreateTags` outside creation (a compromised hub
could otherwise tag someone else's instance `niblr-managed=true` and then terminate it), and any wildcard action.

## Known residual risks (shared account)

* `Describe*` has no resource-level permissions, so a compromised hub can enumerate every instance in the region.
  Acceptable only if instance names and tags in the account are not sensitive.
* Shared vCPU quota: NiBLR can starve other teams. Keep `--want` small, alarm on spend, and request a separate quota if
  usage grows.
* The `niblr-managed` tag is the trust boundary. Anyone else with `ec2:CreateTags` in the account can make
  their own instances look like NiBLR's. Add a tag policy or service control policy (SCP) denying `niblr-*` tag writes to everyone but
  the NiBLR admin role and the hub (this requires an AWS Organizations administrator).
