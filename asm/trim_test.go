package asm

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
)

// busy has something to drop in every droppable layer.
// The request "How do I handle 429?" has the useful words {handle, 429}.
func busy() (Input, Config) {
	in, cfg := sample()
	in.Knowledge = []Item{
		{ID: "k1", Text: "Retry 429 errors with backoff.", Source: "docs/a.md"},   // 1 shared word
		{ID: "k2", Text: "The office is closed on Friday."},                       // 0 shared words
		{ID: "k3", Text: "retry 429 ERRORS   with backoff.", Source: "docs/b.md"}, // same as k1
		{ID: "k4", Text: "Handle 429 by waiting.", Critical: true},
	}
	in.History = []Turn{
		{ID: "h1", Role: "user", Text: "Hi"},
		{ID: "h2", Role: "assistant", Text: "We chose Go.", Critical: true},
		{ID: "h3", Role: "user", Text: "What is new?"},
		{ID: "h4", Role: "assistant", Text: "Nothing much."},
		{ID: "h5", Role: "user", Text: "Ok."},
	}
	in.Tools = []Tool{
		{Name: "search", Description: "Search docs for 429 help."}, // 1 shared word
		{Name: "weather", Description: "Get the weather."},         // 0 shared words
	}
	return in, cfg
}

func ids(m Manifest, action string) []string {
	var out []string
	for _, it := range m.Items {
		if it.Action == action {
			out = append(out, it.ID)
		}
	}
	return out
}

func TestDropOrder(t *testing.T) {
	in, cfg := busy()
	us := units(in)
	var got []string
	for _, c := range dropOrder(us, in.User.Text, cfg.keepTurns()) {
		got = append(got, us[c.i].id)
	}
	want := []string{"k3", "k2", "k1", "h1", "h3", "weather", "search"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFitsNoDrops(t *testing.T) {
	in, cfg := busy()
	cfg.ContextWindow = 100000
	_, m := run(t, in, cfg, nil)
	if m.Status != "ok" || len(ids(m, "dropped")) != 0 {
		t.Errorf("status %s, dropped %v", m.Status, ids(m, "dropped"))
	}
}

func TestDuplicateGoesFirst(t *testing.T) {
	in, cfg := busy()
	cfg.ContextWindow = 100000
	_, full := run(t, in, cfg, nil)
	cfg.MaxInput = full.TotalTokens - 1
	p, m := run(t, in, cfg, nil)
	if m.Status != "trimmed" || m.TotalTokens > cfg.MaxInput {
		t.Errorf("status %s, total %d", m.Status, m.TotalTokens)
	}
	if got := ids(m, "dropped"); !slices.Equal(got, []string{"k3"}) {
		t.Errorf("dropped %v", got)
	}
	if !strings.Contains(p.Text(), "[k1] docs/a.md, docs/b.md") {
		t.Error("source of the duplicate was lost")
	}
}

func TestLayerCap(t *testing.T) {
	in, cfg := busy()
	cfg.ContextWindow = 100000
	only := in
	only.Knowledge = in.Knowledge[3:] // just the critical k4
	_, small := run(t, only, cfg, nil)
	cfg.LayerBudgets = map[string]int{"retrieved_knowledge": small.Layers[2].Tokens}
	_, m := run(t, in, cfg, nil)
	if got := ids(m, "dropped"); !slices.Equal(got, []string{"k1", "k2", "k3"}) {
		t.Errorf("dropped %v, want only knowledge", got)
	}
}

func TestOverflow(t *testing.T) {
	in, cfg := busy()
	cfg.MaxInput = 1
	p, m, err := Assemble(in, cfg, nil)
	if !errors.Is(err, ErrOverflow) || m.Status != "overflow" || len(p.Layers) != 0 {
		t.Fatalf("err %v, status %s", err, m.Status)
	}
	want := []string{"s1", "m1", "k4", "h2", "h4", "h5", "user"}
	if got := ids(m, "included"); !slices.Equal(got, want) {
		t.Errorf("kept %v, want %v", got, want)
	}
}

func TestCapOnSystemOverflows(t *testing.T) {
	in, cfg := busy()
	cfg.LayerBudgets = map[string]int{"system_instructions": 1}
	if _, _, err := Assemble(in, cfg, nil); !errors.Is(err, ErrOverflow) {
		t.Errorf("err = %v, want overflow", err)
	}
}

func TestWordSet(t *testing.T) {
	got := wordSet("How do I handle the 429 Errors?")
	want := map[string]bool{"handle": true, "429": true, "errors": true}
	if !maps.Equal(got, want) {
		t.Errorf("got %v", got)
	}
}

// TestBudgetSweep tries every budget from 1 token to the full size and checks
// the rules that must always hold.
func TestBudgetSweep(t *testing.T) {
	in, cfg := busy()
	cfg.ContextWindow = 100000
	_, full := run(t, in, cfg, nil)
	tok := Estimator{}
	us := units(in)
	order := dropOrder(us, in.User.Text, cfg.keepTurns())
	protected := []string{"s1", "m1", "k4", "h2", "h4", "h5", "user"}

	// totalAfter drops the first k items in order and returns the new total.
	totalAfter := func(k int) int {
		us := units(in)
		order := dropOrder(us, in.User.Text, cfg.keepTurns())
		for _, c := range order[:k] {
			drop(us, []cand{c}, -1, 1, tok)
		}
		_, _, total := render(us, tok)
		return total
	}

	for b := 1; b <= full.TotalTokens+5; b++ {
		cfg.MaxInput = b
		_, m, err := Assemble(in, cfg, nil)
		dropped := ids(m, "dropped")
		n := len(dropped)

		if (err != nil) != (m.Status == "overflow") {
			t.Fatalf("budget %d: err %v but status %s", b, err, m.Status)
		}
		if m.Status != "overflow" && m.TotalTokens > b {
			t.Fatalf("budget %d: total %d is over budget", b, m.TotalTokens)
		}
		if (m.Status == "ok") != (n == 0) {
			t.Fatalf("budget %d: status %s with %d dropped", b, m.Status, n)
		}
		var want []string
		for _, c := range order[:n] {
			want = append(want, us[c.i].id)
		}
		if !sameSet(dropped, want) {
			t.Fatalf("budget %d: dropped %v, want the first %d in order: %v", b, dropped, n, want)
		}
		if m.Status == "trimmed" && totalAfter(n-1) <= b {
			t.Fatalf("budget %d: dropped %d items, but %d were enough", b, n, n-1)
		}
		for _, id := range protected {
			if !slices.Contains(ids(m, "included"), id) {
				t.Fatalf("budget %d: protected item %s was dropped", b, id)
			}
		}
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// order returns the ids in drop order.
func order(in Input, cfg Config) []string {
	us := units(in)
	var out []string
	for _, c := range dropOrder(us, in.User.Text, cfg.keepTurns()) {
		out = append(out, us[c.i].id)
	}
	return out
}

func TestRankTie(t *testing.T) {
	in, cfg := sample()
	in.Knowledge = []Item{{ID: "a", Text: "Lunch is at noon."}, {ID: "b", Text: "Parking is free."}}
	in.Tools = nil
	if got := order(in, cfg); !slices.Equal(got, []string{"b", "a"}) {
		t.Errorf("got %v, want the later item first", got)
	}
}

func TestKeepTurns(t *testing.T) {
	in, cfg := busy()
	cfg.KeepTurns = 4
	got := order(in, cfg)
	if slices.Contains(got, "h3") || !slices.Contains(got, "h1") {
		t.Errorf("got %v, want only h1 from history", got)
	}
}

func TestDuplicateAcrossLayers(t *testing.T) {
	in, cfg := sample()
	in.System = append(in.System, Item{ID: "s2", Text: "be  BRIEF."}) // system copies are kept
	in.Memory[0].Text = "Be brief."                                   // memory copy may go
	got := order(in, cfg)
	if got[0] != "m1" || slices.Contains(got, "s2") {
		t.Errorf("got %v, want m1 first and no s2", got)
	}
}
