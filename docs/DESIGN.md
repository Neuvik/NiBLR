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

## Not decided yet / open

* Invite token format and hub enrollment API transport.
* Hub's cloud role scope (create/terminate tagged instances + EIPs only).
* Second provider (DigitalOcean planned).
