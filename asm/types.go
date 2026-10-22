package asm

import (
	"encoding/json"
	"strings"
)

// Item is one piece of context: a rule, a memory or a doc chunk.
type Item struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Source   string `json:"source,omitempty"`
	Critical bool   `json:"critical,omitempty"` // never drop (used once trimming exists)
}

// Tool is one tool the model may call.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"input_schema,omitempty"`
}

// Turn is one past message.
type Turn struct {
	ID   string `json:"id"`
	Role string `json:"role"` // "user" or "assistant"
	Text string `json:"text"`
}

// User is the current request.
type User struct {
	Text        string            `json:"text"`
	Params      map[string]string `json:"params,omitempty"`
	Attachments []string          `json:"attachments,omitempty"`
}

// Input holds the six layers.
type Input struct {
	System    []Item `json:"system_instructions"`
	Memory    []Item `json:"state_and_memory"`
	Knowledge []Item `json:"retrieved_knowledge"`
	Tools     []Tool `json:"tool_definitions"`
	History   []Turn `json:"conversation_history"`
	User      User   `json:"current_user_input"`
}

// Config sets the model and its limits.
type Config struct {
	Version        string  `json:"version"` // must be "1"
	Model          string  `json:"model"`
	ContextWindow  int     `json:"context_window"`
	MaxInput       int     `json:"max_input,omitempty"` // 0 means no extra cap
	ReservedOutput int     `json:"reserved_output"`
	SafetyMargin   int     `json:"safety_margin"`
	CharsPerToken  float64 `json:"chars_per_token,omitempty"` // estimator ratio; 0 means 3.5
}

// Available is the input token budget.
func (c Config) Available() int {
	n := c.ContextWindow - c.ReservedOutput - c.SafetyMargin
	if c.MaxInput > 0 && c.MaxInput < n {
		n = c.MaxInput
	}
	return n
}

// Payload is the built input: one entry per layer, in order.
type Payload struct {
	Layers []Layer `json:"layers"`
}

type Layer struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// Text joins all layers into one string.
func (p Payload) Text() string {
	s := make([]string, len(p.Layers))
	for i, l := range p.Layers {
		s[i] = l.Text
	}
	return strings.Join(s, "\n\n")
}

// Manifest says what was built. It never holds item text.
type Manifest struct {
	ConfigVersion  string      `json:"config_version"`
	Model          string      `json:"model"`
	Tokenizer      string      `json:"tokenizer"`
	Estimated      bool        `json:"estimated"`
	Layers         []LayerStat `json:"layers"`
	TotalTokens    int         `json:"total_tokens"`
	AvailableInput int         `json:"available_input_tokens"`
	ReservedOutput int         `json:"reserved_output_tokens"`
	OverBudget     bool        `json:"over_budget"`
	Items          []ItemRef   `json:"items"`
	Missing        []string    `json:"missing,omitempty"`
	Warnings       []string    `json:"warnings,omitempty"`
	InputHash      string      `json:"input_hash"`
	ConfigHash     string      `json:"config_hash"`
}

type LayerStat struct {
	Name   string `json:"name"`
	Items  int    `json:"items"`
	Tokens int    `json:"tokens"`
}

type ItemRef struct {
	ID       string `json:"id"`
	Layer    string `json:"layer"`
	Source   string `json:"source,omitempty"`
	Critical bool   `json:"critical,omitempty"`
	Tokens   int    `json:"tokens"`
}
