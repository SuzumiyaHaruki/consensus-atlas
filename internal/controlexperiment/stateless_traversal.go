package controlexperiment

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

const (
	StatelessTraversalSchemaVersion = "consensus-atlas/stateless-traversal-method/v1"
	StatelessTraversalResultVersion = "consensus-atlas/stateless-traversal-result/v1"

	StatelessTraversalCanonical     = "canonical-action-id-depth-first/v1"
	StatelessTraversalSeededUniform = "seeded-uniform-frontier-depth-first/v1"
	StatelessTraversalAgentOrder    = "validated-frontier-order/v1"
)

// StatelessTraversalMethod controls only the order in which already trusted
// ActionRefs are visited. It cannot add, remove, or rewrite a frontier action.
type StatelessTraversalMethod struct {
	SchemaVersion   string `json:"schema_version"`
	ID              string `json:"id"`
	Strategy        string `json:"strategy"`
	SeedHex         string `json:"seed_hex,omitempty"`
	PlannerID       string `json:"planner_id,omitempty"`
	KnowledgeDigest string `json:"knowledge_digest,omitempty"`
	Digest          string `json:"digest"`
}

// StatelessTraversalResult binds a traversal identity to the unchanged DFS
// evidence. Existing M5.23a/b StatelessDFSResult identities remain frozen.
type StatelessTraversalResult struct {
	SchemaVersion string                   `json:"schema_version"`
	Method        StatelessTraversalMethod `json:"method"`
	Search        StatelessDFSResult       `json:"search"`
	Digest        string                   `json:"digest"`
}

func NewStatelessTraversalMethod(
	id string,
	strategy string,
	seedHex string,
) (StatelessTraversalMethod, error) {
	method := StatelessTraversalMethod{
		SchemaVersion: StatelessTraversalSchemaVersion,
		ID:            id,
		Strategy:      strategy,
		SeedHex:       seedHex,
	}
	if err := method.validateInputs(); err != nil {
		return StatelessTraversalMethod{}, err
	}
	return method.seal()
}

func NewStatelessAgentTraversalMethod(
	id string,
	plannerID string,
	knowledge ProtocolKnowledgePack,
) (StatelessTraversalMethod, error) {
	if knowledge.Validate() != nil {
		return StatelessTraversalMethod{}, errors.New("EXPERIMENT_STATELESS_TRAVERSAL_KNOWLEDGE_INVALID")
	}
	method := StatelessTraversalMethod{
		SchemaVersion: StatelessTraversalSchemaVersion, ID: id,
		Strategy: StatelessTraversalAgentOrder, PlannerID: plannerID,
		KnowledgeDigest: knowledge.Digest,
	}
	if err := method.validateInputs(); err != nil {
		return StatelessTraversalMethod{}, err
	}
	return method.seal()
}

func (method StatelessTraversalMethod) Validate() error {
	if err := method.validateInputs(); err != nil {
		return err
	}
	sealed, err := method.seal()
	if err != nil || !validSHA256(method.Digest) || sealed.Digest != method.Digest {
		return errors.New("EXPERIMENT_STATELESS_TRAVERSAL_METHOD_DIGEST_MISMATCH")
	}
	return nil
}

func (method StatelessTraversalMethod) validateInputs() error {
	if method.SchemaVersion != StatelessTraversalSchemaVersion || !validMethodToken(method.ID) {
		return errors.New("EXPERIMENT_STATELESS_TRAVERSAL_METHOD_INVALID")
	}
	switch method.Strategy {
	case StatelessTraversalCanonical:
		if method.SeedHex != "" || method.PlannerID != "" || method.KnowledgeDigest != "" {
			return errors.New("EXPERIMENT_STATELESS_TRAVERSAL_CANONICAL_SEED_INVALID")
		}
	case StatelessTraversalSeededUniform:
		seed, err := hex.DecodeString(method.SeedHex)
		if err != nil || len(seed) == 0 || method.PlannerID != "" || method.KnowledgeDigest != "" {
			return errors.New("EXPERIMENT_STATELESS_TRAVERSAL_SEED_INVALID")
		}
	case StatelessTraversalAgentOrder:
		if method.SeedHex != "" || !validMethodToken(method.PlannerID) ||
			!validSHA256(method.KnowledgeDigest) {
			return errors.New("EXPERIMENT_STATELESS_TRAVERSAL_AGENT_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_STATELESS_TRAVERSAL_STRATEGY_INVALID")
	}
	return nil
}

// ExploreBoundedStatelessDFSWithMethod compares traversal orders on the same
// exact-prefix executor, bounds, child verification, and work accounting.
func ExploreBoundedStatelessDFSWithMethod(
	ctx context.Context,
	method StatelessTraversalMethod,
	spec StatelessDFSSpec,
	root controlruntime.Trace,
	newAdapter AdapterFactory,
) (StatelessTraversalResult, error) {
	if err := method.Validate(); err != nil {
		return StatelessTraversalResult{}, err
	}
	if method.Strategy == StatelessTraversalAgentOrder {
		return StatelessTraversalResult{}, errors.New("EXPERIMENT_STATELESS_TRAVERSAL_AGENT_PLANNER_REQUIRED")
	}
	search, err := exploreBoundedStatelessDFS(ctx, spec, root, newAdapter, func(view ActionFrontierView) ([]FrontierActionRef, error) {
		return orderStatelessTraversalActions(&method, view)
	})
	if err != nil {
		return StatelessTraversalResult{}, err
	}
	result := StatelessTraversalResult{
		SchemaVersion: StatelessTraversalResultVersion,
		Method:        method,
		Search:        search,
	}
	sealed, err := result.seal()
	if err != nil {
		return StatelessTraversalResult{}, err
	}
	if err := sealed.Validate(root); err != nil {
		return StatelessTraversalResult{}, err
	}
	return sealed, nil
}

func orderStatelessTraversalActions(
	method *StatelessTraversalMethod,
	view ActionFrontierView,
) ([]FrontierActionRef, error) {
	actions := append([]FrontierActionRef(nil), view.Actions...)
	if method == nil || method.Strategy == StatelessTraversalCanonical {
		return actions, nil
	}
	if method.Strategy != StatelessTraversalSeededUniform {
		return nil, errors.New("EXPERIMENT_STATELESS_TRAVERSAL_STRATEGY_INVALID")
	}
	seed, err := hex.DecodeString(method.SeedHex)
	if err != nil || len(seed) == 0 {
		return nil, errors.New("EXPERIMENT_STATELESS_TRAVERSAL_SEED_INVALID")
	}
	contextDigest := view.PrefixTraceDigest + view.AdmissibleDigest
	for upper := len(actions) - 1; upper > 0; upper-- {
		step := len(actions) - upper
		selected := domainSeparatedRandomIndex(
			seed, step, "consensus-atlas/stateless-frontier-permutation/v1\x00",
			contextDigest, upper+1,
		)
		actions[upper], actions[selected] = actions[selected], actions[upper]
	}
	return actions, nil
}

func (result StatelessTraversalResult) Validate(root controlruntime.Trace) error {
	if result.SchemaVersion != StatelessTraversalResultVersion || result.Method.Validate() != nil ||
		result.Search.Validate(root) != nil {
		return errors.New("EXPERIMENT_STATELESS_TRAVERSAL_RESULT_INVALID")
	}
	sealed, err := result.seal()
	if err != nil || !validSHA256(result.Digest) || sealed.Digest != result.Digest {
		return errors.New("EXPERIMENT_STATELESS_TRAVERSAL_RESULT_DIGEST_MISMATCH")
	}
	return nil
}

func (method StatelessTraversalMethod) seal() (StatelessTraversalMethod, error) {
	method.Digest = ""
	digest, err := control.CanonicalDigest(method)
	if err != nil {
		return StatelessTraversalMethod{}, err
	}
	method.Digest = digest
	return method, nil
}

func (result StatelessTraversalResult) seal() (StatelessTraversalResult, error) {
	result.Digest = ""
	digest, err := control.CanonicalDigest(result)
	if err != nil {
		return StatelessTraversalResult{}, err
	}
	result.Digest = digest
	return result, nil
}
