// Package asm builds one LLM input from six context layers, in a fixed order.
package asm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Order is the only allowed layer order.
var Order = []string{
	"system_instructions",
	"state_and_memory",
	"retrieved_knowledge",
	"tool_definitions",
	"conversation_history",
	"current_user_input",
}

// ErrOverflow means the input does not fit, even after dropping all it may drop.
var ErrOverflow = errors.New("overflow: input does not fit the budget")

// Assemble checks the input, drops items until it fits the budget,
// renders the six layers in order and builds a manifest.
// A nil tokenizer means the estimator.
// On ErrOverflow the payload is empty but the manifest is filled.
func Assemble(in Input, cfg Config, tok Tokenizer) (Payload, Manifest, error) {
	if err := check(in, cfg); err != nil {
		return Payload{}, Manifest{}, err
	}
	if tok == nil {
		tok = Estimator{CharsPerToken: cfg.CharsPerToken}
	}
	m := Manifest{
		ConfigVersion:  cfg.Version,
		Model:          cfg.Model,
		Tokenizer:      tok.Name(),
		Estimated:      !tok.Exact(),
		AvailableInput: cfg.Available(),
		ReservedOutput: cfg.ReservedOutput,
		Status:         "ok",
		InputHash:      hash(in),
		ConfigHash:     hash(cfg),
	}
	if m.Estimated {
		m.Warnings = append(m.Warnings, "token counts are estimated, not exact")
	}

	us := units(in)
	for i, name := range Order {
		if !slices.ContainsFunc(us, func(u unit) bool { return u.layer == i }) {
			m.Missing = append(m.Missing, name)
		}
	}
	order := dropOrder(us, in.User.Text, cfg.keepTurns())

	// Render, and while something is over budget, drop more and render again.
	var p Payload
	for {
		p, m.Layers, m.TotalTokens = render(us, tok)
		layer, excess := overBy(m.Layers, m.TotalTokens, cfg)
		if excess <= 0 {
			break
		}
		if !drop(us, order, layer, excess, tok) {
			where := "total"
			if layer >= 0 {
				where = Order[layer]
			}
			m.Warnings = append(m.Warnings, fmt.Sprintf("%s is %d tokens over budget and nothing is left to drop", where, excess))
			m.Status, p = "overflow", Payload{}
			break
		}
		m.Status = "trimmed"
	}

	for _, u := range us {
		ref := ItemRef{ID: u.id, Layer: Order[u.layer], Source: strings.Join(u.sources, ", "), Critical: u.critical, Tokens: tok.Count(u.piece()), Action: "included"}
		if u.dropped {
			ref.Action, ref.Reason = "dropped", u.reason
		}
		m.Items = append(m.Items, ref)
	}
	if m.Status == "overflow" {
		return p, m, ErrOverflow
	}
	return p, m, nil
}

// render builds every layer from the units that are not dropped, and counts tokens.
func render(us []unit, tok Tokenizer) (Payload, []LayerStat, int) {
	parts := make([][]string, len(Order))
	for _, u := range us {
		if !u.dropped {
			parts[u.layer] = append(parts[u.layer], u.piece())
		}
	}
	var p Payload
	var stats []LayerStat
	total := 0
	for i, name := range Order {
		body := ""
		if len(parts[i]) > 0 {
			body = escape(strings.Join(parts[i], "\n\n")) + "\n"
		}
		text := "<" + name + ">\n" + body + "</" + name + ">"
		n := tok.Count(text)
		p.Layers = append(p.Layers, Layer{Name: name, Text: text})
		stats = append(stats, LayerStat{Name: name, Items: len(parts[i]), Tokens: n})
		total += n
	}
	return p, stats, total
}

// overBy finds the first layer over its cap, else checks the total.
// It returns the layer (-1 for total) and how many tokens too many.
func overBy(stats []LayerStat, total int, cfg Config) (int, int) {
	for i, s := range stats {
		if c := cfg.LayerBudgets[s.Name]; c > 0 && s.Tokens > c {
			return i, s.Tokens - c
		}
	}
	return -1, total - cfg.Available()
}

func renderTool(t Tool) string {
	s := "name: " + t.Name + "\ndescription: " + t.Description
	if len(t.Schema) > 0 {
		var b bytes.Buffer
		json.Compact(&b, t.Schema) // schema was checked already
		s += "\ninput_schema: " + b.String()
	}
	return s
}

// renderUser puts params and attachments first, so the request itself comes last.
func renderUser(u User) string {
	s := ""
	if len(u.Params) > 0 {
		s += "params:\n"
		for _, k := range slices.Sorted(maps.Keys(u.Params)) {
			s += "- " + k + ": " + u.Params[k] + "\n"
		}
		s += "\n"
	}
	if len(u.Attachments) > 0 {
		s += "attachments:\n"
		for _, a := range u.Attachments {
			s += "- " + a + "\n"
		}
		s += "\n"
	}
	return s + u.Text
}

// tagRE finds text that looks like one of our layer tags.
var tagRE = regexp.MustCompile(`(?i)<\s*(/?)\s*(` + strings.Join(Order, "|") + `)`)

// escape stops text from opening or closing a layer tag.
func escape(s string) string { return tagRE.ReplaceAllString(s, "&lt;$1$2") }

func check(in Input, cfg Config) error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if cfg.Version != "1" {
		bad("config: version must be \"1\", got %q", cfg.Version)
	}
	if cfg.Model == "" {
		bad("config: model is empty")
	}
	if cfg.ContextWindow <= 0 {
		bad("config: context_window must be > 0")
	}
	if cfg.MaxInput < 0 || cfg.ReservedOutput < 0 || cfg.SafetyMargin < 0 {
		bad("config: limits must not be negative")
	}
	if cfg.ContextWindow > 0 && cfg.Available() <= 0 {
		bad("config: no room left for input")
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.LayerBudgets)) {
		if !slices.Contains(Order, name) {
			bad("config: layer_budgets: unknown layer %q", name)
		} else if cfg.LayerBudgets[name] <= 0 {
			bad("config: layer_budgets: %s must be > 0", name)
		}
	}
	if cfg.KeepTurns < 0 {
		bad("config: keep_turns must not be negative")
	}

	seen := map[string]bool{}
	id := func(layer, id string) {
		switch {
		case id == "":
			bad("%s: an item has no id", layer)
		case seen[id]:
			bad("%s: duplicate id %q", layer, id)
		}
		seen[id] = true
	}
	for i, items := range [][]Item{in.System, in.Memory, in.Knowledge} {
		for _, it := range items {
			id(Order[i], it.ID)
			if strings.TrimSpace(it.Text) == "" {
				bad("%s: item %q has no text", Order[i], it.ID)
			}
		}
	}
	tools := map[string]bool{}
	for _, t := range in.Tools {
		switch {
		case t.Name == "":
			bad("tool_definitions: a tool has no name")
		case tools[t.Name]:
			bad("tool_definitions: duplicate tool %q", t.Name)
		case len(t.Schema) > 0 && !json.Valid(t.Schema):
			bad("tool_definitions: tool %q has a bad schema", t.Name)
		}
		tools[t.Name] = true
	}
	for _, t := range in.History {
		id("conversation_history", t.ID)
		if t.Role != "user" && t.Role != "assistant" {
			bad("conversation_history: turn %q has role %q, want user or assistant", t.ID, t.Role)
		}
	}
	if strings.TrimSpace(in.User.Text) == "" {
		bad("current_user_input: text is empty")
	}
	return errors.Join(errs...)
}

// hash gives a short, stable fingerprint of v.
func hash(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:8])
}
