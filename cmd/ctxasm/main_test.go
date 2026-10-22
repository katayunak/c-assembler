package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ex = "../../examples/"

func call(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestAssembleMatchesExamples(t *testing.T) {
	man := filepath.Join(t.TempDir(), "manifest.json")
	code, out, errOut := call("assemble", "-c", ex+"config.json", "-i", ex+"input.json", "-m", man)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if want, _ := os.ReadFile(ex + "payload.txt"); out != string(want) {
		t.Error("payload differs from examples/payload.txt")
	}
	got, _ := os.ReadFile(man)
	if want, _ := os.ReadFile(ex + "manifest.json"); !bytes.Equal(got, want) {
		t.Error("manifest differs from examples/manifest.json")
	}
}

func TestExitCodes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	tiny := write("tiny.json", `{"version":"1","model":"m","context_window":400,"reserved_output":200,"safety_margin":50}`)
	typo := write("typo.json", `{"version":"1","model":"m","context_window":1000,"reserved_output":0,"safety_margn":0}`)
	in := ex + "input.json"

	cases := []struct {
		name           string
		args           []string
		code           int
		stdout, stderr string
	}{
		{"inspect trimmed", []string{"inspect", "-c", ex + "config-small.json", "-i", in}, 0, "status:    trimmed", ""},
		{"assemble overflow", []string{"assemble", "-c", tiny, "-i", in}, 1, "", "overflow"},
		{"inspect overflow", []string{"inspect", "-c", tiny, "-i", in}, 1, "status:    overflow", "overflow"},
		{"typo in config", []string{"assemble", "-c", typo, "-i", in}, 1, "", "unknown field"},
		{"missing file", []string{"assemble", "-c", "nope.json", "-i", in}, 1, "", "nope.json"},
		{"no command", nil, 2, "", "usage"},
		{"bad flag", []string{"assemble", "-x"}, 2, "", "not defined"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, errOut := call(c.args...)
			if code != c.code || !strings.Contains(out, c.stdout) || !strings.Contains(errOut, c.stderr) {
				t.Errorf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
			}
			if len(c.args) > 0 && c.args[0] == "assemble" && code != 0 && out != "" {
				t.Error("payload printed on failure")
			}
		})
	}
}
