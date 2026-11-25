# NiBLR admin guide (AWS)

This guide covers how to set up a NiBLR environment and keep it healthy. A tester is the person who routes traffic
through NiBLR, and the tester guide covers their side.

> **Status:** every step below has been run on a real AWS account, but the hub is still set up by hand. There is no
> installer or machine image. `prototype/aws-go/rig-up.sh` automates most of these steps, so read it alongside this
> guide.

## 1. Plan

* A dedicated AWS account is best. If the account is shared, the hub role can touch only resources tagged for its
  environment, but service quotas are still shared. Read [IAM-aws.md](IAM-aws.md) first.
* Choose a region close to your testers or your target. The examples use `us-west-1`.
* Choose an environment name, for example `myteam`. Every resource NiBLR creates carries it as a tag, and a hub manages
  only resources with its own environment tag.
* Check your rules of engagement ([ACCEPTABLE-USE.md](ACCEPTABLE-USE.md)) and your cloud provider's testing policy.

You need the `aws` CLI configured with administrator credentials (needed only for setup), Go 1.26 or newer to build,
and either an SSH key or AWS Systems Manager Session Manager access to reach the hub.

## 2. Build

    go build -o bin/ ./cmd/...
    GOOS=linux GOARCH=amd64 go build -o bin/niblrd-linux ./cmd/niblrd
    GOOS=windows GOARCH=amd64 go build -o bin/niblr.exe ./cmd/niblr

The last command makes the Windows program for your testers. Linux testers use `bin/niblr`.

## 3. Create the network

    bin/niblr init --env myteam --region us-west-1

This creates a dedicated VPC, subnet, internet gateway, and two security groups, all tagged `niblr-managed=true` and
`niblr-env=myteam`. The command is safe to re-run and prints the IDs used below: `subnet`, `exit-sg`, and `hub-sg`.

The hub security group allows inbound `UDP 51000-51999` (the WireGuard tunnels from exits and testers) and `TCP 8443`
(enrollment). The exit security group allows no inbound traffic.

`niblr init` creates only the network. It does not create the hub's IAM role, a permissions boundary, or a budget
alarm.

## 4. Create the hub's IAM role

The hub needs a role with the policy in [IAM-aws.md](IAM-aws.md), with `REGION`, `ACCOUNT`, `SUBNET_ID`, `SG_ID` (the
exit security group), and `ENV` filled in. The template is `prototype/aws-go/hub-policy.json.tmpl`. Create the role, put
the policy on it, and create an instance profile with the same name. `rig-up.sh` shows the exact `aws iam` commands.
Without this role, the hub cannot create exits.

## 5. Launch the hub

* Use Ubuntu 24.04 and a `t3.nano` or `t3.micro`. Only a `t3.nano` has been tested, in short runs. Its network credits
  run out under sustained load, so use a larger or non-burstable size for real engagements.
* Put it in the hub security group and attach the hub instance profile.
* Set `HttpTokens=required` to enforce Instance Metadata Service version 2 (IMDSv2).
* Give it an Elastic IP, a fixed public address. Every exit has the hub's address built in, so a hub that gets a new
  address strands its existing exits.
* Add your own security group that allows SSH only from your address, never from `0.0.0.0/0`.
* Tag the hub if you want to find it later, but never give it the exit environment's tag. The daemon deletes any
  instance tagged `niblr-managed=true` with its own `niblr-env` that is not a valid exit, so a hub with those tags would
  terminate itself. Use a different environment value, for example `niblr-env=myteam-hub`.

On the hub:

    sudo apt-get update && sudo apt-get install -y wireguard nftables conntrack iptables
    sudo mkdir -p /etc/niblr
    sudo install -m755 niblrd-linux /usr/local/bin/niblrd

Generate the hub key on your workstation with `niblr dev genkey`. It prints a private and a public key. Copy the
private key to `/etc/niblr/hub.key` on the hub (mode 600) by copying a file, for example with `scp`, and do not type it
into a shell where it ends up in the history. You will need the same key if you ever rebuild the hub.

## 6. Run the daemon

Create `/etc/systemd/system/niblrd.service`:

    [Unit]
    Description=NiBLR hub daemon
    After=network-online.target
    StartLimitIntervalSec=0
    [Service]
    Environment=AWS_REGION=us-west-1
    ExecStart=/usr/local/bin/niblrd --want 3 --env myteam --region us-west-1 \
        --ami <ubuntu-24.04-ami> --subnet <subnet> --exit-sg <exit-sg> --hub-ip <hub-elastic-ip> \
        --lease-minutes 240
    Restart=always
    [Install]
    WantedBy=multi-user.target

Then start it and watch the log:

    sudo systemctl daemon-reload && sudo systemctl enable --now niblrd
    sudo journalctl -u niblrd -f

After about a minute, the log shows three healthy exits. The main flags are below, and `niblrd --help` lists all of
them.

| Flag | Meaning |
|---|---|
| `--want N` | How many exits to keep. The daemon creates, replaces, and removes exits to match. |
| `--lease-minutes M` | Each exit shuts itself down and terminates after M minutes, and the daemon refills the slot. This limits the cost of forgotten exits. The default, `0`, disables the lease. |
| `--size` | The exit instance type. The default is `t3.nano`, and the policy allows only small types. |
| `--drain <ids>` | A comma-separated list of exit IDs to take out of rotation without destroying them. |

## 7. Invite testers

On the hub:

    sudo niblrd invite --name alice --ttl 24h --hub-ip <hub-elastic-ip>

Send the printed `niblr1:...` invite to the tester over a private channel. The invite can be used once and expires after
the `--ttl` (24 hours by default). The tester then follows the [tester guide](OPERATOR-GUIDE.md).

## Day to day

To rotate a burned address (one that a client has blocked), delete that exit's instance in the console or CLI. The
daemon builds a replacement with a new address and removes the old tunnel. Connections through the deleted exit end, and
others are unaffected.

To change the number of exits, change `--want` and restart `niblrd`.

If the hub is lost, launch a new one the same way, with the same hub key and the same Elastic IP. It finds its exits
again from their cloud tags. It does not remember testers, so send each one a new invite. If the new hub has a
different address, the old exits cannot reach it. Delete them and let the new hub build fresh ones.

To list the exits for an environment, run `niblr dev exit-list --env myteam --region us-west-1`. The `dev` commands are
developer helpers. This one needs administrator credentials and creates the NiBLR network in that region if it is
missing.

## Teardown

See [COST-AND-CLEANUP.md](COST-AND-CLEANUP.md). Terminate the hub first. Otherwise it recreates exits as you delete
them.

## Known gaps

* No command to revoke a tester
* The hub does not block tester traffic to private and metadata addresses
* Invites can be created only on the hub
* The Windows client has not been tested on a real Windows machine
* IPv6 behavior has not been measured
* Only AWS is implemented

The README section on what has and has not been tested has the full list.
