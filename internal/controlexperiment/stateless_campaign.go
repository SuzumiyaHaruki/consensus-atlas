package controlexperiment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	StatelessCampaignSpecVersion     = "consensus-atlas/stateless-campaign-spec/v1"
	StatelessCampaignArtifactVersion = "consensus-atlas/stateless-campaign-attempt/v1"
	StatelessCampaignSourceCharge    = "charge-complete-source-to-each-attempt"
)

type StatelessCampaignAttemptBudget struct {
	MaxPrimarySchedulerDecisions int `json:"max_primary_scheduler_decisions"`
	MaxPrimaryWorkUnits          int `json:"max_primary_work_units"`
	MaxReplayWorkUnits           int `json:"max_replay_work_units"`
	MaxModelCalls                int `json:"max_model_calls"`
	MaxModelTokens               int `json:"max_model_tokens"`
}

// StatelessCampaignSpec freezes one search method per Campaign attempt.
// Source generation is charged to every attempt so a reused root corpus is
// never presented as free method work.
type StatelessCampaignSpec struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`

	TargetID             string                     `json:"target_id"`
	TargetIdentityDigest string                     `json:"target_identity_digest"`
	Methods              []StatelessTraversalMethod `json:"attempt_methods"`
	CorpusDigest         string                     `json:"corpus_digest"`
	SourceBundleDigest   string                     `json:"source_bundle_digest"`
	SourceManifestDigest string                     `json:"source_manifest_digest"`
	SourceWork           WorkLedger                 `json:"source_work"`

	RootCount                 int                            `json:"root_count"`
	MaxDepth                  int                            `json:"max_depth"`
	MaxWorkItemsPerRoot       int                            `json:"max_work_items_per_root"`
	MaxSearchWorkUnitsPerRoot int                            `json:"max_search_work_units_per_root"`
	Budget                    StatelessCampaignAttemptBudget `json:"attempt_budget"`

	SourceChargePolicy        string `json:"source_charge_policy"`
	StrictReplayRequired      bool   `json:"strict_replay_required"`
	ReadOnlyDiscoveryRequired bool   `json:"read_only_discovery_required"`
	Digest                    string `json:"digest"`
}

func NewStatelessCampaignSpec(spec StatelessCampaignSpec) (StatelessCampaignSpec, error) {
	spec.SchemaVersion = StatelessCampaignSpecVersion
	spec.SourceChargePolicy = StatelessCampaignSourceCharge
	spec.StrictReplayRequired = true
	spec.ReadOnlyDiscoveryRequired = true
	sealed, err := spec.seal()
	if err != nil {
		return StatelessCampaignSpec{}, err
	}
	if err := sealed.Validate(); err != nil {
		return StatelessCampaignSpec{}, err
	}
	return sealed, nil
}

func (spec StatelessCampaignSpec) Validate() error {
	if spec.SchemaVersion != StatelessCampaignSpecVersion || !validMethodToken(spec.ID) ||
		!validMethodToken(spec.TargetID) || !validSHA256(spec.TargetIdentityDigest) ||
		!validStatelessCampaignMethods(spec.Methods) || !validSHA256(spec.CorpusDigest) ||
		!validSHA256(spec.SourceBundleDigest) || !validSHA256(spec.SourceManifestDigest) ||
		validateMethodWork(spec.SourceWork) != nil || spec.SourceWork.Model != (ModelWork{}) ||
		spec.RootCount <= 0 || spec.MaxDepth <= 0 ||
		spec.MaxWorkItemsPerRoot <= 0 || spec.MaxSearchWorkUnitsPerRoot <= 0 ||
		spec.SourceChargePolicy != StatelessCampaignSourceCharge || !spec.StrictReplayRequired ||
		!spec.ReadOnlyDiscoveryRequired || spec.Budget.validate(spec.Methods[0].Strategy) != nil {
		return errors.New("EXPERIMENT_STATELESS_CAMPAIGN_SPEC_INVALID")
	}
	want, err := spec.seal()
	if err != nil || !validSHA256(spec.Digest) || want.Digest != spec.Digest {
		return errors.New("EXPERIMENT_STATELESS_CAMPAIGN_SPEC_DIGEST_MISMATCH")
	}
	return nil
}

func (budget StatelessCampaignAttemptBudget) validate(strategy string) error {
	if budget.MaxPrimarySchedulerDecisions <= 0 || budget.MaxPrimaryWorkUnits <= 0 ||
		budget.MaxPrimarySchedulerDecisions > budget.MaxPrimaryWorkUnits ||
		budget.MaxReplayWorkUnits <= 0 || budget.MaxModelCalls < 0 || budget.MaxModelTokens < 0 ||
		budget.MaxModelCalls != 0 || budget.MaxModelTokens != 0 {
		return errors.New("EXPERIMENT_STATELESS_CAMPAIGN_BUDGET_INVALID")
	}
	return nil
}

func validStatelessCampaignMethods(methods []StatelessTraversalMethod) bool {
	if len(methods) == 0 {
		return false
	}
	strategy := methods[0].Strategy
	for _, method := range methods {
		if method.Validate() != nil || method.Strategy != strategy {
			return false
		}
	}
	return true
}

func (spec StatelessCampaignSpec) ValidateInputs(
	methods []StatelessTraversalMethod,
	corpus StatelessRootCorpus,
	source ExecutionBundle,
) error {
	if spec.Validate() != nil || !reflect.DeepEqual(spec.Methods, methods) || corpus.Validate(source) != nil ||
		spec.CorpusDigest != corpus.Digest ||
		spec.SourceBundleDigest != source.Digest ||
		spec.SourceManifestDigest != source.Identity.ManifestDigest ||
		spec.SourceWork != source.Work || spec.RootCount != len(corpus.Roots) {
		return errors.New("EXPERIMENT_STATELESS_CAMPAIGN_SPEC_INPUT_MISMATCH")
	}
	return nil
}

func NewStatelessCampaignConfig(
	id string,
	spec StatelessCampaignSpec,
	wallClockCeilingMillis int64,
) (CampaignConfig, error) {
	if spec.Validate() != nil {
		return CampaignConfig{}, errors.New("EXPERIMENT_STATELESS_CAMPAIGN_CONFIG_INPUT_INVALID")
	}
	attempts := len(spec.Methods)
	budget, ok := multiplyStatelessCampaignBudget(spec.Budget, attempts)
	if !ok {
		return CampaignConfig{}, errors.New("EXPERIMENT_STATELESS_CAMPAIGN_CONFIG_BUDGET_OVERFLOW")
	}
	config, err := NewCampaignConfig(
		id, spec.TargetID, spec.TargetIdentityDigest, spec.Digest, budget, wallClockCeilingMillis,
	)
	if err != nil {
		return CampaignConfig{}, err
	}
	// Model work is owned by the search attempt artifact and ordinary Campaign
	// allowance. The legacy Campaign Planner mode is deliberately not used.
	return config, nil
}

func multiplyStatelessCampaignBudget(
	budget StatelessCampaignAttemptBudget,
	attempts int,
) (CampaignLogicalBudget, bool) {
	values := []int{
		budget.MaxPrimarySchedulerDecisions, budget.MaxPrimaryWorkUnits,
		budget.MaxReplayWorkUnits, budget.MaxModelCalls, budget.MaxModelTokens,
	}
	for index, value := range values {
		if value != 0 && (attempts <= 0 || value > int(^uint(0)>>1)/attempts) {
			return CampaignLogicalBudget{}, false
		}
		values[index] *= attempts
	}
	return CampaignLogicalBudget{
		MaxAttempts: attempts, MaxPrimarySchedulerDecisions: values[0],
		MaxPrimaryWorkUnits: values[1], MaxReplayWorkUnits: values[2],
		MaxModelCalls: values[3], MaxModelTokens: values[4],
	}, true
}

// StatelessCampaignExecution is the only target-owned callback result. A
// completed execution carries trusted read-only discovery. A failed execution
// carries typed partial work and no invented discovery.
type StatelessCampaignExecution struct {
	Discovery              *StatelessCorpusDiscovery
	SearchWork             StatelessDFSWork
	QualifiedExecutionWork WorkLedger
	ModelWork              ModelWork
	AgentCalls             []StatelessAgentCallAudit
	Failure                *MethodFailure
}

type StatelessCampaignAttemptArtifact struct {
	SchemaVersion string                    `json:"schema_version"`
	ID            string                    `json:"id"`
	RequestDigest string                    `json:"request_digest"`
	Spec          StatelessCampaignSpec     `json:"spec"`
	Method        StatelessTraversalMethod  `json:"method"`
	Outcome       string                    `json:"outcome"`
	Failure       *MethodFailure            `json:"failure,omitempty"`
	Discovery     *StatelessCorpusDiscovery `json:"discovery,omitempty"`

	SearchWork             StatelessDFSWork          `json:"search_work"`
	QualifiedExecutionWork WorkLedger                `json:"qualified_execution_work"`
	ModelWork              ModelWork                 `json:"model_work"`
	AgentCalls             []StatelessAgentCallAudit `json:"agent_calls,omitempty"`
	Work                   WorkLedger                `json:"work"`
	Digest                 string                    `json:"digest"`
}

func NewStatelessCampaignAttemptArtifact(
	request CampaignAttemptRequest,
	spec StatelessCampaignSpec,
	execution StatelessCampaignExecution,
) (StatelessCampaignAttemptArtifact, error) {
	if request.Validate() != nil || spec.Validate() != nil ||
		request.TargetID != spec.TargetID || request.TargetIdentityDigest != spec.TargetIdentityDigest ||
		request.ExperimentSpecDigest != spec.Digest || request.Ordinal > len(spec.Methods) {
		return StatelessCampaignAttemptArtifact{}, errors.New("EXPERIMENT_STATELESS_CAMPAIGN_ATTEMPT_INPUT_INVALID")
	}
	if execution.QualifiedExecutionWork == (WorkLedger{}) {
		execution.QualifiedExecutionWork = emptyWork()
	}
	artifact := StatelessCampaignAttemptArtifact{
		SchemaVersion: StatelessCampaignArtifactVersion,
		ID:            request.CampaignID + "-search-attempt-" + decimalToken(request.Ordinal),
		RequestDigest: request.Digest, Spec: spec, Method: spec.Methods[request.Ordinal-1],
		SearchWork: execution.SearchWork, QualifiedExecutionWork: execution.QualifiedExecutionWork,
		ModelWork: execution.ModelWork, AgentCalls: cloneStatelessAgentCallAudits(execution.AgentCalls),
		Failure: cloneMethodFailure(execution.Failure),
	}
	if execution.Discovery != nil {
		discovery := *execution.Discovery
		artifact.Discovery = &discovery
		artifact.Outcome = CampaignAttemptCompleted
	} else {
		artifact.Outcome = CampaignAttemptFailed
	}
	artifact.Work = statelessCampaignWork(
		spec.SourceWork, artifact.SearchWork, artifact.QualifiedExecutionWork, artifact.ModelWork,
	)
	sealed, err := artifact.seal()
	if err != nil {
		return StatelessCampaignAttemptArtifact{}, err
	}
	if err := sealed.ValidateInputs(request); err != nil {
		return StatelessCampaignAttemptArtifact{}, err
	}
	return sealed, nil
}

func (artifact StatelessCampaignAttemptArtifact) ValidateInputs(request CampaignAttemptRequest) error {
	if request.Validate() != nil || artifact.SchemaVersion != StatelessCampaignArtifactVersion ||
		!validMethodToken(artifact.ID) || artifact.RequestDigest != request.Digest ||
		artifact.Spec.Validate() != nil || request.TargetID != artifact.Spec.TargetID ||
		request.TargetIdentityDigest != artifact.Spec.TargetIdentityDigest ||
		request.ExperimentSpecDigest != artifact.Spec.Digest || request.Ordinal > len(artifact.Spec.Methods) ||
		artifact.Method.Digest != artifact.Spec.Methods[request.Ordinal-1].Digest ||
		!validStatelessDFSWork(artifact.SearchWork, artifact.Spec.RootCount*artifact.Spec.MaxSearchWorkUnitsPerRoot) ||
		validateMethodWork(artifact.QualifiedExecutionWork) != nil ||
		artifact.QualifiedExecutionWork.Model != (ModelWork{}) ||
		!validStatelessCampaignModelWork(artifact.ModelWork, artifact.Spec) ||
		!validStatelessCampaignAgentCalls(artifact.AgentCalls, artifact.ModelWork, artifact.Method) {
		return errors.New("EXPERIMENT_STATELESS_CAMPAIGN_ATTEMPT_INVALID")
	}
	if artifact.Outcome == CampaignAttemptCompleted {
		if artifact.Failure != nil || artifact.Discovery == nil ||
			artifact.Discovery.ValidateStructure() != nil ||
			len(artifact.Discovery.Roots) != artifact.Spec.RootCount ||
			artifact.Discovery.QualifiedExecutionAttempts >
				artifact.Spec.RootCount*artifact.Spec.MaxWorkItemsPerRoot ||
			artifact.Discovery.MethodDigest != artifact.Method.Digest ||
			artifact.Discovery.CorpusDigest != artifact.Spec.CorpusDigest ||
			artifact.Discovery.SearchWork != artifact.SearchWork ||
			artifact.Discovery.QualifiedExecutionWork != artifact.QualifiedExecutionWork {
			return errors.New("EXPERIMENT_STATELESS_CAMPAIGN_COMPLETED_INVALID")
		}
		for _, root := range artifact.Discovery.Roots {
			if root.SearchWork.TotalWorkUnits > artifact.Spec.MaxSearchWorkUnitsPerRoot {
				return errors.New("EXPERIMENT_STATELESS_CAMPAIGN_ROOT_WORK_INVALID")
			}
		}
	} else if artifact.Outcome == CampaignAttemptFailed {
		if artifact.Discovery != nil || artifact.Failure == nil ||
			artifact.Failure.Phase == "" || artifact.Failure.Code == "" ||
			artifact.Failure.Decision < 0 {
			return errors.New("EXPERIMENT_STATELESS_CAMPAIGN_FAILED_INVALID")
		}
	} else {
		return errors.New("EXPERIMENT_STATELESS_CAMPAIGN_OUTCOME_INVALID")
	}
	wantWork := statelessCampaignWork(
		artifact.Spec.SourceWork, artifact.SearchWork,
		artifact.QualifiedExecutionWork, artifact.ModelWork,
	)
	if artifact.Work != wantWork || !statelessCampaignWorkWithinSpec(artifact.Work, artifact.Spec.Budget) ||
		!campaignWorkWithinAllowance(artifact.Work, request.Allowance) {
		return errors.New("EXPERIMENT_STATELESS_CAMPAIGN_WORK_INVALID")
	}
	want, err := artifact.seal()
	if err != nil || !validSHA256(artifact.Digest) || want.Digest != artifact.Digest {
		return errors.New("EXPERIMENT_STATELESS_CAMPAIGN_ATTEMPT_DIGEST_MISMATCH")
	}
	return nil
}

func DecodeStatelessCampaignAttemptArtifact(
	encoded []byte,
	request CampaignAttemptRequest,
) (StatelessCampaignAttemptArtifact, error) {
	var artifact StatelessCampaignAttemptArtifact
	if len(encoded) == 0 || len(encoded) > campaignMaxArtifactBytes ||
		decodeStrictJSON(encoded, &artifact) != nil || artifact.ValidateInputs(request) != nil {
		return StatelessCampaignAttemptArtifact{}, errors.New("EXPERIMENT_STATELESS_CAMPAIGN_ARTIFACT_INVALID")
	}
	return artifact, nil
}

type StatelessCampaignRunner func(context.Context, CampaignAttemptRequest) (StatelessCampaignExecution, error)

func NewStatelessCampaignAttemptProvider(
	spec StatelessCampaignSpec,
	runner StatelessCampaignRunner,
) (CampaignAttemptProvider, error) {
	if spec.Validate() != nil || runner == nil {
		return nil, errors.New("EXPERIMENT_STATELESS_CAMPAIGN_PROVIDER_INPUT_INVALID")
	}
	return CampaignAttemptProviderFunc(func(
		ctx context.Context,
		request CampaignAttemptRequest,
	) (CampaignAttemptResult, error) {
		execution, runErr := runner(ctx, request)
		if runErr != nil {
			return CampaignAttemptResult{}, runErr
		}
		artifact, err := NewStatelessCampaignAttemptArtifact(request, spec, execution)
		if err != nil {
			return CampaignAttemptResult{}, err
		}
		encoded, err := json.Marshal(artifact)
		if err != nil {
			return CampaignAttemptResult{}, err
		}
		return CampaignAttemptResult{
			InputDigest: spec.Digest, Outcome: artifact.Outcome,
			Failure: cloneMethodFailure(artifact.Failure), Work: artifact.Work, Artifact: encoded,
		}, nil
	}), nil
}

func statelessCampaignWork(
	source WorkLedger,
	search StatelessDFSWork,
	qualified WorkLedger,
	model ModelWork,
) WorkLedger {
	searchLedger := emptyWork()
	addDFSPhase(&searchLedger.Primary, search.FrontierReconstruction)
	addDFSPhase(&searchLedger.Primary, search.ChildMaterialization)
	addDFSPhase(&searchLedger.Primary, search.ChildVerification)
	total := addWorkLedgers(source, searchLedger)
	total = addWorkLedgers(total, qualified)
	total.Model.Calls += model.Calls
	total.Model.InputTokens += model.InputTokens
	total.Model.OutputTokens += model.OutputTokens
	total.Model.TotalTokens = total.Model.InputTokens + total.Model.OutputTokens
	return total
}

func validStatelessDFSWork(work StatelessDFSWork, ceiling int) bool {
	return ceiling >= 0 && validDFSPhaseWork(work.FrontierReconstruction) &&
		validDFSPhaseWork(work.ChildMaterialization) && validDFSPhaseWork(work.ChildVerification) &&
		work.TotalWorkUnits >= 0 && work.TotalWorkUnits <= ceiling &&
		work.TotalWorkUnits == work.FrontierReconstruction.WorkUnits+
			work.ChildMaterialization.WorkUnits+work.ChildVerification.WorkUnits
}

func validStatelessCampaignModelWork(work ModelWork, spec StatelessCampaignSpec) bool {
	return spec.Budget.MaxModelCalls == 0 && spec.Budget.MaxModelTokens == 0 && work == (ModelWork{})
}

func validStatelessCampaignAgentCalls(
	calls []StatelessAgentCallAudit,
	model ModelWork,
	method StatelessTraversalMethod,
) bool {
	return method.Validate() == nil && len(calls) == 0 && model == (ModelWork{})
}

func cloneStatelessAgentCallAudits(calls []StatelessAgentCallAudit) []StatelessAgentCallAudit {
	if len(calls) == 0 {
		return nil
	}
	return append([]StatelessAgentCallAudit(nil), calls...)
}

func statelessCampaignWorkWithinSpec(
	work WorkLedger,
	budget StatelessCampaignAttemptBudget,
) bool {
	return work.Primary.SchedulerDecisions <= budget.MaxPrimarySchedulerDecisions &&
		work.Primary.WorkUnits <= budget.MaxPrimaryWorkUnits &&
		work.Replay.WorkUnits <= budget.MaxReplayWorkUnits &&
		work.Model.Calls <= budget.MaxModelCalls && work.Model.TotalTokens <= budget.MaxModelTokens
}

func cloneMethodFailure(failure *MethodFailure) *MethodFailure {
	if failure == nil {
		return nil
	}
	copy := *failure
	return &copy
}

func decimalToken(value int) string {
	if value <= 0 {
		return "invalid"
	}
	digits := []byte{}
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}

func decodeStrictJSON(encoded []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("EXPERIMENT_JSON_TRAILING")
	}
	return nil
}

func (spec StatelessCampaignSpec) seal() (StatelessCampaignSpec, error) {
	spec.Methods = append([]StatelessTraversalMethod(nil), spec.Methods...)
	spec.Digest = ""
	digest, err := control.CanonicalDigest(spec)
	spec.Digest = digest
	return spec, err
}

func (artifact StatelessCampaignAttemptArtifact) seal() (StatelessCampaignAttemptArtifact, error) {
	artifact.Failure = cloneMethodFailure(artifact.Failure)
	if artifact.Discovery != nil {
		discovery := *artifact.Discovery
		artifact.Discovery = &discovery
	}
	artifact.AgentCalls = cloneStatelessAgentCallAudits(artifact.AgentCalls)
	artifact.Digest = ""
	digest, err := control.CanonicalDigest(artifact)
	artifact.Digest = digest
	return artifact, err
}
