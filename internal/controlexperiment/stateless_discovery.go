package controlexperiment

import (
	"errors"
	"reflect"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

const (
	StatelessDiscoverySchemaVersion     = "consensus-atlas/stateless-traversal-discovery/v1"
	StatelessDiscoveryItemSchemaVersion = "consensus-atlas/stateless-traversal-discovery-item/v1"
	StatelessCorpusDiscoveryVersion     = "consensus-atlas/stateless-corpus-discovery/v1"
	StatelessCorpusRootResultVersion    = "consensus-atlas/stateless-corpus-root-result/v1"
	StatelessPSSViewJoint               = "joint"
	StatelessPSSViewProtocol            = "protocol"
)

type StatelessCorpusRootEvidence struct {
	RootID  string
	Result  StatelessTraversalResult
	Bundles []ExecutionBundle
}

type StatelessCorpusRootResult struct {
	SchemaVersion             string           `json:"schema_version"`
	RootID                    string           `json:"root_id"`
	RootPrefixDigest          string           `json:"root_prefix_digest"`
	SearchDigest              string           `json:"search_digest"`
	DiscoveryDigest           string           `json:"discovery_digest"`
	RootPSSStates             int              `json:"root_pss_states"`
	LocalIncrementalPSSStates int              `json:"local_incremental_pss_states"`
	SearchWork                StatelessDFSWork `json:"search_work"`
	QualifiedExecutionWork    WorkLedger       `json:"qualified_execution_work"`
	Digest                    string           `json:"digest"`
}

// StatelessCorpusDiscovery aggregates one traversal method over a root corpus.
// CorpusNovel removes every state already present in any corpus root baseline.
type StatelessCorpusDiscovery struct {
	SchemaVersion                string                      `json:"schema_version"`
	ID                           string                      `json:"id"`
	CorpusDigest                 string                      `json:"corpus_digest"`
	MethodDigest                 string                      `json:"method_digest"`
	PSSView                      string                      `json:"pss_view,omitempty"`
	Roots                        []StatelessCorpusRootResult `json:"roots"`
	CorpusBaselinePSSStates      int                         `json:"corpus_baseline_pss_states"`
	CorpusBaselinePSSKeys        []string                    `json:"corpus_baseline_pss_keys"`
	CorpusBaselinePSSSetDigest   string                      `json:"corpus_baseline_pss_set_digest"`
	LocalIncrementalPSSStates    int                         `json:"local_incremental_pss_states"`
	LocalIncrementalPSSKeys      []string                    `json:"local_incremental_pss_keys"`
	LocalIncrementalPSSSetDigest string                      `json:"local_incremental_pss_set_digest"`
	CorpusNovelPSSStates         int                         `json:"corpus_novel_pss_states"`
	CorpusNovelPSSKeys           []string                    `json:"corpus_novel_pss_keys"`
	CorpusNovelPSSSetDigest      string                      `json:"corpus_novel_pss_set_digest"`
	SearchWork                   StatelessDFSWork            `json:"search_work"`
	QualifiedExecutionWork       WorkLedger                  `json:"qualified_execution_work"`
	QualifiedExecutionAttempts   int                         `json:"qualified_execution_attempts"`
	MarginalEvidenceWorkUnits    int                         `json:"marginal_evidence_work_units"`
	Digest                       string                      `json:"digest"`
}

// StatelessDiscoveryItem binds one trusted WorkItem to a separately executed
// qualified bundle. Since A9e3b, its PSS novelty fields use the independently
// canonicalized protocol view; control and joint diversity remain derivable
// from each bundle's Core PSS and do not count as protocol discovery.
type StatelessDiscoveryItem struct {
	SchemaVersion                   string `json:"schema_version"`
	Ordinal                         int    `json:"ordinal"`
	WorkItemDigest                  string `json:"work_item_digest"`
	BundleDigest                    string `json:"bundle_digest"`
	TraceDigest                     string `json:"trace_digest"`
	PSSSamplesDigest                string `json:"pss_samples_digest"`
	PSSSamples                      int    `json:"pss_samples"`
	UniqueBundlePSSStates           int    `json:"unique_bundle_pss_states"`
	FinalChildPSSKey                string `json:"final_child_pss_key"`
	NewTracePrefix                  bool   `json:"new_trace_prefix"`
	NewFinalChildPSSState           bool   `json:"new_final_child_pss_state"`
	NewIncrementalPSSStates         int    `json:"new_incremental_pss_states"`
	CumulativeUniqueTracePrefixes   int    `json:"cumulative_unique_trace_prefixes"`
	CumulativeUniqueFinalChildState int    `json:"cumulative_unique_final_child_states"`
	CumulativeIncrementalPSSStates  int    `json:"cumulative_incremental_pss_states"`
	Digest                          string `json:"digest"`
}

// StatelessTraversalDiscovery is a read-only post-search projection. It does
// not influence traversal order and does not interpret PSS as correctness,
// coverage, or defect evidence.
type StatelessTraversalDiscovery struct {
	SchemaVersion              string                   `json:"schema_version"`
	ID                         string                   `json:"id"`
	MethodDigest               string                   `json:"method_digest"`
	SearchDigest               string                   `json:"search_digest"`
	RootPrefixDigest           string                   `json:"root_prefix_digest"`
	ManifestDigest             string                   `json:"manifest_digest"`
	PSSID                      string                   `json:"pss_id"`
	PSSView                    string                   `json:"pss_view,omitempty"`
	Items                      []StatelessDiscoveryItem `json:"items"`
	UniqueTracePrefixes        int                      `json:"unique_trace_prefixes"`
	UniqueFinalChildPSSStates  int                      `json:"unique_final_child_pss_states"`
	UniqueObservedPSSStates    int                      `json:"unique_observed_pss_states"`
	ObservedPSSKeys            []string                 `json:"observed_pss_keys"`
	RootPSSSamplesDigest       string                   `json:"root_pss_samples_digest"`
	RootPSSStates              int                      `json:"root_pss_states"`
	RootPSSSetDigest           string                   `json:"root_pss_set_digest"`
	UniqueIncrementalPSSStates int                      `json:"unique_incremental_pss_states"`
	IncrementalPSSKeys         []string                 `json:"incremental_pss_keys"`
	IncrementalPSSSetDigest    string                   `json:"incremental_pss_set_digest"`
	TracePrefixSetDigest       string                   `json:"trace_prefix_set_digest"`
	FinalChildPSSSetDigest     string                   `json:"final_child_pss_set_digest"`
	ObservedPSSSetDigest       string                   `json:"observed_pss_set_digest"`
	QualifiedExecutionWork     WorkLedger               `json:"qualified_execution_work"`
	QualifiedExecutionAttempts int                      `json:"qualified_execution_attempts"`
	Digest                     string                   `json:"digest"`
}

func NewStatelessTraversalDiscovery(
	id string,
	result StatelessTraversalResult,
	root controlruntime.Trace,
	bundles []ExecutionBundle,
	mapper psscore.SemanticMapper,
) (StatelessTraversalDiscovery, error) {
	if !validMethodToken(id) || result.Validate(root) != nil ||
		len(bundles) != len(result.Search.Items) || len(bundles) == 0 ||
		mapper == nil || mapper.ID() == "" {
		return StatelessTraversalDiscovery{}, errors.New("EXPERIMENT_STATELESS_DISCOVERY_INPUT_INVALID")
	}
	discovery := StatelessTraversalDiscovery{
		SchemaVersion: StatelessDiscoverySchemaVersion,
		ID:            id, MethodDigest: result.Method.Digest, SearchDigest: result.Search.Digest,
		RootPrefixDigest: root.Digest, ManifestDigest: root.ManifestDigest,
		PSSView:                StatelessPSSViewProtocol,
		QualifiedExecutionWork: emptyWork(),
	}
	traceSet := make(map[string]bool, len(bundles))
	childSet := make(map[string]bool, len(bundles))
	observedSet := make(map[string]bool)
	rootSet := make(map[string]bool)
	incrementalSet := make(map[string]bool)
	for index, bundle := range bundles {
		item := result.Search.Items[index]
		projectedPSS, err := reprojectBundleCorePSS(bundle, mapper)
		if err != nil || bundle.Identity.ManifestDigest != root.ManifestDigest ||
			bundle.Trace.Digest != item.ChildPrefixDigest ||
			bundle.Trace.FinalStateDigest != item.ChildStateDigest ||
			len(bundle.Trace.Records) != item.Path.Decision ||
			len(projectedPSS) <= result.Search.Spec.RootDecisions {
			return StatelessTraversalDiscovery{}, errors.New("EXPERIMENT_STATELESS_DISCOVERY_BUNDLE_MISMATCH")
		}
		viewKeys := make([]psscore.ViewKeys, len(projectedPSS))
		states := make([]psscore.State, len(projectedPSS))
		for sampleIndex, sample := range projectedPSS {
			viewKeys[sampleIndex], err = psscore.Keys(sample.State)
			if err != nil {
				return StatelessTraversalDiscovery{}, err
			}
			states[sampleIndex] = sample.State
		}
		viewSummary, err := psscore.SummarizeStates(states)
		if err != nil || viewSummary.Validate() != nil {
			return StatelessTraversalDiscovery{}, errors.New("EXPERIMENT_STATELESS_DISCOVERY_PSS_VIEW_INVALID")
		}
		if discovery.PSSID == "" {
			discovery.PSSID = bundle.Identity.PSSID
		} else if discovery.PSSID != bundle.Identity.PSSID {
			return StatelessTraversalDiscovery{}, errors.New("EXPERIMENT_STATELESS_DISCOVERY_PSS_MISMATCH")
		}
		newTrace := !traceSet[bundle.Trace.Digest]
		traceSet[bundle.Trace.Digest] = true
		finalKey := viewKeys[len(viewKeys)-1].Protocol
		newChild := !childSet[finalKey]
		childSet[finalKey] = true
		rootSamples := projectedPSS[:result.Search.Spec.RootDecisions+1]
		rootSamplesDigest, digestErr := control.CanonicalDigest(rootSamples)
		if digestErr != nil {
			return StatelessTraversalDiscovery{}, digestErr
		}
		if discovery.RootPSSSamplesDigest == "" {
			discovery.RootPSSSamplesDigest = rootSamplesDigest
			for _, keys := range viewKeys[:result.Search.Spec.RootDecisions+1] {
				rootSet[keys.Protocol] = true
			}
		} else if discovery.RootPSSSamplesDigest != rootSamplesDigest {
			return StatelessTraversalDiscovery{}, errors.New("EXPERIMENT_STATELESS_DISCOVERY_ROOT_PSS_DRIFT")
		}
		for _, keys := range viewKeys {
			observedSet[keys.Protocol] = true
		}
		newIncremental := 0
		for _, keys := range viewKeys[result.Search.Spec.RootDecisions+1:] {
			if !rootSet[keys.Protocol] && !incrementalSet[keys.Protocol] {
				incrementalSet[keys.Protocol] = true
				newIncremental++
			}
		}
		projected := StatelessDiscoveryItem{
			SchemaVersion: StatelessDiscoveryItemSchemaVersion,
			Ordinal:       index + 1, WorkItemDigest: item.Digest,
			BundleDigest: bundle.Digest, TraceDigest: bundle.Trace.Digest,
			PSSSamplesDigest: bundle.Run.CorePSSSamplesDigest,
			PSSSamples:       len(projectedPSS), UniqueBundlePSSStates: viewSummary.ProtocolStates,
			FinalChildPSSKey: finalKey, NewTracePrefix: newTrace,
			NewFinalChildPSSState:           newChild,
			NewIncrementalPSSStates:         newIncremental,
			CumulativeUniqueTracePrefixes:   len(traceSet),
			CumulativeUniqueFinalChildState: len(childSet),
			CumulativeIncrementalPSSStates:  len(incrementalSet),
		}
		sealed, err := projected.seal()
		if err != nil {
			return StatelessTraversalDiscovery{}, err
		}
		discovery.Items = append(discovery.Items, sealed)
		discovery.QualifiedExecutionWork = addWorkLedgers(discovery.QualifiedExecutionWork, bundle.Work)
	}
	discovery.QualifiedExecutionAttempts = len(bundles)
	discovery.UniqueTracePrefixes = len(traceSet)
	discovery.UniqueFinalChildPSSStates = len(childSet)
	discovery.UniqueObservedPSSStates = len(observedSet)
	discovery.ObservedPSSKeys = sortedStringSet(observedSet)
	discovery.RootPSSStates = len(rootSet)
	discovery.UniqueIncrementalPSSStates = len(incrementalSet)
	discovery.IncrementalPSSKeys = sortedStringSet(incrementalSet)
	var err error
	discovery.TracePrefixSetDigest, err = stringSetDigest(traceSet)
	if err != nil {
		return StatelessTraversalDiscovery{}, err
	}
	discovery.FinalChildPSSSetDigest, err = stringSetDigest(childSet)
	if err != nil {
		return StatelessTraversalDiscovery{}, err
	}
	discovery.ObservedPSSSetDigest, err = stringSetDigest(observedSet)
	if err != nil {
		return StatelessTraversalDiscovery{}, err
	}
	discovery.RootPSSSetDigest, err = stringSetDigest(rootSet)
	if err != nil {
		return StatelessTraversalDiscovery{}, err
	}
	discovery.IncrementalPSSSetDigest, err = stringSetDigest(incrementalSet)
	if err != nil {
		return StatelessTraversalDiscovery{}, err
	}
	return discovery.seal()
}

func (discovery StatelessTraversalDiscovery) Validate(
	result StatelessTraversalResult,
	root controlruntime.Trace,
	bundles []ExecutionBundle,
	mapper psscore.SemanticMapper,
) error {
	want, err := NewStatelessTraversalDiscovery(discovery.ID, result, root, bundles, mapper)
	if err != nil || !reflect.DeepEqual(want, discovery) {
		return errors.New("EXPERIMENT_STATELESS_DISCOVERY_MISMATCH")
	}
	return nil
}

func NewStatelessCorpusDiscovery(
	id string,
	corpus StatelessRootCorpus,
	source ExecutionBundle,
	evidence []StatelessCorpusRootEvidence,
	mapper psscore.SemanticMapper,
) (StatelessCorpusDiscovery, error) {
	if !validMethodToken(id) || corpus.Validate(source) != nil || len(evidence) != len(corpus.Roots) ||
		len(evidence) == 0 || mapper == nil || mapper.ID() == "" {
		return StatelessCorpusDiscovery{}, errors.New("EXPERIMENT_STATELESS_CORPUS_DISCOVERY_INPUT_INVALID")
	}
	result := StatelessCorpusDiscovery{
		SchemaVersion: StatelessCorpusDiscoveryVersion, ID: id, CorpusDigest: corpus.Digest,
		PSSView: StatelessPSSViewProtocol, QualifiedExecutionWork: emptyWork(),
	}
	baseline := make(map[string]bool)
	localIncremental := make(map[string]bool)
	for index, input := range evidence {
		rootEntry := corpus.Roots[index]
		if input.RootID != rootEntry.ID {
			return StatelessCorpusDiscovery{}, errors.New("EXPERIMENT_STATELESS_CORPUS_ROOT_ORDER_MISMATCH")
		}
		root, err := corpus.Prefix(source, input.RootID)
		if err != nil || input.Result.Validate(root) != nil {
			return StatelessCorpusDiscovery{}, errors.New("EXPERIMENT_STATELESS_CORPUS_RESULT_INVALID")
		}
		if result.MethodDigest == "" {
			result.MethodDigest = input.Result.Method.Digest
		} else if result.MethodDigest != input.Result.Method.Digest {
			return StatelessCorpusDiscovery{}, errors.New("EXPERIMENT_STATELESS_CORPUS_METHOD_DRIFT")
		}
		discovery, err := NewStatelessTraversalDiscovery(
			id+"-"+rootEntry.ID, input.Result, root, input.Bundles, mapper,
		)
		if err != nil {
			return StatelessCorpusDiscovery{}, err
		}
		if discovery.PSSView != result.PSSView {
			return StatelessCorpusDiscovery{}, errors.New("EXPERIMENT_STATELESS_CORPUS_PSS_VIEW_MISMATCH")
		}
		incremental := make(map[string]bool, len(discovery.IncrementalPSSKeys))
		for _, key := range discovery.IncrementalPSSKeys {
			incremental[key], localIncremental[key] = true, true
		}
		for _, key := range discovery.ObservedPSSKeys {
			if !incremental[key] {
				baseline[key] = true
			}
		}
		rootResult, err := (StatelessCorpusRootResult{
			SchemaVersion: StatelessCorpusRootResultVersion, RootID: rootEntry.ID,
			RootPrefixDigest: root.Digest, SearchDigest: input.Result.Search.Digest,
			DiscoveryDigest: discovery.Digest, RootPSSStates: discovery.RootPSSStates,
			LocalIncrementalPSSStates: discovery.UniqueIncrementalPSSStates,
			SearchWork:                input.Result.Search.Work,
			QualifiedExecutionWork:    discovery.QualifiedExecutionWork,
		}).seal()
		if err != nil {
			return StatelessCorpusDiscovery{}, err
		}
		result.Roots = append(result.Roots, rootResult)
		addStatelessDFSWork(&result.SearchWork, input.Result.Search.Work)
		result.QualifiedExecutionWork = addWorkLedgers(
			result.QualifiedExecutionWork, discovery.QualifiedExecutionWork,
		)
		result.QualifiedExecutionAttempts += discovery.QualifiedExecutionAttempts
	}
	corpusNovel := make(map[string]bool)
	for key := range localIncremental {
		if !baseline[key] {
			corpusNovel[key] = true
		}
	}
	result.CorpusBaselinePSSKeys = sortedStringSet(baseline)
	result.CorpusBaselinePSSStates = len(baseline)
	result.LocalIncrementalPSSKeys = sortedStringSet(localIncremental)
	result.LocalIncrementalPSSStates = len(localIncremental)
	result.CorpusNovelPSSKeys = sortedStringSet(corpusNovel)
	result.CorpusNovelPSSStates = len(corpusNovel)
	var err error
	result.CorpusBaselinePSSSetDigest, err = stringSetDigest(baseline)
	if err != nil {
		return StatelessCorpusDiscovery{}, err
	}
	result.LocalIncrementalPSSSetDigest, err = stringSetDigest(localIncremental)
	if err != nil {
		return StatelessCorpusDiscovery{}, err
	}
	result.CorpusNovelPSSSetDigest, err = stringSetDigest(corpusNovel)
	if err != nil {
		return StatelessCorpusDiscovery{}, err
	}
	result.MarginalEvidenceWorkUnits = result.SearchWork.TotalWorkUnits +
		result.QualifiedExecutionWork.Primary.WorkUnits +
		result.QualifiedExecutionWork.Replay.WorkUnits
	return result.seal()
}

func (discovery StatelessCorpusDiscovery) Validate(
	corpus StatelessRootCorpus,
	source ExecutionBundle,
	evidence []StatelessCorpusRootEvidence,
	mapper psscore.SemanticMapper,
) error {
	want, err := NewStatelessCorpusDiscovery(discovery.ID, corpus, source, evidence, mapper)
	if err != nil || !reflect.DeepEqual(want, discovery) {
		return errors.New("EXPERIMENT_STATELESS_CORPUS_DISCOVERY_MISMATCH")
	}
	return nil
}

// ValidateStructure checks the sealed, self-contained discovery boundary used
// by durable Campaign artifacts. Full evidence validation remains the stronger
// Validate(corpus, source, evidence, mapper) path at the target composition.
func (discovery StatelessCorpusDiscovery) ValidateStructure() error {
	if discovery.SchemaVersion != StatelessCorpusDiscoveryVersion || !validMethodToken(discovery.ID) ||
		!validSHA256(discovery.CorpusDigest) || !validSHA256(discovery.MethodDigest) ||
		!validStatelessPSSView(discovery.PSSView) ||
		len(discovery.Roots) == 0 || discovery.CorpusBaselinePSSStates < 0 ||
		discovery.LocalIncrementalPSSStates < 0 || discovery.CorpusNovelPSSStates < 0 ||
		discovery.CorpusBaselinePSSStates != len(discovery.CorpusBaselinePSSKeys) ||
		discovery.LocalIncrementalPSSStates != len(discovery.LocalIncrementalPSSKeys) ||
		discovery.CorpusNovelPSSStates != len(discovery.CorpusNovelPSSKeys) ||
		!validSHA256(discovery.CorpusBaselinePSSSetDigest) ||
		!validSHA256(discovery.LocalIncrementalPSSSetDigest) ||
		!validSHA256(discovery.CorpusNovelPSSSetDigest) ||
		discovery.QualifiedExecutionAttempts <= 0 ||
		discovery.QualifiedExecutionAttempts < len(discovery.Roots) ||
		validateMethodWork(discovery.QualifiedExecutionWork) != nil ||
		discovery.QualifiedExecutionWork.Model != (ModelWork{}) ||
		!validStatelessDFSWork(discovery.SearchWork, int(^uint(0)>>1)) ||
		discovery.MarginalEvidenceWorkUnits != discovery.SearchWork.TotalWorkUnits+
			discovery.QualifiedExecutionWork.Primary.WorkUnits+
			discovery.QualifiedExecutionWork.Replay.WorkUnits ||
		!canonicalStrings(discovery.CorpusBaselinePSSKeys, false) ||
		!canonicalStrings(discovery.LocalIncrementalPSSKeys, false) ||
		!canonicalStrings(discovery.CorpusNovelPSSKeys, false) {
		return errors.New("EXPERIMENT_STATELESS_CORPUS_DISCOVERY_INVALID")
	}
	var rootSearch StatelessDFSWork
	rootExecution := emptyWork()
	for _, root := range discovery.Roots {
		if root.SchemaVersion != StatelessCorpusRootResultVersion || !validMethodToken(root.RootID) ||
			!validSHA256(root.RootPrefixDigest) || !validSHA256(root.SearchDigest) ||
			!validSHA256(root.DiscoveryDigest) || root.RootPSSStates < 0 ||
			root.LocalIncrementalPSSStates < 0 ||
			!validStatelessDFSWork(root.SearchWork, int(^uint(0)>>1)) ||
			validateMethodWork(root.QualifiedExecutionWork) != nil ||
			root.QualifiedExecutionWork.Model != (ModelWork{}) {
			return errors.New("EXPERIMENT_STATELESS_CORPUS_ROOT_INVALID")
		}
		sealed, err := root.seal()
		if err != nil || sealed.Digest != root.Digest {
			return errors.New("EXPERIMENT_STATELESS_CORPUS_ROOT_DIGEST_MISMATCH")
		}
		addStatelessDFSWork(&rootSearch, root.SearchWork)
		rootExecution = addWorkLedgers(rootExecution, root.QualifiedExecutionWork)
	}
	if rootSearch != discovery.SearchWork || rootExecution != discovery.QualifiedExecutionWork {
		return errors.New("EXPERIMENT_STATELESS_CORPUS_WORK_MISMATCH")
	}
	wantBaseline, err := control.CanonicalDigest(discovery.CorpusBaselinePSSKeys)
	if err != nil || wantBaseline != discovery.CorpusBaselinePSSSetDigest {
		return errors.New("EXPERIMENT_STATELESS_CORPUS_BASELINE_DIGEST_MISMATCH")
	}
	wantLocal, err := control.CanonicalDigest(discovery.LocalIncrementalPSSKeys)
	if err != nil || wantLocal != discovery.LocalIncrementalPSSSetDigest {
		return errors.New("EXPERIMENT_STATELESS_CORPUS_LOCAL_DIGEST_MISMATCH")
	}
	wantNovel, err := control.CanonicalDigest(discovery.CorpusNovelPSSKeys)
	if err != nil || wantNovel != discovery.CorpusNovelPSSSetDigest {
		return errors.New("EXPERIMENT_STATELESS_CORPUS_NOVEL_DIGEST_MISMATCH")
	}
	want, err := discovery.seal()
	if err != nil || !validSHA256(discovery.Digest) || want.Digest != discovery.Digest {
		return errors.New("EXPERIMENT_STATELESS_CORPUS_DISCOVERY_DIGEST_MISMATCH")
	}
	return nil
}

func validStatelessPSSView(view string) bool {
	return view == "" || view == StatelessPSSViewJoint || view == StatelessPSSViewProtocol
}

func normalizedStatelessPSSView(view string) string {
	if view == "" {
		return StatelessPSSViewJoint
	}
	return view
}

func addStatelessDFSWork(total *StatelessDFSWork, delta StatelessDFSWork) {
	addDFSPhase(&total.FrontierReconstruction, delta.FrontierReconstruction)
	addDFSPhase(&total.ChildMaterialization, delta.ChildMaterialization)
	addDFSPhase(&total.ChildVerification, delta.ChildVerification)
	total.TotalWorkUnits += delta.TotalWorkUnits
}

func stringSetDigest(set map[string]bool) (string, error) {
	return control.CanonicalDigest(sortedStringSet(set))
}

func sortedStringSet(set map[string]bool) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func (item StatelessDiscoveryItem) seal() (StatelessDiscoveryItem, error) {
	item.Digest = ""
	digest, err := control.CanonicalDigest(item)
	if err != nil {
		return StatelessDiscoveryItem{}, err
	}
	item.Digest = digest
	return item, nil
}

func (result StatelessCorpusRootResult) seal() (StatelessCorpusRootResult, error) {
	result.Digest = ""
	digest, err := control.CanonicalDigest(result)
	if err != nil {
		return StatelessCorpusRootResult{}, err
	}
	result.Digest = digest
	return result, nil
}

func (discovery StatelessCorpusDiscovery) seal() (StatelessCorpusDiscovery, error) {
	discovery.Roots = append([]StatelessCorpusRootResult(nil), discovery.Roots...)
	discovery.CorpusBaselinePSSKeys = append([]string(nil), discovery.CorpusBaselinePSSKeys...)
	discovery.LocalIncrementalPSSKeys = append([]string(nil), discovery.LocalIncrementalPSSKeys...)
	discovery.CorpusNovelPSSKeys = append([]string(nil), discovery.CorpusNovelPSSKeys...)
	discovery.Digest = ""
	digest, err := control.CanonicalDigest(discovery)
	if err != nil {
		return StatelessCorpusDiscovery{}, err
	}
	discovery.Digest = digest
	return discovery, nil
}

func (discovery StatelessTraversalDiscovery) seal() (StatelessTraversalDiscovery, error) {
	discovery.Items = append([]StatelessDiscoveryItem(nil), discovery.Items...)
	discovery.ObservedPSSKeys = append([]string(nil), discovery.ObservedPSSKeys...)
	discovery.IncrementalPSSKeys = append([]string(nil), discovery.IncrementalPSSKeys...)
	discovery.Digest = ""
	digest, err := control.CanonicalDigest(discovery)
	if err != nil {
		return StatelessTraversalDiscovery{}, err
	}
	discovery.Digest = digest
	return discovery, nil
}
