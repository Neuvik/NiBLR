package boot

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenKey(t *testing.T) {
	a, ap, err := GenKey()
	if err != nil {
		t.Fatal(err)
	}
	b, _, _ := GenKey()
	if a == b {
		t.Fatal("keys must differ")
	}
	for _, k := range []string{a, ap} {
		if raw, err := base64.StdEncoding.DecodeString(k); err != nil || len(raw) != 32 {
			t.Fatalf("not a 32-byte base64 key: %q", k)
		}
	}
}

func TestExitCloudInit(t *testing.T) {
	y, err := ExitCloudInit(ExitParams{Slot: 3, PrivateKey: "PRIV", HubPublic: "HUBPUB", HubEndpoint: "1.2.3.4"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Address = 10.20.3.2/30", "PrivateKey = PRIV", "PublicKey = HUBPUB", "Endpoint = 1.2.3.4:51103",
		"AllowedIPs = 10.20.3.1/32, 10.10.9.0/24", // the operator-subnet return route
		"StartLimitIntervalSec=0", "Restart=on-failure", "masquerade",
	} {
		if !strings.Contains(y, want) {
			t.Errorf("missing %q", want)
		}
	}
	if !strings.HasPrefix(y, "#cloud-config\n") {
		t.Error("must start with #cloud-config")
	}
	if _, err := ExitCloudInit(ExitParams{Slot: 0, PrivateKey: "a", HubPublic: "b", HubEndpoint: "c"}); err == nil {
		t.Error("slot 0 must be rejected")
	}
	if _, err := ExitCloudInit(ExitParams{Slot: 1}); err == nil {
		t.Error("missing keys must be rejected")
	}
}

func TestHubUpDownAreInverseAndOrdered(t *testing.T) {
	up, err := HubExitUp(2, "EXITPUB", "/etc/niblr/hub.key")
	if err != nil {
		t.Fatal(err)
	}
	last := up[len(up)-1]
	if last[1] != "rule" || last[2] != "add" {
		t.Errorf("the fwmark rule must come last, after link+route: %v", last)
	}
	down, _ := HubExitDown(2)
	if down[0][1] != "rule" || down[0][2] != "del" {
		t.Errorf("rule must be removed first: %v", down[0])
	}
	if down[1][0] != "conntrack" || down[1][3] != "2" {
		t.Errorf("must purge conntrack marks before reuse: %v", down[1])
	}
	if _, err := HubExitUp(251, "x", "k"); err == nil {
		t.Error("slot 251 must be rejected")
	}
}
