package client

import (
	"strings"
	"testing"

	"niblr/internal/enroll"
)

func params() Params {
	return Params{PrivateKey: "PRIV", Result: enroll.Result{Address: "10.10.9.2/32", HubKey: "HUB", WG: "1.2.3.4:51000", DNS: []string{"1.1.1.1", "9.9.9.9"}, MTU: 1420}}
}

func TestBothPlatformsSendEverythingIncludingIPv6ThroughTheTunnel(t *testing.T) {
	for name, f := range map[string]func(Params) (string, error){"windows": Windows, "linux": Linux} {
		c, err := f(params())
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"AllowedIPs = 0.0.0.0/0, ::/0", "DNS = 1.1.1.1, 9.9.9.9", "MTU = 1420", "Address = 10.10.9.2/32", "Endpoint = 1.2.3.4:51000", "PersistentKeepalive"} {
			if !strings.Contains(c, want) {
				t.Errorf("%s: missing %q", name, want)
			}
		}
	}
}

func TestLinuxHasKillSwitchForBothIPFamiliesAndRemovesItOnDown(t *testing.T) {
	c, _ := Linux(params())
	for _, want := range []string{"PostUp = iptables -I OUTPUT", "ip6tables -I OUTPUT", "-j REJECT", "PreDown = iptables -D OUTPUT", "ip6tables -D OUTPUT"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestWindowsConfigRelyingOnBuiltInKillSwitchHasNoScripts(t *testing.T) {
	c, _ := Windows(params())
	if strings.Contains(c, "PostUp") || strings.Contains(c, "iptables") {
		t.Fatal("Windows config must not carry Linux scripts (and WireGuard for Windows ignores them anyway)")
	}
}

func TestRejectsIncompleteOrSillyInput(t *testing.T) {
	p := params()
	p.Result.MTU = 100
	if _, err := Linux(p); err == nil {
		t.Error("tiny MTU accepted")
	}
	p = params()
	p.PrivateKey = ""
	if _, err := Windows(p); err == nil {
		t.Error("missing key accepted")
	}
}
