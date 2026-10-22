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

// Assemble checks the input, renders the six layers in order and builds a manifest.
// A nil tokenizer means the estimator. Nothing is trimmed yet.
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
		InputHash:      hash(in),
		ConfigHash:     hash(cfg),
	}

	// parts[i] holds the rendered pieces of layer i.
	parts := make([][]string, len(Order))
	add := func(layer int, id, source string, critical bool, text string) {
		parts[layer] = append(parts[layer], text)
		m.Items = append(m.Items, ItemRef{ID: id, Layer: Order[layer], Source: source, Critical: critical, Tokens: tok.Count(text)})
	}
	for layer, items := range [][]Item{in.System, in.Memory, in.Knowledge} {
		for _, it := range items {
			head := "[" + it.ID + "]"
			if it.Source != "" {
				head += " " + it.Source
			}
			add(layer, it.ID, it.Source, it.Critical, head+"\n"+it.Text)
		}
	}
	for _, t := range in.Tools {
		add(3, t.Name, "", false, renderTool(t))
	}
	for _, t := range in.History {
		add(4, t.ID, "", false, t.Role+": "+t.Text)
	}
	add(5, "user", strings.Join(in.User.Attachments, ", "), true, renderUser(in.User))

	var p Payload
	for i, name := range Order {
		body := ""
		if len(parts[i]) > 0 {
			body = escape(strings.Join(parts[i], "\n\n")) + "\n"
		} else {
			m.Missing = append(m.Missing, name)
		}
		text := "<" + name + ">\n" + body + "</" + name + ">"
		n := tok.Count(text)
		p.Layers = append(p.Layers, Layer{Name: name, Text: text})
		m.Layers = append(m.Layers, LayerStat{Name: name, Items: len(parts[i]), Tokens: n})
		m.TotalTokens += n
	}

	m.OverBudget = m.TotalTokens > m.AvailableInput
	if m.Estimated {
		m.Warnings = append(m.Warnings, "token counts are estimated, not exact")
	}
	if m.OverBudget {
		m.Warnings = append(m.Warnings, fmt.Sprintf("over budget by %d tokens; trimming is not built yet", m.TotalTokens-m.AvailableInput))
	}
	return p, m, nil
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
