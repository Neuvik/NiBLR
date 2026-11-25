# AWS: hub role for a SHARED account (tested against a real account: see prototype/aws-go/iam-test.sh)

The account also holds things that are not NiBLR's. The rule: the hub can only create inside NiBLR's own
network and can only touch instances that carry NiBLR's tags. A popped hub must not be able to read, stop or
delete anyone else's resources.

## Preconditions (created by `niblr init`, using the admin's own credentials, not the hub's)

* A dedicated VPC (or at least a dedicated subnet + security group) tagged `niblr-managed=true`. Do NOT use the
  default VPC: resource-level permissions below are scoped to this subnet and SG.
* A permissions boundary on the hub role so it can never be escalated past this policy.
* A budget alarm and an instance-count cap enforced by the daemon (`max-exits`), because quotas are shared.

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

Deliberately absent: `iam:PassRole` (exits get no instance profile), `ec2:CreateTags` outside creation (a popped hub
could otherwise tag someone else's instance `niblr-managed=true` and then terminate it), and any wildcard action.

## Known residual risks (shared account)

* `Describe*` has no resource-level permissions, so a popped hub can enumerate every instance in the region.
  Acceptable only if instance names/tags in this account are not sensitive.
* Shared vCPU quota: NiBLR can starve other teams. Cap `max-exits`, alarm on spend, and request a quota
  separation if this ever gets large.
* The `niblr-managed` tag is the trust boundary. Anyone else with `ec2:CreateTags` in the account can make
  their own instances look like NiBLR's. Add a tag-policy / SCP denying `niblr-*` tag writes to everyone but
  the NiBLR admin role and the hub (needs an org admin).
