// Command ctxasm builds an LLM input from six context layers.
//
//	ctxasm assemble -c config.json -i input.json [-m manifest.json]
//	ctxasm inspect  -c config.json -i input.json
//
// Exit codes: 0 ok, 1 error or overflow, 2 bad usage.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"ctxasm/asm"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run is main without os.Exit, so tests can call it. It returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || (args[0] != "assemble" && args[0] != "inspect") {
		fmt.Fprintln(stderr, "usage: ctxasm assemble|inspect -c config.json -i input.json [-m manifest.json]")
		return 2
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("c", "config.json", "config file")
	inPath := fs.String("i", "input.json", "input file")
	manPath := fs.String("m", "", "write the manifest to this file (assemble only)")
	if fs.Parse(args[1:]) != nil {
		return 2
	}

	var cfg asm.Config
	var in asm.Input
	if err := errors.Join(load(*cfgPath, &cfg), load(*inPath, &in)); err != nil {
		return fail(stderr, err)
	}
	p, m, err := asm.Assemble(in, cfg, nil)
	if err != nil && !errors.Is(err, asm.ErrOverflow) {
		return fail(stderr, err)
	}

	// On overflow, still show the manifest, then fail.
	if args[0] == "inspect" {
		inspect(stdout, m)
	} else {
		if err == nil {
			fmt.Fprintln(stdout, p.Text())
		}
		if *manPath != "" {
			b, _ := json.MarshalIndent(m, "", "  ")
			if werr := os.WriteFile(*manPath, append(b, '\n'), 0o644); werr != nil {
				return fail(stderr, werr)
			}
		}
	}
	if err != nil {
		return fail(stderr, err)
	}
	return 0
}

// load reads JSON and rejects unknown fields, so typos show up.
func load(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func inspect(out io.Writer, m asm.Manifest) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "layer\titems\ttokens")
	for _, l := range m.Layers {
		fmt.Fprintf(w, "%s\t%d\t%d\n", l.Name, l.Items, l.Tokens)
	}
	fmt.Fprintf(w, "total\t\t%d\n", m.TotalTokens)
	w.Flush()
	fmt.Fprintf(out, "\nmodel:     %s\ntokenizer: %s\nbudget:    %d of %d input tokens\nstatus:    %s\n",
		m.Model, m.Tokenizer, m.TotalTokens, m.AvailableInput, m.Status)
	for _, it := range m.Items {
		if it.Action == "dropped" {
			fmt.Fprintf(out, "dropped: %s (%s): %s\n", it.ID, it.Layer, it.Reason)
		}
	}
	for _, s := range m.Missing {
		fmt.Fprintln(out, "missing:", s)
	}
	for _, s := range m.Warnings {
		fmt.Fprintln(out, "warning:", s)
	}
}

func fail(w io.Writer, err error) int {
	fmt.Fprintln(w, "error:", err)
	return 1
}
