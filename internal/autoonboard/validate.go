package autoonboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolcontract"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/scenario"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type Factory func(coverage.Profile) (adapter.Adapter, driver.Manifest, error)

type Finding struct {
	Code       string `json:"code"`
	Subject    string `json:"subject,omitempty"`
	Message    string `json:"message"`
	Actionable bool   `json:"actionable"`
}

type WitnessResult struct {
	ID                 string      `json:"id"`
	Scenario           string      `json:"scenario"`
	Passed             bool        `json:"passed"`
	ReplayStable       bool        `json:"replay_stable"`
	Conformant         bool        `json:"conformant"`
	ObservedLabels     []string    `json:"observed_labels,omitempty"`
	CheckedMonitors    []string    `json:"checked_monitors,omitempty"`
	ViolationCount     int         `json:"violation_count"`
	CoveredObligations []string    `json:"covered_obligations,omitempty"`
	CapabilityClaims   []string    `json:"capability_claims,omitempty"`
	FailureEvent       *core.Event `json:"failure_event,omitempty"`
	FailureOutcome     string      `json:"failure_outcome,omitempty"`
}

type Report struct {
	Version                 int              `json:"version"`
	Status                  string           `json:"status"`
	ContractID              string           `json:"contract_id"`
	ContractDigest          string           `json:"contract_digest"`
	BindingID               string           `json:"binding_id"`
	Driver                  string           `json:"driver"`
	FullySupported          bool             `json:"fully_supported"`
	CapabilitySupport       float64          `json:"capability_support"`
	ObligationSupport       float64          `json:"obligation_support"`
	ValidatedCapabilities   []string         `json:"validated_capabilities,omitempty"`
	UnsupportedCapabilities []string         `json:"unsupported_capabilities,omitempty"`
	ValidatedObligations    []string         `json:"validated_obligations,omitempty"`
	UnsupportedObligations  []string         `json:"unsupported_obligations,omitempty"`
	Witnesses               []WitnessResult  `json:"witnesses,omitempty"`
	Findings                []Finding        `json:"findings,omitempty"`
	Profile                 coverage.Profile `json:"profile"`
}

type execution struct {
	trace      []core.TraceRecord
	conformant bool
}

func Validate(ctx context.Context, repoRoot string, contract *protocolcontract.Contract, binding *Binding, factory Factory) (Report, error) {
	report := Report{Version: Version, Status: "invalid"}
	if contract == nil || binding == nil || factory == nil {
		return report, errors.New("contract, binding, and factory are required")
	}
	if err := contract.Validate(); err != nil {
		return report, fmt.Errorf("validate contract: %w", err)
	}
	digest, err := protocolcontract.Digest(contract)
	if err != nil {
		return report, err
	}
	report.ContractID, report.ContractDigest = contract.ID, digest
	report.BindingID, report.Driver = binding.ID, binding.Driver

	add := func(code, subject, message string, actionable bool) {
		report.Findings = append(report.Findings, Finding{Code: code, Subject: subject, Message: message, Actionable: actionable})
	}
	if binding.Version != Version {
		add("binding.version", binding.ID, fmt.Sprintf("unsupported binding version %d", binding.Version), true)
	}
	if binding.ID == "" || binding.Driver == "" {
		add("binding.identity", binding.ID, "binding id and driver are required", true)
	}
	if binding.ContractID != contract.ID || binding.ContractDigest != digest || binding.Protocol != contract.Protocol {
		add("binding.contract-mismatch", binding.ID, "binding does not identify the exact contract digest and protocol", true)
	}

	declaredCapabilities := make(map[string]bool, len(contract.Capabilities))
	for _, capability := range contract.Capabilities {
		declaredCapabilities[capability.ID] = true
	}
	capabilityBindings := make(map[string]string, len(binding.Capabilities))
	for _, item := range binding.Capabilities {
		if !declaredCapabilities[item.ContractID] || item.RuntimeID == "" || capabilityBindings[item.ContractID] != "" {
			add("binding.capability", item.ContractID, "capability mapping is unknown, empty, or duplicated", true)
			continue
		}
		capabilityBindings[item.ContractID] = item.RuntimeID
		if item.ContractID != item.RuntimeID {
			add("binding.capability-id", item.ContractID, "v1 runtime capability IDs use the contract vocabulary and must match exactly", true)
		}
	}
	for id := range declaredCapabilities {
		if capabilityBindings[id] == "" {
			add("binding.capability-missing", id, "every contract capability needs one runtime mapping", true)
		}
	}

	declaredOperations := make(map[string]protocolcontract.Operation, len(contract.Operations))
	for _, operation := range contract.Operations {
		declaredOperations[operation.ID] = operation
	}
	operationBindings := make(map[string]core.EventKind, len(binding.Operations))
	for _, item := range binding.Operations {
		operation, exists := declaredOperations[item.OperationID]
		if !exists || item.RuntimeKind == "" || operationBindings[item.OperationID] != "" {
			add("binding.operation", item.OperationID, "operation mapping is unknown, empty, or duplicated", true)
			continue
		}
		operationBindings[item.OperationID] = item.RuntimeKind
		if operation.EventKind != item.RuntimeKind {
			add("binding.operation-kind", item.OperationID, fmt.Sprintf("contract requires %q, binding proposed %q", operation.EventKind, item.RuntimeKind), true)
		}
	}
	for id := range declaredOperations {
		if operationBindings[id] == "" {
			add("binding.operation-missing", id, "every contract operation needs one runtime mapping", true)
		}
	}

	allCapabilities := make(map[string]bool, len(contract.Capabilities))
	for _, capability := range contract.Capabilities {
		allCapabilities[capability.ID] = true
	}
	preliminary, err := protocolcontract.Compile(contract, allCapabilities)
	if err != nil {
		return report, err
	}
	runtimeAdapter, manifest, err := factory(preliminary)
	if err != nil {
		add("runtime.factory", binding.Driver, err.Error(), true)
		return finishReport(report, contract), nil
	}
	if manifest.Driver != binding.Driver {
		add("runtime.driver", binding.Driver, fmt.Sprintf("runtime reported driver %q", manifest.Driver), true)
	}
	if runtimeAdapter.Protocol() != contract.Protocol {
		add("runtime.protocol", runtimeAdapter.Protocol(), fmt.Sprintf("contract requires protocol %q", contract.Protocol), true)
	}
	if !sameStrings(runtimeAdapter.Nodes(), contract.Nodes) {
		add("runtime.nodes", binding.Driver, "runtime nodes differ from contract nodes", true)
	}
	if err := runtimeAdapter.CheckConformance(); err != nil {
		add("runtime.conformance", binding.Driver, err.Error(), true)
	}

	runtimeCapabilities := make(map[string]driver.Capability, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		if capability.ID == "" || runtimeCapabilities[capability.ID].ID != "" {
			add("runtime.capability-manifest", capability.ID, "runtime capability IDs must be non-empty and unique", true)
			continue
		}
		runtimeCapabilities[capability.ID] = capability
	}
	runtimeSupported := make(map[string]bool, len(contract.Capabilities))
	unsupportedCapabilities := make(map[string]bool, len(contract.Capabilities))
	for _, capability := range contract.Capabilities {
		runtimeID := capabilityBindings[capability.ID]
		runtimeCapability, exists := runtimeCapabilities[runtimeID]
		switch {
		case runtimeID == "":
			unsupportedCapabilities[capability.ID] = true
		case !exists:
			add("runtime.capability-unknown", capability.ID, fmt.Sprintf("runtime manifest has no capability %q", runtimeID), true)
			unsupportedCapabilities[capability.ID] = true
		case runtimeCapability.Supported:
			runtimeSupported[capability.ID] = true
		default:
			unsupportedCapabilities[capability.ID] = true
			add("runtime.capability-unsupported", capability.ID, runtimeCapability.Detail, false)
		}
	}

	preliminary, err = protocolcontract.Compile(contract, runtimeSupported)
	if err != nil {
		return report, err
	}
	obligations := make(map[string]protocolcontract.Obligation)
	for _, obligation := range contract.Obligations() {
		obligations[obligation.ID] = obligation
	}
	witnessed := make(map[string]bool)
	capabilityLabels := make(map[string]map[string]bool)
	seenWitness := make(map[string]bool)
	for _, witness := range binding.Witnesses {
		if witness.ID == "" || witness.Scenario == "" || len(witness.Covers)+len(witness.ValidatesCapabilities) == 0 || seenWitness[witness.ID] {
			add("witness.definition", witness.ID, "witness needs a unique id, scenario, and at least one obligation or capability claim", true)
			continue
		}
		seenWitness[witness.ID] = true
		covered := make([]protocolcontract.Obligation, 0, len(witness.Covers))
		seenCover := make(map[string]bool)
		for _, id := range witness.Covers {
			obligation, exists := obligations[id]
			if !exists || seenCover[id] {
				add("witness.obligation", witness.ID, fmt.Sprintf("unknown or duplicate obligation %q", id), true)
				continue
			}
			seenCover[id] = true
			if capabilitiesSatisfied(obligation.Requires, runtimeSupported) {
				covered = append(covered, obligation)
			}
		}
		capabilityClaims := make([]string, 0, len(witness.ValidatesCapabilities))
		seenCapability := make(map[string]bool)
		for _, id := range witness.ValidatesCapabilities {
			if !declaredCapabilities[id] || seenCapability[id] {
				add("witness.capability", witness.ID, fmt.Sprintf("unknown or duplicate capability %q", id), true)
				continue
			}
			seenCapability[id] = true
			if runtimeSupported[id] {
				capabilityClaims = append(capabilityClaims, id)
			}
		}
		if len(covered) == 0 && len(capabilityClaims) == 0 {
			continue
		}
		result, runErr := validateWitness(ctx, repoRoot, preliminary, witness, covered, factory)
		result.CapabilityClaims = append(result.CapabilityClaims, capabilityClaims...)
		report.Witnesses = append(report.Witnesses, result)
		if runErr != nil {
			add("witness.failed", witness.ID, runErr.Error(), true)
			continue
		}
		for _, obligation := range covered {
			witnessed[obligation.ID] = true
		}
		for _, capability := range capabilityClaims {
			labels := capabilityLabels[capability]
			if labels == nil {
				labels = make(map[string]bool)
				capabilityLabels[capability] = labels
			}
			for _, label := range result.ObservedLabels {
				labels[label] = true
			}
		}
	}

	validatedCapabilities := make(map[string]bool, len(contract.Capabilities))
	for _, capability := range contract.Capabilities {
		if !runtimeSupported[capability.ID] {
			continue
		}
		var missing []string
		for _, label := range capability.WitnessLabels {
			if !capabilityLabels[capability.ID][label] {
				missing = append(missing, label)
			}
		}
		if len(missing) > 0 {
			unsupportedCapabilities[capability.ID] = true
			add("capability.witness-missing", capability.ID, fmt.Sprintf("missing contract evidence labels %v", missing), true)
			continue
		}
		validatedCapabilities[capability.ID] = true
		report.ValidatedCapabilities = append(report.ValidatedCapabilities, capability.ID)
	}
	for _, capability := range contract.Capabilities {
		if unsupportedCapabilities[capability.ID] {
			report.UnsupportedCapabilities = append(report.UnsupportedCapabilities, capability.ID)
		}
	}

	profile, err := protocolcontract.Compile(contract, validatedCapabilities)
	if err != nil {
		return report, err
	}
	report.Profile = profile

	for index := range report.Profile.Coverage.Obligations {
		item := &report.Profile.Coverage.Obligations[index]
		obligation := obligations[item.ID]
		if item.Status == coverage.StatusSupported && !witnessed[item.ID] {
			item.Status = coverage.StatusUnsupported
			add("witness.missing", item.ID, "supported obligation has no mechanically valid witness", true)
		}
		if item.Status == coverage.StatusSupported && capabilitiesSatisfied(obligation.Requires, validatedCapabilities) {
			report.ValidatedObligations = append(report.ValidatedObligations, item.ID)
		} else {
			report.UnsupportedObligations = append(report.UnsupportedObligations, item.ID)
		}
	}
	return finishReport(report, contract), nil
}

func validateWitness(ctx context.Context, root string, profile coverage.Profile, witness Witness, obligations []protocolcontract.Obligation, factory Factory) (WitnessResult, error) {
	result := WitnessResult{ID: witness.ID, Scenario: witness.Scenario}
	path, err := withinRoot(root, witness.Scenario)
	if err != nil {
		return result, err
	}
	var spec scenario.Spec
	if err := readStrictJSON(path, &spec); err != nil {
		return result, err
	}
	if err := spec.Validate(); err != nil {
		return result, err
	}
	first, err := execute(ctx, profile, spec, factory)
	if err != nil {
		populatePartialWitness(&result, first.trace)
		return result, err
	}
	second, err := execute(ctx, profile, spec, factory)
	if err != nil {
		return result, fmt.Errorf("deterministic replay: %w", err)
	}
	firstHash, err := semantic.ExecutionFingerprint(first.trace)
	if err != nil {
		return result, err
	}
	secondHash, err := semantic.ExecutionFingerprint(second.trace)
	if err != nil {
		return result, err
	}
	result.ReplayStable = firstHash == secondHash
	result.Conformant = first.conformant && second.conformant
	checked := oracle.Check(first.trace, oracle.TraceIntegrity{}, oracle.Agreement{})
	result.CheckedMonitors = append([]string(nil), checked.Checked...)
	result.ViolationCount = len(checked.Violations)
	labels := observationLabels(first.trace)
	for label := range labels {
		result.ObservedLabels = append(result.ObservedLabels, label)
	}
	sort.Strings(result.ObservedLabels)
	checkedSet := make(map[string]bool, len(checked.Checked))
	for _, monitor := range checked.Checked {
		checkedSet[monitor] = true
	}
	if !result.ReplayStable {
		return result, errors.New("execution fingerprints differ across replay")
	}
	if !result.Conformant {
		return result, errors.New("driver conformance check failed")
	}
	if len(checked.Violations) > 0 {
		return result, fmt.Errorf("oracle reported %d violation(s)", len(checked.Violations))
	}
	for _, obligation := range obligations {
		if !labels[obligation.WitnessLabel] {
			return result, fmt.Errorf("obligation %s did not produce contract label %q", obligation.ID, obligation.WitnessLabel)
		}
		if !checkedSet[obligation.Monitor] {
			return result, fmt.Errorf("obligation %s requires unavailable monitor %q", obligation.ID, obligation.Monitor)
		}
		result.CoveredObligations = append(result.CoveredObligations, obligation.ID)
	}
	result.Passed = true
	return result, nil
}

func execute(ctx context.Context, profile coverage.Profile, spec scenario.Spec, factory Factory) (execution, error) {
	runtimeAdapter, _, err := factory(profile)
	if err != nil {
		return execution{}, err
	}
	conformant := runtimeAdapter.CheckConformance() == nil
	e := engine.New(runtimeAdapter)
	if err := scenario.Run(ctx, e, spec); err != nil {
		return execution{trace: e.Trace(), conformant: runtimeAdapter.CheckConformance() == nil}, err
	}
	if runtimeAdapter.CheckConformance() != nil {
		conformant = false
	}
	return execution{trace: e.Trace(), conformant: conformant}, nil
}

func populatePartialWitness(result *WitnessResult, trace []core.TraceRecord) {
	if result == nil || len(trace) == 0 {
		return
	}
	labels := observationLabels(trace)
	for label := range labels {
		result.ObservedLabels = append(result.ObservedLabels, label)
	}
	sort.Strings(result.ObservedLabels)
	last := trace[len(trace)-1]
	event := last.Event
	result.FailureEvent = &event
	result.FailureOutcome = last.Outcome
}

func finishReport(report Report, contract *protocolcontract.Contract) Report {
	sort.Strings(report.ValidatedCapabilities)
	sort.Strings(report.UnsupportedCapabilities)
	sort.Strings(report.ValidatedObligations)
	sort.Strings(report.UnsupportedObligations)
	if len(contract.Capabilities) > 0 {
		report.CapabilitySupport = float64(len(report.ValidatedCapabilities)) / float64(len(contract.Capabilities))
	}
	obligationCount := len(contract.Obligations())
	if obligationCount > 0 {
		report.ObligationSupport = float64(len(report.ValidatedObligations)) / float64(obligationCount)
	}
	actionable := false
	for _, finding := range report.Findings {
		actionable = actionable || finding.Actionable
	}
	if !actionable {
		report.Status = "validated"
	}
	report.FullySupported = report.Status == "validated" && len(report.UnsupportedCapabilities) == 0 && len(report.UnsupportedObligations) == 0
	return report
}

func withinRoot(root, relative string) (string, error) {
	if root == "" || filepath.IsAbs(relative) {
		return "", errors.New("repository root is required and witness scenario must be relative")
	}
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		return "", err
	}
	path := filepath.Join(resolvedRoot, filepath.Clean(relative))
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return "", fmt.Errorf("witness scenario escapes repository: %s", relative)
	}
	return resolvedPath, nil
}

func readStrictJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func capabilitiesSatisfied(required []string, supported map[string]bool) bool {
	for _, id := range required {
		if !supported[id] {
			return false
		}
	}
	return true
}

func observationLabels(trace []core.TraceRecord) map[string]bool {
	labels := make(map[string]bool)
	for _, record := range trace {
		for _, observation := range record.Observations {
			labels[observation.Label] = true
		}
	}
	return labels
}

func sameStrings(left, right []string) bool {
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
