# NiBLR design

Goal: an operator runs `niblr join`, gets a full L3 connection, and their traffic leaves from
rotating public addresses. Operators are not network people: nothing to configure, plain-language
errors, no proxy settings.

## Shape

Go, one module, direct cloud SDKs. No Terraform, no Pulumi, no Ansible.

* `cmd/niblr`  - the operator/admin CLI (`join`, `status`, `rotate`, `scale`, `down`).
* `cmd/niblrd` - the hub daemon (health checks, exit pool, rotation, operator enrollment API).
* `internal/provider` - cloud contract. One small implementation per cloud.
* `internal/pool`, `internal/dataplane`, `internal/health` - the hub's brains (pure logic is unit-tested without root).

Why not Terraform/Pulumi: operators would need them installed, rotation is imperative churn
(create / replace / re-IP in a loop), and `join` needs idempotent discovery. State lives in cloud tags
plus the hub; there is no state file to lose.

## Data plane (proven locally and on AWS, see prototype/)

* Operator = WireGuard peer on the hub with its own /32 and key.
* Each exit has its own WireGuard interface on the hub. Exits DIAL the hub (so their public IP can change).
* nftables marks each NEW operator flow with an exit id stored in conntrack; `ip rule fwmark` selects the
  exit's route table. Every packet of a flow, both directions, uses the same exit (symmetric by construction).
* A health loop rewrites the marking rules atomically. No healthy exit => `drop` (fail closed).
* Health = recent handshake AND an end-to-end probe (marked ping sourced from the operator subnet, out
  via the exit and NAT to the internet and back). A tunnel-only probe is NOT enough: it kept routing into an
  exit that had lost its return route.
* Draining an exit (leave pool, keep tunnel) never breaks established flows.

## Exit types (same pool, same health, same marking)

| type | what | L3 | notes |
|---|---|---|---|
| vm | small VM per exit, dials the hub | yes | any provider |
| hub-ip | extra private IPs on the hub mapped 1:1 to public IPs, chosen by `nft snat` | yes | one box, N addresses; untested |
| proxy | container / function exits | TCP via SOCKS or HTTP only | later; optional |

## Operator flow

1. Admin: `niblr init --env X` creates the hub with a scoped cloud role (cloud creds needed only here).
2. Operator: `niblr join <invite>` registers their WireGuard key with the hub, gets a config, brings the tunnel up.
3. `niblr status` says in plain language which address they are leaving from and when it next changes.

If `join` is run with cloud credentials and no environment exists, it creates one first (init + join).

## Provider contract (sketch)

    type Provider interface {
        Name() string
        CreateExit(ctx, spec ExitSpec) (Exit, error)   // boots with the exit cloud-init, dials the hub
        ReplaceExit(ctx, id string) (Exit, error)      // new public IP, same slot/key
        DeleteExit(ctx, id string) error
        ListExits(ctx, env string) ([]Exit, error)     // discovery by tag, no state file
    }

## Known operational notes

* wg-quick@ units hit systemd start limits when restarted >5x in 10s: exit units set Restart=on-failure and a relaxed limit.
* Per-flow rotation can break targets that pin sessions by source IP: rotation policy is configurable.

## Review findings adopted (Opus design review, checked against what the prototype showed)

**Lifecycle (must exist before rotation/scale):**
* Atomic tag-on-create (`TagSpecifications`) and idempotent creates (`ClientToken`), so a half-finished create is never an untagged, billed orphan.
* A GC loop on the hub, plus an out-of-band `niblr down` that sweeps by environment tag without needing the hub.
* Leases so a dead hub cannot leave exits running (instance-initiated shutdown / on-box dead-man; GCP `maxRunDuration`).
* Never reuse an exit mark id without `conntrack -D --mark N` first. Ordering invariant: route + interface up BEFORE an id enters the map; remove the id BEFORE teardown.

**Control-plane security (the hub is internet-facing):**
* Operators enroll OVER WireGuard: an invite is a one-time WG key whose peer can reach only the enrollment API on the hub's tunnel IP. The hub exposes only silent UDP. This settles the enrollment transport.
* The hub's cloud role is scoped to the minimum: no `iam:PassRole` (exits get no instance profile), `ec2:CreateTags` only with `ec2:CreateAction=RunInstances`, IMDSv2 required. A dedicated account/SCP and a budget alarm are the only hard ceiling.
* `vm` exits use ephemeral public IPs (they dial out). EIPs are only for the hub endpoint (and the hub-ip type).
* The hub drops link-local/metadata (169.254.169.254, 168.63.129.16), RFC1918 and CGNAT destinations in the mark chain so operators cannot pivot into the infrastructure.

**Operator client (first-class, not an afterthought):**
* AllowedIPs `0.0.0.0/0, ::/0` (IPv6 would otherwise leak the real address), DNS through the tunnel, hub drops v6 it cannot carry.
* Use the official WireGuard client's kill-switch instead of a hand-rolled dead-man (ours failed OPEN). Target OSes: decide early; emitting a config for the WireGuard app is the cheap v1.

**Data plane traps to handle:**
* conntrack exhaustion under scanning: raise `nf_conntrack_max`/hashsize, tune timeouts, alert on table fill.
* MTU/MSS: tunnels are chained at the hub (inner ~1420); set per-provider MTUs (GCP 1460 default) and clamp MSS on the hub. Add a large-payload test.
* Pin `rp_filter` per interface after creation.
* Hub is cattle, not HA: config derivable from tags, hub key in a secret store, stable endpoint (EIP/DNS), auto-restart. One hub per engagement limits blast radius. Do not size it as a `t3.nano` beyond tests (burst network credits).

**hub-ip exit type:** gated per provider. AWS and Azure support many IPs per NIC (size-limited, needs `ip addr add` by niblrd); GCP allows one external IP per NIC, so it does not fit. `vm` is the only portable type.

## Build order (revised)

1. Thin vertical slice first: minimal AWS provider (create/list/delete with atomic tags + GC), minimal CLI (`join`, `status`), real niblrd. Prove it reproduces the AWS prototype, including large payload/MTU and conntrack under load, plus a rooted netns integration test.
2. Then rotate/scale/replace (churn, quotas, slot reuse), then the operator client leak-proofing.
3. Stub the second provider's signatures during step 1 to catch an AWS-shaped interface early.
4. Delete the legacy Python/Terraform/Ansible once the AWS slice is proven.

## Operator client (Linux and Windows operators)

`niblr join` writes a WireGuard config and brings the tunnel up using the official client on each OS.
Config always routes `0.0.0.0/0, ::/0` and sets `DNS=` to the hub, so nothing leaks around the tunnel.

* Windows: WireGuard for Windows; `niblr` installs the tunnel service (`wireguard.exe /installtunnelservice`,
  needs an admin prompt). With `0.0.0.0/0, ::/0` the app's "block untunneled traffic" kill-switch is active.
* Linux: `wg-quick` with PostUp/PreDown rules that REJECT anything not leaving through the tunnel (wg-quick has no
  kill-switch by default), plus IPv6 and DNS. Needs `wireguard-tools`.
* Neither is tested yet. Windows in particular needs a real run before the kill-switch is trusted.

## Open

* Invite token format (one-time WG key + enrollment API), and hub credential story for non-home clouds (static tokens on the hub: scope tightly).
* Second provider (DigitalOcean planned).
