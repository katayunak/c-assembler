package asm

import (
	"encoding/json"
	"flag"
	"os"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in examples/")

// TestGolden checks that the examples still match the code's output.
func TestGolden(t *testing.T) {
	var in Input
	readJSON(t, "../examples/input.json", &in)
	cases := []struct{ config, payload, manifest string }{
		{"config.json", "payload.txt", "manifest.json"},
		{"config-small.json", "payload-small.txt", "manifest-small.json"},
	}
	for _, c := range cases {
		t.Run(c.config, func(t *testing.T) {
			var cfg Config
			readJSON(t, "../examples/"+c.config, &cfg)
			p, m := run(t, in, cfg, nil)
			b, _ := json.MarshalIndent(m, "", "  ")
			golden(t, "../examples/"+c.payload, p.Text()+"\n")
			golden(t, "../examples/"+c.manifest, string(b)+"\n")
		})
	}
}

func golden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s changed; if on purpose, run: go test ./asm -update", path)
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}
