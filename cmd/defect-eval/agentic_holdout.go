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

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

const agenticHoldoutInputsSchemaVersion = "consensus-atlas/agentic-holdout-inputs/v2"

type agenticHoldoutTrialInput struct {
	TrialID          string `json:"trial_id"`
	EpisodeDir       string `json:"episode_dir,omitempty"`
	InvestigationDir string `json:"investigation_dir,omitempty"`
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
	targetID, err := agenticHoldoutTargetID(evidence)
	if err != nil {
		return err
	}
	projector, registry, err := targetoracles.Resolve(targetID, contract.Composition.ProjectorID)
	if err != nil {
		return errors.New("AGENTIC_HOLDOUT_CLI_TARGET_ORACLE_COMPOSITION_UNSUPPORTED")
	}
	report, err := defectbench.EvaluateAgenticHoldoutBundles(
		contract, exposure, evidence, projector, registry.EvaluationMonitors()...,
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
		if !wanted[input.TrialID] || seenTrials[input.TrialID] {
			return nil, errors.New("AGENTIC_HOLDOUT_CLI_INPUT_SET_INVALID")
		}
		directories, methodSpec, err := resolveAgenticTrialDirectories(base, input)
		if err != nil {
			return nil, fmt.Errorf("load Agentic trial %s: %w", input.TrialID, err)
		}
		if methodSpec.Digest != contract.MethodSpecDigest || contract.AgenticBudget == nil ||
			methodSpec.InvestigationBudget != *contract.AgenticBudget {
			return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_METHOD_SPEC_INVALID: %s", input.TrialID)
		}
		for _, directory := range directories {
			if seenDirectories[directory] {
				return nil, errors.New("AGENTIC_HOLDOUT_CLI_INPUT_SET_INVALID")
			}
			seenDirectories[directory] = true
		}
		current := defectbench.AgenticTrialEvidence{
			MethodSpecDigest: methodSpec.Digest, MethodSpec: methodSpec,
			EpisodeCount: len(directories), EpisodeStatus: defectbench.AgenticEpisodeCompleted,
			Budget: methodSpec.InvestigationBudget,
		}
		for _, directory := range directories {
			episode, err := loadAgenticEpisodeEvidence(directory, input.TrialID, contract, methodSpec)
			if err != nil {
				return nil, err
			}
			if current.TargetID == "" {
				current.TargetID = episode.TargetID
			} else if current.TargetID != episode.TargetID {
				return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_TARGET_DRIFT: %s", input.TrialID)
			}
			if episode.EpisodeStatus != defectbench.AgenticEpisodeCompleted {
				current.EpisodeStatus = episode.EpisodeStatus
			}
			current.EvidenceStatus = episode.EvidenceStatus
			if !mergeAgenticEpisodeEvidence(&current, episode) {
				return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_WORK_OVERFLOW: %s", input.TrialID)
			}
		}
		if current.Bundle != nil || len(current.CandidateBundles) != 0 {
			current.EpisodeStatus = defectbench.AgenticEpisodeCompleted
		}
		evidence[input.TrialID] = current
		seenTrials[input.TrialID] = true
	}
	return evidence, nil
}

func resolveAgenticTrialDirectories(
	base string,
	input agenticHoldoutTrialInput,
) ([]string, controlexperiment.AgenticMethodSpec, error) {
	episode := strings.TrimSpace(input.EpisodeDir)
	investigation := strings.TrimSpace(input.InvestigationDir)
	if (episode == "") == (investigation == "") {
		return nil, controlexperiment.AgenticMethodSpec{}, errors.New("AGENTIC_HOLDOUT_CLI_INPUT_SET_INVALID")
	}
	path := episode
	if path == "" {
		path = investigation
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, filepath.Clean(path))
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, controlexperiment.AgenticMethodSpec{}, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, controlexperiment.AgenticMethodSpec{}, errors.New("AGENTIC_HOLDOUT_CLI_DIRECTORY_INVALID")
	}
	if episode != "" {
		spec, err := readAgenticHoldoutMethodSpec(path)
		if err != nil || spec.InvestigationEpisodes != 1 {
			return nil, controlexperiment.AgenticMethodSpec{}, errors.New("AGENTIC_HOLDOUT_CLI_SINGLE_EPISODE_METHOD_INVALID")
		}
		return []string{path}, spec, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) == 0 {
		return nil, controlexperiment.AgenticMethodSpec{}, errors.New("AGENTIC_HOLDOUT_CLI_INVESTIGATION_INVALID")
	}
	first := filepath.Join(path, "episode-0001")
	spec, err := readAgenticHoldoutMethodSpec(first)
	if err != nil || len(entries) != spec.InvestigationEpisodes {
		return nil, controlexperiment.AgenticMethodSpec{}, errors.New("AGENTIC_HOLDOUT_CLI_INVESTIGATION_INVALID")
	}
	directories := make([]string, spec.InvestigationEpisodes)
	for index := range directories {
		name := fmt.Sprintf("episode-%04d", index+1)
		entry := entries[index]
		if entry.Name() != name || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil, controlexperiment.AgenticMethodSpec{}, errors.New("AGENTIC_HOLDOUT_CLI_INVESTIGATION_SEQUENCE_INVALID")
		}
		directory := filepath.Join(path, name)
		current, err := readAgenticHoldoutMethodSpec(directory)
		if err != nil || !reflect.DeepEqual(current, spec) {
			return nil, controlexperiment.AgenticMethodSpec{}, errors.New("AGENTIC_HOLDOUT_CLI_INVESTIGATION_METHOD_DRIFT")
		}
		directories[index] = directory
	}
	return directories, spec, nil
}

func readAgenticHoldoutMethodSpec(directory string) (controlexperiment.AgenticMethodSpec, error) {
	path := filepath.Join(directory, "method-spec.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 128<<10 {
		return controlexperiment.AgenticMethodSpec{}, errors.New("AGENTIC_HOLDOUT_CLI_METHOD_SPEC_INVALID")
	}
	var spec controlexperiment.AgenticMethodSpec
	if err := readStrictJSON(path, &spec); err != nil || spec.Validate() != nil {
		return controlexperiment.AgenticMethodSpec{}, errors.New("AGENTIC_HOLDOUT_CLI_METHOD_SPEC_INVALID")
	}
	return spec, nil
}

func loadAgenticEpisodeEvidence(
	directory string,
	trialID string,
	contract defectbench.FormalBenchmarkContract,
	methodSpec controlexperiment.AgenticMethodSpec,
) (defectbench.AgenticTrialEvidence, error) {
	storedSpec, err := readAgenticHoldoutMethodSpec(directory)
	if err != nil || !reflect.DeepEqual(storedSpec, methodSpec) {
		return defectbench.AgenticTrialEvidence{}, fmt.Errorf("AGENTIC_HOLDOUT_CLI_METHOD_SPEC_INVALID: %s", trialID)
	}
	info, statErr := os.Lstat(directory)
	if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return defectbench.AgenticTrialEvidence{}, fmt.Errorf("AGENTIC_HOLDOUT_CLI_EPISODE_DIRECTORY_INVALID: %s", trialID)
	}
	summary, err := readAgenticEpisodeSummary(filepath.Join(directory, "summary.json"))
	if err != nil {
		return defectbench.AgenticTrialEvidence{}, fmt.Errorf("load Agentic summary %s: %w", trialID, err)
	}
	if !validAgenticEpisodeSummaryProjection(summary, contract, methodSpec) {
		return defectbench.AgenticTrialEvidence{}, fmt.Errorf("AGENTIC_HOLDOUT_CLI_SUMMARY_INVALID: %s", trialID)
	}
	current := defectbench.AgenticTrialEvidence{
		TargetID: summary.TargetID, MethodSpecDigest: summary.MethodSpecDigest,
		EpisodeStatus: summary.Status, EvidenceStatus: summary.Assessment.Status,
		Budget: methodSpec.EpisodeBudget, RiskAttempts: summary.RiskAttempts,
		ScenarioAttempts: summary.ScenarioAttempts, ScenarioDecisionsUsed: summary.ScenarioDecisionsUsed,
		ModelWork: summary.Work.Model, ScenarioFrontier: summary.Work.ScenarioFrontier,
		ScenarioSearch: summary.Work.ScenarioSearch,
	}
	bundlePath := filepath.Join(directory, "bundle.json")
	if bundleInfo, bundleErr := os.Lstat(bundlePath); bundleErr == nil {
		if !bundleInfo.Mode().IsRegular() || bundleInfo.Mode()&os.ModeSymlink != 0 || summary.PlanID == "" {
			return defectbench.AgenticTrialEvidence{}, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BUNDLE_INVALID: %s", trialID)
		}
		var bundle controlexperiment.ExecutionBundle
		if err := readStrictJSON(bundlePath, &bundle); err != nil ||
			!agenticSummaryBundleMatches(summary.MethodSpecDigest, summary.PlanID, summary.RiskResultID,
				summary.TraceDigest, summary.Work.QualifiedExecution, bundle) {
			return defectbench.AgenticTrialEvidence{}, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BUNDLE_INVALID: %s", trialID)
		}
		current.Bundle = &bundle
	} else if !errors.Is(bundleErr, os.ErrNotExist) {
		return defectbench.AgenticTrialEvidence{}, bundleErr
	} else if summary.PlanID != "" {
		return defectbench.AgenticTrialEvidence{}, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BUNDLE_INVALID: %s", trialID)
	}
	branchPath := filepath.Join(directory, "branch-evidence.json")
	if branchInfo, branchErr := os.Lstat(branchPath); branchErr == nil {
		if !branchInfo.Mode().IsRegular() || branchInfo.Mode()&os.ModeSymlink != 0 {
			return defectbench.AgenticTrialEvidence{}, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BRANCH_EVIDENCE_INVALID: %s", trialID)
		}
		var branches []agenticBranchEvidenceProjection
		if err := readStrictJSON(branchPath, &branches); err != nil || len(branches) == 0 ||
			len(branches) > controlexperiment.ScenarioAgentMaxCalls || len(branches) != len(summary.BranchEvidence) {
			return defectbench.AgenticTrialEvidence{}, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BRANCH_EVIDENCE_INVALID: %s", trialID)
		}
		seenBranches := make(map[string]bool, len(branches))
		for index, branch := range branches {
			declared := summary.BranchEvidence[index]
			if strings.TrimSpace(branch.BranchID) == "" || strings.TrimSpace(branch.Intent) == "" ||
				seenBranches[branch.BranchID] || len(branch.Testing) == 0 || branch.BranchID != declared.BranchID ||
				branch.Intent != declared.Intent || branch.ReferenceBranchID != declared.ReferenceBranchID {
				return defectbench.AgenticTrialEvidence{}, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BRANCH_EVIDENCE_INVALID: %s", trialID)
			}
			var testing agenticTestingProjection
			if err := json.Unmarshal(branch.Testing, &testing); err != nil ||
				!agenticSummaryBundleMatches(summary.MethodSpecDigest, declared.PlanID, declared.RiskResultID,
					declared.TraceDigest, declared.Work, testing.Bundle) ||
				testing.PlanID != declared.PlanID || testing.Risk.ID != declared.RiskResultID {
				return defectbench.AgenticTrialEvidence{}, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BRANCH_EVIDENCE_INVALID: %s", trialID)
			}
			current.CandidateBundles = append(current.CandidateBundles, testing.Bundle)
			seenBranches[branch.BranchID] = true
		}
	} else if !errors.Is(branchErr, os.ErrNotExist) {
		return defectbench.AgenticTrialEvidence{}, branchErr
	} else if len(summary.BranchEvidence) != 0 {
		return defectbench.AgenticTrialEvidence{}, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BRANCH_EVIDENCE_INVALID: %s", trialID)
	}
	return current, nil
}

func mergeAgenticEpisodeEvidence(
	total *defectbench.AgenticTrialEvidence,
	episode defectbench.AgenticTrialEvidence,
) bool {
	add := func(target *int, value int) bool {
		maxInt := int(^uint(0) >> 1)
		if *target < 0 || value < 0 || value > maxInt-*target {
			return false
		}
		*target += value
		return true
	}
	if !add(&total.RiskAttempts, episode.RiskAttempts) ||
		!add(&total.ScenarioAttempts, episode.ScenarioAttempts) ||
		!add(&total.ScenarioDecisionsUsed, episode.ScenarioDecisionsUsed) ||
		!mergeAgenticModelWork(&total.ModelWork, episode.ModelWork) ||
		!mergeAgenticPhaseWork(&total.ScenarioFrontier, episode.ScenarioFrontier) ||
		!mergeAgenticSearchWork(&total.ScenarioSearch, episode.ScenarioSearch) {
		return false
	}
	bundles := make([]controlexperiment.ExecutionBundle, 0, len(episode.CandidateBundles)+1)
	if episode.Bundle != nil {
		bundles = append(bundles, *episode.Bundle)
	}
	bundles = append(bundles, episode.CandidateBundles...)
	if total.Bundle == nil && len(bundles) != 0 {
		bundle := bundles[0]
		total.Bundle = &bundle
		bundles = bundles[1:]
	}
	total.CandidateBundles = append(total.CandidateBundles, bundles...)
	return true
}

func mergeAgenticModelWork(total *controlexperiment.ModelWork, current controlexperiment.ModelWork) bool {
	values := [][2]*int{{&total.Calls, &current.Calls}, {&total.InputTokens, &current.InputTokens},
		{&total.OutputTokens, &current.OutputTokens}, {&total.TotalTokens, &current.TotalTokens}}
	for _, pair := range values {
		maxInt := int(^uint(0) >> 1)
		if *pair[0] < 0 || *pair[1] < 0 || *pair[1] > maxInt-*pair[0] {
			return false
		}
		*pair[0] += *pair[1]
	}
	return true
}

func mergeAgenticPhaseWork(total *controlexperiment.PhaseWork, current controlexperiment.PhaseWork) bool {
	values := [][2]*int{{&total.SetupAttempts, &current.SetupAttempts},
		{&total.RuntimeInitializations, &current.RuntimeInitializations},
		{&total.PrepareActions, &current.PrepareActions},
		{&total.SchedulerDecisions, &current.SchedulerDecisions}, {&total.WorkUnits, &current.WorkUnits}}
	for _, pair := range values {
		maxInt := int(^uint(0) >> 1)
		if *pair[0] < 0 || *pair[1] < 0 || *pair[1] > maxInt-*pair[0] {
			return false
		}
		*pair[0] += *pair[1]
	}
	return true
}

func mergeAgenticSearchWork(
	total *controlexperiment.StatelessDFSWork,
	current controlexperiment.StatelessDFSWork,
) bool {
	return mergeAgenticPhaseWork(&total.FrontierReconstruction, current.FrontierReconstruction) &&
		mergeAgenticPhaseWork(&total.ChildMaterialization, current.ChildMaterialization) &&
		mergeAgenticPhaseWork(&total.ChildVerification, current.ChildVerification) &&
		func() bool {
			maxInt := int(^uint(0) >> 1)
			if current.TotalWorkUnits < 0 || current.TotalWorkUnits > maxInt-total.TotalWorkUnits {
				return false
			}
			total.TotalWorkUnits += current.TotalWorkUnits
			return true
		}()
}

func validAgenticEpisodeSummaryProjection(
	summary agenticEpisodeSummaryProjection,
	contract defectbench.FormalBenchmarkContract,
	methodSpec controlexperiment.AgenticMethodSpec,
) bool {
	limits := methodSpec.EpisodeLimits
	hasPrimary := summary.PlanID != "" && summary.RiskResultID != "" && summary.TraceDigest != ""
	if summary.TargetID == "" || summary.MethodSpecDigest != contract.MethodSpecDigest ||
		summary.Budget.Logical == nil || summary.Budget.Logical.Validate() != nil ||
		contract.AgenticBudget == nil || *summary.Budget.Logical != methodSpec.EpisodeBudget ||
		summary.Budget.MaxRiskCalls != limits.MaxRiskCalls ||
		summary.Budget.MaxScenarioCalls != limits.MaxScenarioCalls ||
		summary.Budget.MaxTotalCalls != limits.MaxTotalCalls ||
		summary.Budget.MaxObservedTokens != limits.MaxObservedTokens ||
		summary.Budget.MaxScenarioPlanSteps != limits.MaxScenarioPlanSteps ||
		summary.Budget.MaxRuntimeDecisions != limits.MaxRuntimeDecisions ||
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

func agenticHoldoutTargetID(
	evidence map[string]defectbench.AgenticTrialEvidence,
) (string, error) {
	targetID := ""
	for _, current := range evidence {
		if strings.TrimSpace(current.TargetID) == "" ||
			targetID != "" && current.TargetID != targetID {
			return "", errors.New("AGENTIC_HOLDOUT_CLI_TARGET_SET_INVALID")
		}
		targetID = current.TargetID
	}
	if targetID == "" {
		return "", errors.New("AGENTIC_HOLDOUT_CLI_TARGET_SET_INVALID")
	}
	return targetID, nil
}
