# Troubleshooting and FAQ

Messages below are what `niblr` actually prints. Find yours, do what it says.

## Joining

| You see | What it means | What to do |
|---|---|---|
| "needs to run as Administrator" (Windows) | The window lacks rights | Close it, right-click PowerShell, "Run as administrator", retry |
| "needs administrator rights ... use sudo" (Linux) | Same | Put `sudo` in front of the command |
| "WireGuard isn't installed" | The tunnel software is missing | Install it as the message says, then retry |
| "you're already joined" | A previous join is still active | `niblr leave`, then join with the new invite |
| "this invite has expired" | Invites time out | Ask your admin for a new one |
| "this invite isn't valid any more: it was already used or has expired" | Invites work once | Ask for a new one |
| "too many attempts. Wait a minute" | Rate limit on the server | Wait a minute, retry |
| "this doesn't look like the right NiBLR server (its identity didn't match the invite)" | The server's certificate isn't the one in your invite. Could be a wrong/old invite or someone intercepting | **Stop.** Don't retry. Tell whoever gave you the invite |
| "the server's key doesn't match your invite" | Same kind of problem | **Stop** and tell your admin |
| "couldn't reach the NiBLR server" | No route to the admin's hub | Check your internet; check you are allowed to reach the hub's address on port 8443; tell your admin |
| "couldn't start the connection" | WireGuard refused to start | Read the rest of the message; if unclear, send it to your admin |

## After joining

**`status` says "Joined, but the internet isn't reachable".** Nothing is leaking; traffic is blocked until it works.
Usual causes: all exits are replacing themselves (about a minute), or the hub is down. Wait a minute, then tell your admin.

**Everything is slow.** Two encrypted hops cost speed, and exits are small machines. Ask your admin for more or
larger exits. Very large downloads work but will not be fast.

**A site loads over IPv4 but some tools hang for a while first.** IPv6 is deliberately dropped inside the tunnel
(NiBLR has no IPv6 exits), and some tools wait for a timeout before falling back to IPv4. This is a known rough edge;
use `-4` where your tool supports it. (Not yet measured on a real dual-stack machine.)

**My tool ignores NiBLR.** Full-tunnel mode has no proxy settings; everything goes through. If a tool still shows
your real address, run `niblr status` and tell your admin immediately.

**An address got blocked.** Ask your admin to delete that exit; the hub builds a replacement with a new address.

## Admins

**No exits come up.** Check the hub's log (`journalctl -u niblrd`). Typical causes: the instance type is not allowed
by the policy, the account's instance quota is reached, or the AMI is wrong for the region. The first launch in a new
region can fail once with a "pending verification" message from AWS; retrying works.

**Operators can't enroll.** The hub must be reachable on **TCP 8443** and **UDP 51000-51999** (`niblr init` opens
these). Invites are minted on the hub with `niblrd invite`; check the hub address you used is its public IP.

**"state" disappeared after rebuilding the hub.** Exits are rediscovered from cloud tags, but operators are not.
Re-invite them. There is no revoke command yet.

**Things to never do:** don't share an invite in a public channel (anyone who uses it first gets in), and don't
reuse one operator's key on two machines.

## FAQ

**Can I use it with Burp, nmap, curl, a browser?** Yes. It is a full tunnel, so everything works with no settings.
Expect nmap to be slower; scan rates are limited by the small exits.

**Does each packet change address?** No. Each new *connection* picks an exit and keeps it. This is deliberate: it
keeps logins and sessions working.

**How many addresses do I get?** One per exit. Ask your admin how many are running (a handful is typical).

**Can I use SOCKS5/HTTP instead of a full tunnel?** Not yet. It is on the roadmap (see README).

**Does it work from inside a corporate network?** Only if UDP to the hub's WireGuard port is allowed.
