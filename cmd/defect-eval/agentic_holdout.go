package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const agenticHoldoutInputsSchemaVersion = "consensus-atlas/agentic-holdout-inputs/v1"

type agenticHoldoutTrialInput struct {
	TrialID    string `json:"trial_id"`
	EpisodeDir string `json:"episode_dir"`
}

// agenticHoldoutInputs is private evaluator I/O. Directory paths never enter
// the evaluation result or any Agent-facing material.
type agenticHoldoutInputs struct {
	SchemaVersion string                     `json:"schema_version"`
	Trials        []agenticHoldoutTrialInput `json:"trials"`
}

// agenticEpisodeSummaryProjection deliberately reads only method-reported
// navigation/accounting fields. Unknown summary fields remain outside the
// trusted verdict path; the full Bundle is validated independently.
type agenticEpisodeSummaryProjection struct {
	TargetID              string `json:"target_id"`
	MethodSpecDigest      string `json:"method_spec_digest"`
	Status                string `json:"status"`
	RiskAttempts          int    `json:"risk_attempts"`
	ScenarioAttempts      int    `json:"scenario_attempts"`
	ScenarioDecisionsUsed int    `json:"scenario_decisions_used"`
	PlanID                string `json:"plan_id"`
	RiskResultID          string `json:"risk_result_id"`
	TraceDigest           string `json:"trace_digest"`
	Budget                struct {
		MaxRiskCalls         int                                     `json:"max_risk_calls"`
		MaxScenarioCalls     int                                     `json:"max_scenario_calls"`
		MaxTotalCalls        int                                     `json:"max_total_calls"`
		MaxObservedTokens    int                                     `json:"max_observed_tokens"`
		MaxScenarioPlanSteps int                                     `json:"max_scenario_plan_steps"`
		MaxRuntimeDecisions  int                                     `json:"max_runtime_decisions"`
		Logical              *controlexperiment.AgenticLogicalBudget `json:"logical_budget"`
	} `json:"budget"`
	BranchEvidence []agenticBranchSummaryProjection `json:"branch_evidence"`
	Work           struct {
		Model                     controlexperiment.ModelWork        `json:"model"`
		ScenarioFrontier          controlexperiment.PhaseWork        `json:"scenario_frontier"`
		ScenarioSearch            controlexperiment.StatelessDFSWork `json:"scenario_search"`
		QualifiedExecution        controlexperiment.WorkLedger       `json:"qualified_execution"`
		BranchQualifiedExecutions []agenticBranchWorkProjection      `json:"branch_qualified_executions"`
	} `json:"work"`
	Assessment struct {
		Status string `json:"status"`
	} `json:"evidence_assessment"`
}

type agenticBranchSummaryProjection struct {
	BranchID          string                       `json:"branch_id"`
	Intent            string                       `json:"intent"`
	ReferenceBranchID string                       `json:"reference_branch_id"`
	PlanID            string                       `json:"plan_id"`
	RiskResultID      string                       `json:"risk_result_id"`
	TraceDigest       string                       `json:"trace_digest"`
	Work              controlexperiment.WorkLedger `json:"work"`
}

type agenticBranchWorkProjection struct {
	BranchID string                       `json:"branch_id"`
	Work     controlexperiment.WorkLedger `json:"work"`
}

// The private evaluator deliberately projects only the independently checked
// execution bundle from each persisted branch. Agent-reported Risk and Oracle
// fields remain outside the trusted verdict path.
type agenticBranchEvidenceProjection struct {
	BranchID          string          `json:"branch_id"`
	Intent            string          `json:"intent"`
	ReferenceBranchID string          `json:"reference_branch_id,omitempty"`
	Testing           json.RawMessage `json:"testing"`
}

type agenticTestingProjection struct {
	PlanID string                            `json:"plan_id"`
	Bundle controlexperiment.ExecutionBundle `json:"execution_bundle"`
	Risk   struct {
		ID string `json:"id"`
	} `json:"risk"`
}

func runAgenticHoldoutEvaluation(
	contractPath string,
	exposurePath string,
	inputsPath string,
	outPath string,
) error {
	var contract defectbench.FormalBenchmarkContract
	if err := readStrictJSON(contractPath, &contract); err != nil {
		return err
	}
	var exposure defectbench.FormalExposureAudit
	if err := readStrictJSON(exposurePath, &exposure); err != nil {
		return err
	}
	var inputs agenticHoldoutInputs
	if err := readStrictJSON(inputsPath, &inputs); err != nil {
		return err
	}
	evidence, err := loadAgenticHoldoutEvidence(inputsPath, inputs, contract)
	if err != nil {
		return err
	}
	projector, monitors, err := agenticHoldoutComposition(contract.Composition.ProjectorID)
	if err != nil {
		return err
	}
	report, err := defectbench.EvaluateAgenticHoldoutBundles(
		contract, exposure, evidence, projector, monitors...,
	)
	if err != nil {
		return err
	}
	if err := writeJSONExclusive(outPath, report); err != nil {
		return err
	}
	fmt.Printf(
		"wrote %s\ntarget=%s pairs=%d controls=%d false-positive=%d candidates=%d killed=%d invalid=%d\n",
		outPath, report.TargetID, len(report.Pairs), report.Summary.Controls,
		report.Summary.FalsePositives, report.Summary.Candidates,
		report.Summary.KilledCandidates, report.Summary.InvalidTrials,
	)
	return nil
}

func loadAgenticHoldoutEvidence(
	inputsPath string,
	inputs agenticHoldoutInputs,
	contract defectbench.FormalBenchmarkContract,
) (map[string]defectbench.AgenticTrialEvidence, error) {
	if inputs.SchemaVersion != agenticHoldoutInputsSchemaVersion ||
		len(inputs.Trials) != len(contract.Pairs)*2 {
		return nil, errors.New("AGENTIC_HOLDOUT_CLI_INPUT_SET_INVALID")
	}
	wanted := make(map[string]bool, len(inputs.Trials))
	for _, pair := range contract.Pairs {
		wanted[pair.Control.TrialID] = true
		wanted[pair.Candidate.TrialID] = true
	}
	base, err := filepath.Abs(filepath.Dir(inputsPath))
	if err != nil {
		return nil, err
	}
	seenTrials, seenDirectories := map[string]bool{}, map[string]bool{}
	evidence := make(map[string]defectbench.AgenticTrialEvidence, len(inputs.Trials))
	for _, input := range inputs.Trials {
		if !wanted[input.TrialID] || seenTrials[input.TrialID] || strings.TrimSpace(input.EpisodeDir) == "" {
			return nil, errors.New("AGENTIC_HOLDOUT_CLI_INPUT_SET_INVALID")
		}
		directory := input.EpisodeDir
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(base, filepath.Clean(directory))
		}
		directory, err = filepath.Abs(directory)
		if err != nil || seenDirectories[directory] {
			return nil, errors.New("AGENTIC_HOLDOUT_CLI_INPUT_SET_INVALID")
		}
		info, statErr := os.Lstat(directory)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_EPISODE_DIRECTORY_INVALID: %s", input.TrialID)
		}
		summary, err := readAgenticEpisodeSummary(filepath.Join(directory, "summary.json"))
		if err != nil {
			return nil, fmt.Errorf("load Agentic summary %s: %w", input.TrialID, err)
		}
		if !validAgenticEpisodeSummaryProjection(summary, contract) {
			return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_SUMMARY_INVALID: %s", input.TrialID)
		}
		current := defectbench.AgenticTrialEvidence{
			TargetID: summary.TargetID, MethodSpecDigest: summary.MethodSpecDigest,
			EpisodeStatus: summary.Status, EvidenceStatus: summary.Assessment.Status,
			Budget: *summary.Budget.Logical, RiskAttempts: summary.RiskAttempts,
			ScenarioAttempts:      summary.ScenarioAttempts,
			ScenarioDecisionsUsed: summary.ScenarioDecisionsUsed,
			ModelWork:             summary.Work.Model, ScenarioFrontier: summary.Work.ScenarioFrontier,
			ScenarioSearch: summary.Work.ScenarioSearch,
		}
		bundlePath := filepath.Join(directory, "bundle.json")
		if bundleInfo, bundleErr := os.Lstat(bundlePath); bundleErr == nil {
			if !bundleInfo.Mode().IsRegular() || bundleInfo.Mode()&os.ModeSymlink != 0 ||
				summary.PlanID == "" {
				return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BUNDLE_INVALID: %s", input.TrialID)
			}
			var bundle controlexperiment.ExecutionBundle
			if err := readStrictJSON(bundlePath, &bundle); err != nil ||
				!agenticSummaryBundleMatches(
					summary.MethodSpecDigest, summary.PlanID, summary.RiskResultID,
					summary.TraceDigest, summary.Work.QualifiedExecution, bundle,
				) {
				return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BUNDLE_INVALID: %s", input.TrialID)
			}
			current.Bundle = &bundle
		} else if !errors.Is(bundleErr, os.ErrNotExist) {
			return nil, bundleErr
		} else if summary.PlanID != "" {
			return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BUNDLE_INVALID: %s", input.TrialID)
		}
		branchPath := filepath.Join(directory, "branch-evidence.json")
		if branchInfo, branchErr := os.Lstat(branchPath); branchErr == nil {
			if !branchInfo.Mode().IsRegular() || branchInfo.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BRANCH_EVIDENCE_INVALID: %s", input.TrialID)
			}
			var branches []agenticBranchEvidenceProjection
			if err := readStrictJSON(branchPath, &branches); err != nil || len(branches) == 0 ||
				len(branches) > controlexperiment.ScenarioAgentMaxCalls ||
				len(branches) != len(summary.BranchEvidence) {
				return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BRANCH_EVIDENCE_INVALID: %s", input.TrialID)
			}
			seenBranches := make(map[string]bool, len(branches))
			for index, branch := range branches {
				declared := summary.BranchEvidence[index]
				if strings.TrimSpace(branch.BranchID) == "" || strings.TrimSpace(branch.Intent) == "" ||
					seenBranches[branch.BranchID] || len(branch.Testing) == 0 ||
					branch.BranchID != declared.BranchID || branch.Intent != declared.Intent ||
					branch.ReferenceBranchID != declared.ReferenceBranchID {
					return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BRANCH_EVIDENCE_INVALID: %s", input.TrialID)
				}
				var testing agenticTestingProjection
				if err := json.Unmarshal(branch.Testing, &testing); err != nil ||
					!agenticSummaryBundleMatches(
						summary.MethodSpecDigest, declared.PlanID, declared.RiskResultID,
						declared.TraceDigest, declared.Work, testing.Bundle,
					) || testing.PlanID != declared.PlanID || testing.Risk.ID != declared.RiskResultID {
					return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BRANCH_EVIDENCE_INVALID: %s", input.TrialID)
				}
				current.CandidateBundles = append(current.CandidateBundles, testing.Bundle)
				seenBranches[branch.BranchID] = true
			}
		} else if !errors.Is(branchErr, os.ErrNotExist) {
			return nil, branchErr
		} else if len(summary.BranchEvidence) != 0 {
			return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BRANCH_EVIDENCE_INVALID: %s", input.TrialID)
		}
		evidence[input.TrialID] = current
		seenTrials[input.TrialID], seenDirectories[directory] = true, true
	}
	return evidence, nil
}

func validAgenticEpisodeSummaryProjection(
	summary agenticEpisodeSummaryProjection,
	contract defectbench.FormalBenchmarkContract,
) bool {
	hasPrimary := summary.PlanID != "" && summary.RiskResultID != "" && summary.TraceDigest != ""
	if summary.TargetID == "" || summary.MethodSpecDigest != contract.MethodSpecDigest ||
		summary.Budget.Logical == nil || summary.Budget.Logical.Validate() != nil ||
		contract.AgenticBudget == nil || *summary.Budget.Logical != *contract.AgenticBudget ||
		summary.Budget.MaxRiskCalls <= 0 || summary.Budget.MaxScenarioCalls <= 0 ||
		summary.Budget.MaxRiskCalls+summary.Budget.MaxScenarioCalls > summary.Budget.MaxTotalCalls ||
		summary.Budget.MaxTotalCalls != summary.Budget.Logical.MaxModelCalls ||
		summary.Budget.MaxObservedTokens != summary.Budget.Logical.MaxModelTokens ||
		summary.Budget.MaxScenarioPlanSteps <= 0 ||
		summary.Budget.MaxRuntimeDecisions <= 0 ||
		summary.Budget.MaxRuntimeDecisions > summary.Budget.Logical.MaxPrimarySchedulerDecisions ||
		summary.RiskAttempts < 0 || summary.ScenarioAttempts < 0 ||
		summary.RiskAttempts > summary.Budget.MaxRiskCalls ||
		summary.ScenarioAttempts > summary.Budget.MaxScenarioCalls ||
		summary.RiskAttempts+summary.ScenarioAttempts != summary.Work.Model.Calls ||
		summary.ScenarioDecisionsUsed < 0 ||
		summary.ScenarioDecisionsUsed > summary.Budget.MaxRuntimeDecisions ||
		hasPrimary != (summary.PlanID != "" || summary.RiskResultID != "" || summary.TraceDigest != "") ||
		!hasPrimary && !reflect.DeepEqual(
			summary.Work.QualifiedExecution, controlexperiment.WorkLedger{},
		) ||
		len(summary.BranchEvidence) != len(summary.Work.BranchQualifiedExecutions) ||
		len(summary.BranchEvidence) > summary.ScenarioAttempts ||
		len(summary.BranchEvidence) > controlexperiment.ScenarioAgentMaxCalls {
		return false
	}
	for index, branch := range summary.BranchEvidence {
		work := summary.Work.BranchQualifiedExecutions[index]
		if branch.BranchID == "" || branch.Intent == "" || branch.PlanID == "" ||
			branch.RiskResultID == "" || branch.TraceDigest == "" ||
			work.BranchID != branch.BranchID || !reflect.DeepEqual(work.Work, branch.Work) {
			return false
		}
	}
	return true
}

func agenticSummaryBundleMatches(
	methodSpecDigest string,
	planID string,
	riskResultID string,
	traceDigest string,
	work controlexperiment.WorkLedger,
	bundle controlexperiment.ExecutionBundle,
) bool {
	return planID != "" && riskResultID != "" && traceDigest != "" &&
		bundle.Validate() == nil &&
		bundle.SchemaVersion == controlexperiment.ExecutionBundleSchemaVersionV3 &&
		bundle.Identity.MethodSpecDigest == methodSpecDigest &&
		bundle.Trace.Digest == traceDigest && reflect.DeepEqual(bundle.Work, work)
}

func readAgenticEpisodeSummary(path string) (agenticEpisodeSummaryProjection, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 512<<10 {
		return agenticEpisodeSummaryProjection{}, errors.New("AGENTIC_HOLDOUT_CLI_SUMMARY_INVALID")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return agenticEpisodeSummaryProjection{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var summary agenticEpisodeSummaryProjection
	if err := decoder.Decode(&summary); err != nil {
		return agenticEpisodeSummaryProjection{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agenticEpisodeSummaryProjection{}, errors.New("AGENTIC_HOLDOUT_CLI_SUMMARY_TRAILING_JSON")
	}
	return summary, nil
}

func agenticHoldoutComposition(
	projectorID string,
) (semantic.DecisionProjector, []oracle.BundleMonitor, error) {
	monitors := []oracle.BundleMonitor{oracle.BundleAgreement{}}
	switch projectorID {
	case etcdraftv2.DecisionProjectionID:
		return etcdraftv2.DecisionProjector{}, monitors, nil
	case omnipaxosv2.DecisionProjectionID:
		return omnipaxosv2.DecisionProjector{}, monitors, nil
	default:
		return nil, nil, errors.New("AGENTIC_HOLDOUT_CLI_PROJECTOR_UNSUPPORTED")
	}
}
