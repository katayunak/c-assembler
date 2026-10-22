package asm

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// unit is one piece of a layer: an item, a tool, a turn or the request.
type unit struct {
	id, body string
	sources  []string
	layer    int
	head     bool // print "[id] sources" before the body
	critical bool
	dropped  bool
	reason   string
	dupOf    int // index of the kept copy, or -1
}

func (u unit) piece() string {
	if !u.head {
		return u.body
	}
	h := "[" + u.id + "]"
	if len(u.sources) > 0 {
		h += " " + strings.Join(u.sources, ", ")
	}
	return h + "\n" + u.body
}

func units(in Input) []unit {
	var us []unit
	add := func(u unit) {
		u.dupOf = -1
		us = append(us, u)
	}
	for layer, items := range [][]Item{in.System, in.Memory, in.Knowledge} {
		for _, it := range items {
			u := unit{id: it.ID, body: it.Text, layer: layer, head: true, critical: it.Critical}
			if it.Source != "" {
				u.sources = []string{it.Source}
			}
			add(u)
		}
	}
	for _, t := range in.Tools {
		add(unit{id: t.Name, body: renderTool(t), layer: 3, critical: t.Critical})
	}
	for _, t := range in.History {
		add(unit{id: t.ID, body: t.Role + ": " + t.Text, layer: 4, critical: t.Critical})
	}
	add(unit{id: "user", body: renderUser(in.User), layer: 5, critical: true, sources: in.User.Attachments})
	return us
}

// cand is a unit that may be dropped, and why.
type cand struct {
	i, score int
	reason   string
}

// dropOrder lists what may be dropped, first to last:
// duplicates, low-rank knowledge, old turns, low-rank tools.
// System items, other memory, critical items, the last turns and the request are never listed.
func dropOrder(us []unit, request string, keepTurns int) []cand {
	req := wordSet(request)
	seen := map[string]int{}
	var dups, kb, turns, tools []cand
	var hist []int
	for i := range us {
		u := &us[i]
		switch {
		case u.head:
			key := norm(u.body)
			if k, ok := seen[key]; !ok {
				seen[key] = i
			} else if u.layer > 0 && !u.critical {
				u.dupOf = k
				dups = append(dups, cand{i: i, reason: "duplicate of " + us[k].id})
				continue
			}
			if u.layer == 2 && !u.critical {
				kb = append(kb, ranked(i, u.body, req))
			}
		case u.layer == 3 && !u.critical:
			tools = append(tools, ranked(i, u.body, req))
		case u.layer == 4:
			hist = append(hist, i)
		}
	}
	for _, i := range hist[:max(0, len(hist)-keepTurns)] {
		if !us[i].critical {
			turns = append(turns, cand{i: i, reason: "old turn"})
		}
	}
	slices.SortFunc(kb, byRank)
	slices.SortFunc(tools, byRank)
	return slices.Concat(dups, kb, turns, tools)
}

// drop marks units as dropped, in order, until about excess tokens are freed.
// If layer is not -1, only units in that layer count. It reports whether anything was dropped.
func drop(us []unit, order []cand, layer, excess int, tok Tokenizer) bool {
	done := false
	for _, c := range order {
		if excess <= 0 {
			break
		}
		u := &us[c.i]
		if u.dropped || (layer >= 0 && u.layer != layer) {
			continue
		}
		u.dropped, u.reason = true, c.reason
		if u.dupOf >= 0 { // keep its sources on the kept copy
			k := &us[u.dupOf]
			for _, s := range u.sources {
				if !slices.Contains(k.sources, s) {
					k.sources = append(k.sources, s)
				}
			}
		}
		excess -= tok.Count(u.piece())
		done = true
	}
	return done
}

// ranked scores a unit by how many useful words it shares with the request.
func ranked(i int, text string, req map[string]bool) cand {
	n := 0
	for w := range wordSet(text) {
		if req[w] {
			n++
		}
	}
	return cand{i: i, score: n, reason: fmt.Sprintf("low rank (%d shared words)", n)}
}

// byRank puts the lowest score first; on a tie, the later item goes first.
func byRank(a, b cand) int {
	if a.score != b.score {
		return a.score - b.score
	}
	return b.i - a.i
}

var stop = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields("the and for are but not you your with this that from have has was were how what when where which who why can should would could will into about there their them they then than does did all any") {
		m[w] = true
	}
	return m
}()

// wordSet returns the useful words in s: lower case, 3+ letters, no stop words.
func wordSet(s string) map[string]bool {
	set := map[string]bool{}
	split := func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }
	for _, w := range strings.FieldsFunc(strings.ToLower(s), split) {
		if utf8.RuneCountInString(w) > 2 && !stop[w] {
			set[w] = true
		}
	}
	return set
}

// norm makes texts equal when only case or spacing differs.
func norm(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
