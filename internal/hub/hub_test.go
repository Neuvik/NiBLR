package hub

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"niblr/internal/boot"
	"niblr/internal/provider"
	"niblr/internal/provider/fake"
	"niblr/internal/reconcile"
)

// kernel is a fake hub: it interprets the same commands the real hub would run.
type kernel struct {
	mu    sync.Mutex
	ifs   map[string]string // iface -> peer pub
	log   []string
	fail  string // substring of a command that should fail
	order []string
}

func newKernel() *kernel { return &kernel{ifs: map[string]string{}} }

func (k *kernel) Run(_ context.Context, argv ...string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	cmd := strings.Join(argv, " ")
	k.log = append(k.log, cmd)
	if k.fail != "" && strings.Contains(cmd, k.fail) {
		return "", fmt.Errorf("boom")
	}
	switch {
	case cmd == "wg show interfaces":
		var n []string
		for i := range k.ifs {
			n = append(n, i)
		}
		sort.Strings(n)
		return strings.Join(n, " "), nil
	case strings.HasPrefix(cmd, "wg show ") && strings.HasSuffix(cmd, " peers"):
		return k.ifs[argv[2]] + "\n", nil
	case argv[0] == "ip" && argv[1] == "link" && argv[2] == "add":
		k.ifs[argv[3]] = ""
	case argv[0] == "wg" && argv[1] == "set":
		for i, a := range argv {
			if a == "peer" {
				k.ifs[argv[2]] = argv[i+1]
			}
		}
	case argv[0] == "ip" && argv[1] == "link" && argv[2] == "del":
		if _, ok := k.ifs[argv[3]]; !ok {
			return "", fmt.Errorf("Cannot find device")
		}
		delete(k.ifs, argv[3])
	}
	return "", nil
}

func (k *kernel) ran(sub string) int {
	n := 0
	for _, c := range k.log {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

func newMgr(t *testing.T, want int) (*Manager, *fake.Fake, *kernel) {
	f, k := fake.New(), newKernel()
	n := 0
	tm := reconcile.Template{Env: "e", Render: func(slot int) (string, map[string]string, error) {
		n++
		return "ci", map[string]string{"pub": fmt.Sprintf("PUB-%d-%d", slot, n)}, nil
	}}
	return &Manager{P: f, Env: "e", Want: want, Tmpl: tmpl(tm), R: k, KeyFile: "/k"}, f, k
}
func tmpl(t reconcile.Template) reconcile.Template { return t }

func round(t *testing.T, m *Manager, r string) Plan {
	t.Helper()
	p, err := m.Reconcile(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Down(context.Background(), p.Down); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(context.Background(), p.Up); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestColdStartBuildsEverySlotAndThenIsQuiet(t *testing.T) {
	m, _, k := newMgr(t, 3)
	p := round(t, m, "r1")
	if len(p.Up) != 3 || len(p.Down) != 0 {
		t.Fatalf("plan %+v", p)
	}
	if !reflect.DeepEqual(m.Live(), []int{1, 2, 3}) {
		t.Fatalf("live %v", m.Live())
	}
	before := len(k.log)
	p = round(t, m, "r2")
	if len(p.Up)+len(p.Down) != 0 {
		t.Fatalf("second round must change nothing: %+v", p)
	}
	if k.ran("link add") != 3 {
		t.Fatalf("interfaces recreated needlessly")
	}
	_ = before
}

func TestRotationRebuildsTheSlotAroundTheNewKey(t *testing.T) {
	m, f, k := newMgr(t, 2)
	round(t, m, "r1")
	exits, _ := f.ListExits(context.Background(), "e")
	var old provider.Exit
	for _, e := range exits {
		if e.Slot == 2 {
			old = e
		}
	}
	f.DeleteExit(context.Background(), old.ID) // "rotate": just kill the instance
	p := round(t, m, "r2")
	if !reflect.DeepEqual(p.Down, []int{2}) || len(p.Up) != 1 || p.Up[0].Slot != 2 {
		t.Fatalf("plan %+v", p)
	}
	if k.ifs["wg-e2"] == old.Meta["pub"] || k.ifs["wg-e2"] == "" {
		t.Fatalf("wg-e2 still has the old peer: %q", k.ifs["wg-e2"])
	}
	if k.ran("conntrack -D --mark 2") != 1 {
		t.Fatal("old flows on the slot's mark must be purged before it is reused")
	}
}

func TestHubRestartRebuildsFromTagsAlone(t *testing.T) {
	m, f, k := newMgr(t, 2)
	round(t, m, "r1")
	k.ifs = map[string]string{}                                                // hub rebooted: kernel state lost
	m2 := &Manager{P: f, Env: "e", Want: 2, Tmpl: m.Tmpl, R: k, KeyFile: "/k"} // fresh process, no memory
	p := round(t, m2, "r2")
	if len(p.Up) != 2 {
		t.Fatalf("must rebuild both from tags: %+v", p)
	}
	exits, _ := f.ListExits(context.Background(), "e")
	for _, e := range exits {
		if k.ifs[boot.Iface(e.Slot)] != e.Meta["pub"] {
			t.Errorf("slot %d wired to wrong key", e.Slot)
		}
	}
	live := 0
	for _, e := range exits {
		if e.State == provider.StateRunning {
			live++
		}
	}
	if live != 2 {
		t.Fatalf("rebuilding the hub must not create new exits (got %d)", live)
	}
}

func TestScaleDownTearsDownInterfaces(t *testing.T) {
	m, _, k := newMgr(t, 3)
	round(t, m, "r1")
	m.Want = 1
	p := round(t, m, "r2")
	if !reflect.DeepEqual(p.Down, []int{2, 3}) {
		t.Fatalf("plan %+v", p)
	}
	if _, ok := k.ifs["wg-e3"]; ok || len(k.ifs) != 1 {
		t.Fatalf("ifs %v", k.ifs)
	}
}

func TestIgnoresInterfacesThatAreNotExits(t *testing.T) {
	m, _, k := newMgr(t, 1)
	k.ifs["wg-op"] = "OPERATORPEER"
	round(t, m, "r1")
	if _, ok := k.ifs["wg-op"]; !ok {
		t.Fatal("the operator interface must never be touched")
	}
}

func TestHalfBuiltSlotIsTornDownNotLeftLive(t *testing.T) {
	m, _, k := newMgr(t, 1)
	k.fail = "ip route replace"
	p, _ := m.Reconcile(context.Background(), "r1")
	if err := m.Up(context.Background(), p.Up); err == nil {
		t.Fatal("expected error")
	}
	if len(m.Live()) != 0 {
		t.Fatalf("half-built slot must not be live: %v", m.Live())
	}
	if _, ok := k.ifs["wg-e1"]; ok {
		t.Fatal("half-built interface must be removed")
	}
}

func TestPendingExitWithoutKeyIsNotWired(t *testing.T) {
	got := WantedPeers([]provider.Exit{
		{Slot: 1, State: provider.StateRunning}, // no pub tag
		{Slot: 2, State: provider.StateGone, Meta: map[string]string{"pub": "x"}},
		{Slot: 0, State: provider.StateRunning, Meta: map[string]string{"pub": "x"}},
		{Slot: 3, State: provider.StatePending, Meta: map[string]string{"pub": "p3"}},
	})
	if !reflect.DeepEqual(got, map[int]string{3: "p3"}) {
		t.Fatalf("%v", got)
	}
}
