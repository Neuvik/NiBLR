# How NiBLR works

A short, current explanation. [DESIGN.md](DESIGN.md) holds the reasoning, review notes and roadmap.

## The picture

    operator ──WireGuard──▶ hub ──one WireGuard tunnel per exit──▶ exit VM ──▶ internet
                              ▲                                      │
                              └──────── exits dial the hub ──────────┘

* **Operators** are WireGuard peers on `10.10.9.0/24`. They send all traffic to the hub.
* **Exits** are tiny cloud VMs. Each one dials *back* to the hub, so exits need no inbound ports. Each exit NATs
  what it receives out of its own public address.
* **The hub** owns one WireGuard interface per exit (`wg-eN`), plus one for operators (`wg-op`).

## Choosing an exit (the rotation)

On the hub, nftables gives every **new** operator connection a mark (the exit number), chosen at random from the
healthy exits. The mark is saved with the connection (conntrack) and re-applied to its later packets, so a connection
never changes exit mid-stream. `ip rule fwmark N` sends marked traffic to routing table `1000+N`, whose default route
is that exit's tunnel. Replies come back the same way, so it is symmetric by construction.

**Failing closed:** a fallback routing rule for operator traffic ends in an unreachable route, and when no exit is
healthy new connections are dropped. Nothing falls back to the hub's own address.

## Health

An exit is in the pool only if it has a recent WireGuard handshake (under about 200 s) **and** a marked ping to
`1.1.1.1` through it succeeds end to end. A drained exit leaves the pool while its tunnel stays up.
Pool changes are applied atomically in one `nft -f`.

## The reconcile loop (no state file)

`niblrd` repeats every few seconds:

1. **List** exits in the cloud by tag (`niblr-managed`, `niblr-env`, `niblr-slot`, plus the exit's public key).
2. **Plan** against `--want`: create missing slots, delete strays, duplicates, stopped (broken) instances.
3. **Apply** through the provider (AWS today). Creates carry an idempotency token and tags set at creation time.
4. **Sync the hub** with what exists: build interfaces for new exits, remove interfaces for gone ones.

**Ordering rule:** a slot is taken out of the routing pool *before* its interface is torn down, then its conntrack
entries are purged.

**Rotation = delete the instance.** The loop refills the slot with a new instance (new address, new key).
Losing the hub loses nothing about exits: a new hub rebuilds from the tags.

## Joining

`niblrd invite` mints a one-time token. `niblr join` generates the operator's key **locally**, then sends only the
public key and token to the hub over TLS pinned to the certificate fingerprint carried in the invite. The hub adds
the peer and returns the tunnel settings. Client config sends `0.0.0.0/0` and `::/0` into the tunnel, forces DNS
through it, and on Linux blocks non-tunnel output with a firewall rule.

## Code map

    cmd/niblr          operator + admin CLI
    cmd/niblrd         hub daemon (static mode, managed mode, invite)
    internal/provider  cloud contract (Provider: CreateExit, DeleteExit, ListExits), aws/ and fake/
    internal/reconcile plan + apply, level-triggered
    internal/hub       Manager (sync hub to exits), Base (one-time hub setup)
    internal/boot      exit cloud-init and the exact hub commands
    internal/pool, dataplane, health   routing marks, nftables ruleset, health checks
    internal/enroll    invites, enrollment server and client
    internal/client    renders and applies the operator's tunnel config (Linux, Windows)

## What is *not* here yet

Other clouds, SOCKS5/HTTPS workers, IPv6 exits and operator revocation. See the README roadmap.
