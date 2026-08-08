package architecture_test

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const checkedEPaxosFeasibility = "../../benchmarks/feasibility/efficient-epaxos-m5.8a/report.json"

type feasibilitySourceFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type feasibilitySource struct {
	Repository    string                  `json:"repository"`
	Commit        string                  `json:"commit"`
	Tree          string                  `json:"tree"`
	ArchiveSHA256 string                  `json:"archive_sha256"`
	Files         []feasibilitySourceFile `json:"files"`
}

type feasibilityFact struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	EvidenceCode   string `json:"evidence_code"`
	EvidenceDigest string `json:"evidence_digest"`
	Detail         string `json:"detail"`
}

type feasibilityBlocker struct {
	Capability string `json:"capability"`
	ReasonCode string `json:"reason_code"`
}

type feasibilityDecision struct {
	Status             string `json:"status"`
	NextAllowed        string `json:"next_allowed"`
	FullAdapterAllowed bool   `json:"full_adapter_allowed"`
	ActionChanges      int    `json:"action_changes"`
	RuntimeChanges     int    `json:"runtime_changes"`
	CorePSSChanges     int    `json:"core_pss_changes"`
}

type epaxosFeasibilityReport struct {
	SchemaVersion string               `json:"schema_version"`
	ReportID      string               `json:"report_id"`
	Source        feasibilitySource    `json:"source"`
	Toolchain     string               `json:"toolchain"`
	Facts         []feasibilityFact    `json:"facts"`
	Blockers      []feasibilityBlocker `json:"blockers"`
	Decision      feasibilityDecision  `json:"decision"`
	Digest        string               `json:"digest"`
}

func TestFrozenEfficientEPaxosFeasibilityMatchesDerivedDecision(t *testing.T) {
	fresh, err := freshEPaxosFeasibility()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(checkedEPaxosFeasibility)
	if err != nil {
		pretty, _ := json.MarshalIndent(fresh, "", "  ")
		t.Fatalf("%v; fresh report:\n%s", err, pretty)
	}
	var checked epaxosFeasibilityReport
	if err := json.Unmarshal(encoded, &checked); err != nil {
		t.Fatal(err)
	}
	if err := validateEPaxosFeasibility(checked); err != nil {
		t.Fatal(err)
	}
	freshJSON, _ := json.Marshal(fresh)
	checkedJSON, _ := json.Marshal(checked)
	if string(freshJSON) != string(checkedJSON) {
		pretty, _ := json.MarshalIndent(fresh, "", "  ")
		t.Fatalf("frozen EPaxos feasibility report is stale; fresh report:\n%s", pretty)
	}
}

func freshEPaxosFeasibility() (epaxosFeasibilityReport, error) {
	const sourceDigest = "9ffde18a3bfb7763c0995ce20064d34c7ddfcf944510a845cbc655a94a676068"
	const smokeDigest = "546fca29d760da30f52d3c24a853d6025de8004a20701dc0daa59ef6e5f730b2"
	const interfaceDigest = "13e6b05ecbd30749a3452e5978c78ffaf21bb8a34de6f10a265b644a0180d8f7"
	report := epaxosFeasibilityReport{
		SchemaVersion: "consensus-atlas/target-feasibility/v1", ReportID: "efficient-epaxos-m5.8a",
		Source: feasibilitySource{
			Repository: "https://github.com/efficient/epaxos.git",
			Commit:     "791b115669fca472d3136f6a2eda46c00b3f8251",
			Tree:       "708b8e37f6b50a4e6b6fcbb95045dc53ad7d6344", ArchiveSHA256: sourceDigest,
			Files: []feasibilitySourceFile{
				{Path: "README.md", SHA256: "75b383c71ee050b140d1f49bc4f605c18a4c9ea1a8d53d60cc2022d38853738f"},
				{Path: "src/README", SHA256: "5d937d521ace3e5a7ad7bad429f752e196344cc7d1d5d8b2b48cb5222ce13653"},
				{Path: "src/client/client.go", SHA256: "630312170c5a368547af5f11a4e94d17d82ab3e622491b7aab68dca6ca8c2553"},
				{Path: "src/epaxos/epaxos-exec.go", SHA256: "1595210f19e8c9bc93e6a40db71b75c93e21dfdd4d4d62249d19ace6b4a4c5e1"},
				{Path: "src/epaxos/epaxos.go", SHA256: "8f7c01cfd2a00d30ce4f5dbb0c7e7325adeaffa66b7fe1c067f61d8196d66488"},
				{Path: "src/genericsmr/genericsmr.go", SHA256: "10f6c8d0e283d8ab197e85266002029f25fd664d273db282f36176f6280f66c3"},
				{Path: "src/server/server.go", SHA256: "7039fa4b92d04732ed93cb4bb928a8c71bd89f90104529ce9654fdedd87e6d94"},
			},
		},
		Toolchain: "go1.25.8-linux-amd64/GO111MODULE=off",
		Facts: []feasibilityFact{
			{ID: "source-lock", Status: "observed", EvidenceCode: "GIT_COMMIT_AND_ARCHIVE_MATCH", EvidenceDigest: sourceDigest, Detail: "official master HEAD and selected files are digest-bound"},
			{ID: "production-build", Status: "observed", EvidenceCode: "LEGACY_GOPATH_BUILD_PASSED", EvidenceDigest: sourceDigest, Detail: "epaxos, genericsmr, server, master and client production packages build"},
			{ID: "upstream-tests", Status: "blocked", EvidenceCode: "UPSTREAM_TEST_FIXTURE_STALE", EvidenceDigest: sourceDigest, Detail: "epaxos_test.go no longer matches the production channel types"},
			{ID: "three-node-smoke", Status: "observed", EvidenceCode: "THREE_NODE_ONE_COMMAND_SUCCESS", EvidenceDigest: smokeDigest, Detail: "three official server processes returned Successful: 1 with client exit 0"},
			{ID: "external-input-surface", Status: "observed", EvidenceCode: "PUBLIC_PROPOSE_CHANNEL_AND_TCP_CLIENT", EvidenceDigest: interfaceDigest, Detail: "opaque commands enter through ProposeChan or the client TCP codec"},
			{ID: "peer-message-surface", Status: "observed", EvidenceCode: "DIRECT_TCP_SEND_AND_RPC_CHANNEL_RECEIVE", EvidenceDigest: interfaceDigest, Detail: "SendMsg writes framed protocol bytes and replicaListener feeds registered protocol channels"},
			{ID: "message-freeze-witness", Status: "missing", EvidenceCode: "NO_STABLE_MESSAGE_ITEM_BOUNDARY", EvidenceDigest: interfaceDigest, Detail: "no real peer frame has yet been frozen, selected and released by Control Runtime"},
			{ID: "semantic-fields", Status: "observed", EvidenceCode: "PUBLIC_INSTANCE_SPACE_AND_FRONTIERS", EvidenceDigest: interfaceDigest, Detail: "InstanceSpace, CommittedUpTo and ExecedUpTo expose candidate mapping inputs"},
			{ID: "stable-yield", Status: "missing", EvidenceCode: "BACKGROUND_SELECT_LOOP_HAS_NO_QUIESCENCE_HANDSHAKE", EvidenceDigest: interfaceDigest, Detail: "constructor starts goroutines and observation has no stable yield identity"},
			{ID: "temporal-control", Status: "unsupported", EvidenceCode: "WALL_CLOCK_SLEEP_NOT_INJECTABLE", EvidenceDigest: interfaceDigest, Detail: "batch, beacon, recovery and execution progress use package time sleeps"},
			{ID: "durable-restart", Status: "unsupported", EvidenceCode: "STABLE_STORE_RECOVERY_UNAVAILABLE", EvidenceDigest: interfaceDigest, Detail: "the fixed source creates and appends a store but exposes no reconstruction path"},
			{ID: "strict-replay", Status: "unsupported", EvidenceCode: "ASYNC_TCP_GOROUTINE_REPLAY_UNPROVEN", EvidenceDigest: smokeDigest, Detail: "successful wall-clock smoke is not a deterministic Action replay witness"},
		},
	}
	sort.Slice(report.Source.Files, func(i, j int) bool { return report.Source.Files[i].Path < report.Source.Files[j].Path })
	sort.Slice(report.Facts, func(i, j int) bool { return report.Facts[i].ID < report.Facts[j].ID })
	report.Decision, report.Blockers = deriveEPaxosDecision(report.Facts)
	report.Digest = ""
	digest, err := control.CanonicalDigest(report)
	if err != nil {
		return epaxosFeasibilityReport{}, err
	}
	report.Digest = digest
	return report, nil
}

func deriveEPaxosDecision(facts []feasibilityFact) (feasibilityDecision, []feasibilityBlocker) {
	status := make(map[string]string, len(facts))
	for _, fact := range facts {
		status[fact.ID] = fact.Status
	}
	required := []string{"source-lock", "production-build", "three-node-smoke", "external-input-surface", "peer-message-surface", "semantic-fields"}
	decision := feasibilityDecision{Status: "stop", NextAllowed: "none"}
	ready := true
	for _, id := range required {
		ready = ready && status[id] == "observed"
	}
	if ready {
		decision.Status = "proceed-limited"
		decision.NextAllowed = "test-only-message-port-worker-spike"
	}
	reasons := map[string]feasibilityBlocker{
		"message-freeze-witness": {Capability: "runtime-owned-message", ReasonCode: "NO_STABLE_MESSAGE_ITEM_BOUNDARY"},
		"stable-yield":           {Capability: "strict-yield-evidence", ReasonCode: "BACKGROUND_SELECT_LOOP_HAS_NO_QUIESCENCE_HANDSHAKE"},
		"temporal-control":       {Capability: "natural-temporal-progress", ReasonCode: "WALL_CLOCK_SLEEP_NOT_INJECTABLE"},
		"durable-restart":        {Capability: "durable-restart", ReasonCode: "STABLE_STORE_RECOVERY_UNAVAILABLE"},
		"strict-replay":          {Capability: "strict-decision-replay", ReasonCode: "ASYNC_TCP_GOROUTINE_REPLAY_UNPROVEN"},
	}
	blockers := make([]feasibilityBlocker, 0, len(reasons))
	for id, blocker := range reasons {
		if status[id] != "observed" {
			blockers = append(blockers, blocker)
		}
	}
	sort.Slice(blockers, func(i, j int) bool { return blockers[i].Capability < blockers[j].Capability })
	return decision, blockers
}

func validateEPaxosFeasibility(report epaxosFeasibilityReport) error {
	fresh, err := freshEPaxosFeasibility()
	if err != nil {
		return err
	}
	if report.Digest != fresh.Digest {
		return os.ErrInvalid
	}
	return nil
}
