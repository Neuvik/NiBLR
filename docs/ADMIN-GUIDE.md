# NiBLR admin guide (AWS)

How to stand up a NiBLR environment and keep it healthy.

> **Honest status:** every step here is proven on a real AWS account, but the hub is still set up by hand. There is no
> installer or machine image yet. The automated version of these steps is `prototype/aws-go/rig-up.sh`; read it next to
> this guide. Packaging the hub is the next documentation-relevant piece of work.

## 0. Decide where it lives

* **Use a dedicated AWS account if you can.** If the account is shared with other things, NiBLR is built to cope (its
  role can only touch its own tagged resources) but quotas are shared. Read [IAM-aws.md](IAM-aws.md) first.
* **Pick a region.** All examples use `us-west-1`; pick the one close to your testers or your target.
* **Pick an environment name** (e.g. `myteam`). Everything NiBLR creates is tagged with it, and a hub only sees its own.
* **Check your engagement rules** ([ACCEPTABLE-USE.md](ACCEPTABLE-USE.md)) and your cloud provider's testing policy.

You need: the `aws` CLI configured with **admin** credentials (only for setup), Go 1.26+ to build, and an SSH key
or SSM access to reach the hub.

## 1. Build

    go build -o bin/ ./cmd/...
    GOOS=linux GOARCH=amd64 go build -o bin/niblrd-linux ./cmd/niblrd

## 2. Create the network

    bin/niblr init --env myteam --region us-west-1

This creates a dedicated VPC, subnet, internet gateway and two security groups, all tagged `niblr-managed=true` and
`niblr-env=myteam`. It is safe to re-run. It prints the ids you need below (`subnet`, `exit-sg`, `hub-sg`).
The hub security group opens **UDP 51000-51999** (WireGuard) and **TCP 8443** (enrollment). Exits get no inbound access.

## 3. Create the hub's IAM role

The hub needs a role with the policy in [IAM-aws.md](IAM-aws.md), with `REGION`, `ACCOUNT`, `SUBNET_ID`, `SG_ID`
(the **exit** SG) and `ENV` filled in. The template is `prototype/aws-go/hub-policy.json.tmpl`. Create the role, put the
policy on it, and create an instance profile of the same name. Without this the hub cannot create exits.

## 4. Launch the hub

* Ubuntu 24.04, `t3.micro` or `t3.nano` is enough.
* In the **hub** security group, with the hub instance profile.
* `HttpTokens=required` (IMDSv2) and a public IP.
* Optional: add your own SSH-only security group restricted to your address. Never open SSH to the world.

On the hub:

    sudo apt-get update && sudo apt-get install -y wireguard nftables conntrack
    # hub key: generate on your workstation with `niblr dev genkey` (prints private and public key)
    sudo mkdir -p /etc/niblr
    echo "<PRIVATE KEY>" | sudo tee /etc/niblr/hub.key >/dev/null && sudo chmod 600 /etc/niblr/hub.key
    sudo install -m755 niblrd-linux /usr/local/bin/niblrd

## 5. Run the daemon

`/etc/systemd/system/niblrd.service`:

    [Unit]
    Description=NiBLR hub daemon
    After=network-online.target
    StartLimitIntervalSec=0
    [Service]
    Environment=AWS_REGION=us-west-1
    ExecStart=/usr/local/bin/niblrd --want 3 --env myteam --region us-west-1 \
        --ami <ubuntu-24.04-ami> --subnet <subnet> --exit-sg <exit-sg> --hub-ip <hub public ip> \
        --lease-minutes 240
    Restart=always
    [Install]
    WantedBy=multi-user.target

    sudo systemctl daemon-reload && sudo systemctl enable --now niblrd
    journalctl -u niblrd -f

Within about a minute the log should show three healthy exits. Key flags (`niblrd --help` lists all):

| Flag | Meaning |
|---|---|
| `--want N` | how many exits to keep. The daemon creates, replaces and removes to match |
| `--lease-minutes M` | each exit shuts itself down and terminates after M minutes (a safety net against forgotten exits; the daemon refills the slot). `0` = never |
| `--size` | exit instance type (default `t3.nano`; the policy allows only small types) |
| `--drain ids` | take exits out of rotation without destroying them |

## 6. Invite operators

On the hub:

    sudo niblrd invite --name alice --ttl 24h --hub-ip <hub public ip>

Give the printed `niblr1:...` string to the operator over a private channel. It works once and expires. The operator
then follows the [operator guide](OPERATOR-GUIDE.md).

## Day-to-day

* **Burned address / rotate one:** delete that exit instance in the console or CLI. The daemon notices, builds a
  replacement with a fresh address and removes the old tunnel. Established connections on it end; others are unaffected.
* **More or fewer exits:** change `--want` and restart `niblrd`.
* **Hub died:** launch a new hub the same way with the **same hub key**. It finds its exits again from cloud tags.
  Operators' enrollments are not recovered: re-invite them.
* **See what exists:** `niblr dev exit-list --env myteam` (lists the instances tagged for that environment; `dev` commands are developer helpers).

## Teardown

See [COST-AND-CLEANUP.md](COST-AND-CLEANUP.md). **Terminate the hub first**, otherwise it recreates exits as you delete them.

## Known gaps

No revoke-operator command; invites can only be minted on the hub; Windows client untested on real Windows; IPv6
behaviour unmeasured; only AWS is implemented. See README "What has been proven".
