package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	etcdqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
	hashicorpqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/hashicorpraftv2"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "adapter-qualify:", err)
		os.Exit(1)
	}
}

func run() error {
	target := flag.String("target", "", "qualification target: etcdraftv2 or hashicorpraftv2")
	out := flag.String("out", "", "qualification bundle output path")
	flag.Parse()
	if *out == "" {
		return errors.New("-out is required")
	}
	ctx := context.Background()
	var bundle conformance.QualificationBundle
	var err error
	switch *target {
	case "etcdraftv2":
		bundle, err = etcdqualification.Run(ctx)
	case "hashicorpraftv2":
		bundle, err = hashicorpqualification.Run(ctx)
	default:
		return fmt.Errorf("unsupported -target %q", *target)
	}
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(*out, append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("qualified=%t required=%d validated=%d/%d unsupported=%d digest=%s\n",
		bundle.Qualification.Qualified, bundle.Qualification.Summary.Required,
		bundle.Qualification.Summary.Validated, bundle.Qualification.Summary.Total,
		bundle.Qualification.Summary.Unsupported, bundle.Digest)
	return nil
}
