package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	qualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/hashicorpraftv2"
)

func main() {
	var out string
	flag.StringVar(&out, "out", "", "qualification bundle output path")
	flag.Parse()
	if out == "" {
		fatal(fmt.Errorf("-out is required"))
	}
	bundle, err := qualification.Run(context.Background())
	if err != nil {
		fatal(err)
	}
	encoded, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(out, append(encoded, '\n'), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("qualified=%t required=%d validated=%d/%d unsupported=%d digest=%s\n",
		bundle.Qualification.Qualified, bundle.Qualification.Summary.Required,
		bundle.Qualification.Summary.Validated, bundle.Qualification.Summary.Total,
		bundle.Qualification.Summary.Unsupported, bundle.Digest)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
