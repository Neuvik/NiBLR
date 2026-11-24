#!/usr/bin/env python3
"""Runs inside the client namespace."""
import socket, sys, time, collections
import os
HOST = os.environ.get("TARGET", "203.0.113.1")
T = (HOST, 8080)
def one():
    s = socket.create_connection(T, timeout=3); f = s.makefile("rw")
    f.write("x\n"); f.flush(); r = f.readline().strip(); s.close(); return r
def many(n):
    c = collections.Counter()
    for _ in range(n):
        try: c[one()] += 1
        except Exception as e:
            c["FAIL"] += 1; print("flow", sum(c.values()), "failed:", repr(e), file=sys.stderr)
    return c
def udp():
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.settimeout(3)
    s.sendto(b"x", (HOST, 9999)); return s.recvfrom(100)[0].decode()
cmd = sys.argv[1]
if cmd == "many": print(dict(many(int(sys.argv[2]))))
elif cmd == "udp": print(udp())
elif cmd == "long":   # one connection, many messages over `secs`; print distinct sources
    s = socket.create_connection(T, timeout=3); f = s.makefile("rw"); seen = set(); end = time.time() + float(sys.argv[2])
    try:
        while time.time() < end:
            f.write("x\n"); f.flush(); seen.add(f.readline().strip()); time.sleep(0.2)
        print(sorted(seen))
    except Exception as e: print("BROKEN", e)
