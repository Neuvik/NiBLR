# Troubleshooting and FAQ

This page lists the messages `niblr` prints and what to do about each one. Find your message in the first column and
follow the third.

One thing to know first. An invite works once. `niblr` now checks for administrator rights and for WireGuard before it
uses your invite, so those two problems do not use it up. Any problem that appears after "Connecting to NiBLR..." can
mean the invite is used, and you may need a new one.

## Joining

| You see | What it means | What to do |
|---|---|---|
| Usage: niblr join `<invite>` | You ran `join` without the invite | Paste the whole invite after `join` |
| that doesn't look like a NiBLR invite | The text you pasted is not an invite | Copy the whole line you were sent. It starts with `niblr1:` |
| the invite looks damaged | The invite was cut off or changed when copied | Ask for a new one |
| NiBLR needs to run as Administrator (Windows) | The window does not have administrator rights | Close it, open PowerShell with Run as administrator, and try the same invite again |
| NiBLR needs administrator rights ... "sudo" (Linux) | The command did not run as administrator | Run it again with `sudo` in front |
| WireGuard isn't installed | The VPN software NiBLR uses is missing | Install it as the message says, then try the same invite again |
| you're already joined | A previous join is still active | Run `niblr leave`, then join with the new invite |
| this invite has expired | Invites time out. This check uses your computer's clock | Check your clock, then ask for a new invite |
| this invite isn't valid any more: it was already used or has expired | Invites work once | Ask for a new one |
| too many attempts. Wait a minute | The server limits repeated attempts | Wait a minute and try again |
| this doesn't look like the right NiBLR server | The server's identity check failed. The invite may be old or wrong, or someone may be pretending to be the server | Do not retry. Tell the person who gave you the invite |
| the server's key doesn't match your invite | The same kind of problem | Do not retry. Tell your admin |
| couldn't reach the NiBLR server | Your computer could not contact the hub | See the next section |
| the NiBLR server had a problem | The hub returned an error, for example because it has no free addresses left | Try again in a moment, then tell your admin |
| the NiBLR server sent an unexpected answer | The hub replied with something `niblr` did not expect | Tell your admin |
| couldn't save the connection settings | `niblr` could not write its settings file | Check that the disk is not full, then tell your admin |
| couldn't start the connection | WireGuard refused to start. The message continues with its reason | Your invite is now used. Send the full message to your admin and ask for a new invite |
| Couldn't create your security key | Your computer could not generate a key | Tell your admin |

### Couldn't reach the NiBLR server

Your computer could not contact the hub. Work through these in order.

1. Check that your internet works.
2. Check that your network allows outgoing connections to the hub on TCP port 8443. A port is a numbered door on a
   server, and a corporate firewall often blocks this one. On Linux, `nc -vz HUB-ADDRESS 8443` tests it. On Windows,
   `Test-NetConnection HUB-ADDRESS -Port 8443` does the same.
3. If it still fails, tell your admin.

## After joining

### "Joined, but the internet isn't reachable"

NiBLR is blocking your traffic so your real address cannot leak. Two common causes are that every exit is being rebuilt
(this takes about a minute) and that the hub is down. Wait a minute, and tell your admin if it persists.

### "Not joined" but I just joined

On Linux, run `sudo niblr status`. Without `sudo`, `niblr` cannot see the connection and says "Not joined" even while
the firewall rule is active.

### Everything is slow

Your traffic passes through two servers and is encrypted twice, and the exits are small cloud machines. Ask your admin
for more exits or larger ones. Large downloads work but are slow.

### Some tools pause for several seconds before a site loads

NiBLR blocks IPv6, the newer address type, because its exits only use IPv4. Some tools try IPv6 first and wait for it to
time out before switching to IPv4. This is a known rough edge. Add `-4` to your command if the tool supports it, for
example `curl -4` or `ping -4`. We have not yet measured how long the delay is on a machine that has both IPv4 and
IPv6.

### A tool still shows my real address

NiBLR sends all of your computer's traffic through the tunnel, so you do not set a proxy in Burp, your browser, or
other tools. If any tool shows your real address, stop testing, run `sudo niblr status`, and tell your admin.

### An address got blocked

Ask your admin to delete that exit. The hub builds a replacement with a new address, usually within about a minute.

### I lost my SSH session, or my local devices stopped working

On Linux, the firewall rule that protects you also stops your computer from answering anything outside the tunnel, so an
SSH session drops and local devices become unreachable. Run `sudo niblr leave` from the local console, and do not run
`join` over SSH.

## For admins

Testers can skip this section.

### No exits come up

On the hub, run `sudo journalctl -u niblrd`. Typical causes are an instance type the policy does not allow, a reached
instance quota in the account, or an AMI (the machine image AWS uses to start the exit) that is wrong for the region.
AWS may refuse the first launch in a new region with a "pending verification" message. If that happens, retry the
launch.

### Testers cannot join

The hub must accept incoming connections on TCP port 8443 and UDP ports 51000 to 51999. `niblr init` opens them. Create
invites on the hub with `niblrd invite`, and make sure `--hub-ip` is the hub's public IP address.

### I rebuilt the hub and exits stopped working

Each exit is built with the hub's public IP address inside it. A new hub with a different IP leaves the old exits
connecting to the old address, and they never become healthy. Delete them so the new hub builds fresh ones, and give
the hub an Elastic IP next time so its address survives a rebuild. Testers are not recovered either, so each tester
needs a new invite. There is no command to revoke a tester yet.

### Things to avoid

Do not post an invite in a shared or public channel, because the first person to use it gets in. Do not copy one
tester's key to a second machine.

## FAQ

### Can I use it with Burp, nmap, curl, or a browser?

Yes. NiBLR sends all traffic from your computer through the tunnel, so you do not change any tool settings. Expect nmap
scans to be slower because the exits are small.

### Does the address change on every request?

No. Each new connection uses one exit and stays on it until the connection ends. This keeps logins and sessions
working.

### How many addresses do I get?

One per exit. Your admin decides how many exits run, often three to five.

### Can I use a SOCKS5 or HTTP proxy instead?

Not yet. It is on the roadmap in the README.

### Will reverse shells work?

No. Exits accept no incoming connections, so nothing on the internet can reach your computer through NiBLR.

### Does it work from inside a corporate network?

Only if your network allows outgoing UDP traffic to the hub's WireGuard ports (51000 to 51999) and TCP port 8443 for
joining. If you are not sure, ask the network team or try it.
