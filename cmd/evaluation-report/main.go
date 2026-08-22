package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/evalreport"
)

type inputList []string

func (values *inputList) String() string { return strings.Join(*values, ",") }

func (values *inputList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "evaluation-report:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("evaluation-report", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var inputs inputList
	flags.Var(&inputs, "input", "method label and trusted evaluator JSON as label=path (repeatable)")
	outPath := flags.String("out", "", "optional Markdown output path; stdout when omitted")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(inputs) == 0 || flags.NArg() != 0 {
		return errors.New("at least one -input label=path and no positional arguments are required")
	}

	evidence := make([]evalreport.Evidence, 0, len(inputs))
	for _, input := range inputs {
		label, path, err := parseInput(input)
		if err != nil {
			return err
		}
		current, err := loadEvidence(label, path)
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		evidence = append(evidence, current)
	}
	report, err := evalreport.Build(evidence)
	if err != nil {
		return err
	}
	markdown := report.Markdown()
	if *outPath == "" {
		_, err = io.WriteString(stdout, markdown)
		return err
	}
	if err := os.WriteFile(*outPath, []byte(markdown), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s\n", *outPath)
	return nil
}

func parseInput(input string) (string, string, error) {
	label, path, ok := strings.Cut(input, "=")
	if !ok || strings.TrimSpace(label) == "" || strings.TrimSpace(path) == "" {
		return "", "", errors.New("input must use label=path")
	}
	return label, path, nil
}

func loadEvidence(label, path string) (evalreport.Evidence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return evalreport.Evidence{}, err
	}
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return evalreport.Evidence{}, err
	}
	switch envelope.SchemaVersion {
	case defectbench.FormalFreshEvaluationSchemaVersion:
		var report defectbench.FormalFreshEvaluation
		if err := decodeStrict(data, &report); err != nil {
			return evalreport.Evidence{}, err
		}
		return evalreport.FromFormal(label, report)
	case defectbench.AgenticHoldoutEvaluationSchemaVersion:
		var report defectbench.AgenticHoldoutEvaluation
		if err := decodeStrict(data, &report); err != nil {
			return evalreport.Evidence{}, err
		}
		return evalreport.FromAgentic(label, report)
	default:
		return evalreport.Evidence{}, fmt.Errorf("unsupported evaluator schema %q", envelope.SchemaVersion)
	}
}

func decodeStrict(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}
