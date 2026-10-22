// Command ctxasm builds an LLM input from six context layers.
//
//	ctxasm assemble -c config.json -i input.json [-m manifest.json]
//	ctxasm inspect  -c config.json -i input.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"ctxasm/asm"
)

func main() {
	if len(os.Args) < 2 || (os.Args[1] != "assemble" && os.Args[1] != "inspect") {
		fmt.Fprintln(os.Stderr, "usage: ctxasm assemble|inspect -c config.json -i input.json [-m manifest.json]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	cfgPath := fs.String("c", "config.json", "config file")
	inPath := fs.String("i", "input.json", "input file")
	manPath := fs.String("m", "", "write the manifest to this file (assemble only)")
	fs.Parse(os.Args[2:])

	var cfg asm.Config
	var in asm.Input
	must(load(*cfgPath, &cfg))
	must(load(*inPath, &in))
	p, m, err := asm.Assemble(in, cfg, nil)
	must(err)

	if os.Args[1] == "inspect" {
		inspect(m)
		return
	}
	fmt.Println(p.Text())
	if *manPath != "" {
		b, _ := json.MarshalIndent(m, "", "  ")
		must(os.WriteFile(*manPath, append(b, '\n'), 0o644))
	}
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

func inspect(m asm.Manifest) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "layer\titems\ttokens")
	for _, l := range m.Layers {
		fmt.Fprintf(w, "%s\t%d\t%d\n", l.Name, l.Items, l.Tokens)
	}
	fmt.Fprintf(w, "total\t\t%d\n", m.TotalTokens)
	w.Flush()
	fmt.Printf("\nmodel:       %s\ntokenizer:   %s\nbudget:      %d of %d input tokens\nover_budget: %v\n",
		m.Model, m.Tokenizer, m.TotalTokens, m.AvailableInput, m.OverBudget)
	for _, s := range m.Missing {
		fmt.Println("missing:", s)
	}
	for _, s := range m.Warnings {
		fmt.Println("warning:", s)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
