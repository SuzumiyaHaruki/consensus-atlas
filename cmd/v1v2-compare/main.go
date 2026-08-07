package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/migrations/etcdraftv1v2"
)

func main() {
	out := flag.String("out", "", "write the deterministic comparison report; stdout when empty")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "v1v2-compare:", err)
		os.Exit(1)
	}
}

func run(out string) error {
	report, err := etcdraftv1v2.Run(context.Background())
	if err != nil {
		return err
	}
	if err := report.Validate(); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if out == "" {
		_, err = os.Stdout.Write(encoded)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(out, encoded, 0o644); err != nil {
		return err
	}
	fmt.Printf(
		"wrote %s\npassed=%d mismatched=%d deferred=%d qualified=%t digest=%s\n",
		out, report.Passed, report.Mismatched, report.Deferred, report.Qualified, report.Digest,
	)
	return nil
}
