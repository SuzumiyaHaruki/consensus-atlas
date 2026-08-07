package hashicorpraftv2

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	hraft "github.com/hashicorp/raft"
)

type determinismAudit struct {
	SchemaVersion                string           `json:"schema_version"`
	AuditID                      string           `json:"audit_id"`
	SUT                          SUTIdentity      `json:"sut"`
	PublicConfigFields           []string         `json:"public_config_fields"`
	ExportedInjectionIdentifiers []string         `json:"exported_injection_identifiers"`
	Sources                      []sourceBoundary `json:"sources"`
	Findings                     []auditFinding   `json:"findings"`
	Decision                     string           `json:"decision"`
	ProductionGoDelta            int              `json:"production_go_delta"`
	Digest                       string           `json:"canonical_digest"`
}

type sourceBoundary struct {
	Path               string `json:"path"`
	SHA256             string `json:"sha256"`
	WallClockCalls     int    `json:"wall_clock_calls"`
	RandomTimeoutCalls int    `json:"random_timeout_calls"`
	EntropyCalls       int    `json:"entropy_calls"`
}

type auditFinding struct {
	Capability string   `json:"capability"`
	Status     string   `json:"status"`
	ReasonCode string   `json:"reason_code"`
	Evidence   []string `json:"evidence"`
}

func TestOfficialDeterminismBoundaryMatchesFrozenAudit(t *testing.T) {
	fresh := auditOfficialModule(t)
	encoded, err := os.ReadFile("../../benchmarks/probes/hashicorp-raft-v1.7.3-m5.4d/report.json")
	if err != nil {
		t.Fatal(err)
	}
	var frozen determinismAudit
	if err := json.Unmarshal(encoded, &frozen); err != nil {
		t.Fatal(err)
	}
	wantDigest := frozen.Digest
	frozen.Digest = ""
	gotDigest, err := control.CanonicalDigest(frozen)
	if err != nil || gotDigest != wantDigest {
		t.Fatalf("frozen audit digest invalid: got=%s want=%s err=%v", gotDigest, wantDigest, err)
	}
	if fresh.Digest != wantDigest {
		freshDigest := fresh.Digest
		fresh.Digest = ""
		detail, _ := json.MarshalIndent(fresh, "", "  ")
		t.Fatalf("official source boundary changed: fresh=%s frozen=%s\n%s", freshDigest, wantDigest, detail)
	}
}

func auditOfficialModule(t *testing.T) determinismAudit {
	t.Helper()
	command := exec.Command("go", "list", "-m", "-json", modulePath)
	command.Dir = "../.."
	encoded, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var module struct {
		Path, Version, Sum, Dir string
		Replace                 *json.RawMessage
	}
	if err := json.Unmarshal(encoded, &module); err != nil {
		t.Fatal(err)
	}
	if module.Path != modulePath || module.Version != moduleVersion || module.Sum != moduleSum || module.Replace != nil {
		t.Fatalf("unexpected module identity: %+v", module)
	}

	paths := []string{"config.go", "log.go", "raft.go", "replication.go", "snapshot.go", "util.go"}
	sources := make([]sourceBoundary, 0, len(paths))
	for _, path := range paths {
		body, err := os.ReadFile(filepath.Join(module.Dir, path))
		if err != nil {
			t.Fatal(err)
		}
		wall, randomTimeout, entropy := countBoundaryCalls(t, path, body)
		sum := sha256.Sum256(body)
		sources = append(sources, sourceBoundary{
			Path: path, SHA256: hex.EncodeToString(sum[:]), WallClockCalls: wall,
			RandomTimeoutCalls: randomTimeout, EntropyCalls: entropy,
		})
	}
	audit := determinismAudit{
		SchemaVersion:                "consensus-atlas/hashicorp-raft-determinism-audit/v1",
		AuditID:                      "hashicorp-raft-v1.7.3-m5.4d",
		SUT:                          SUTIdentity{modulePath, moduleVersion, moduleRevision, moduleSum},
		PublicConfigFields:           configFields(),
		ExportedInjectionIdentifiers: exportedInjectionIdentifiers(t, module.Dir),
		Sources:                      sources,
		Findings: []auditFinding{
			{
				Capability: "virtual-clock", Status: "unsupported", ReasonCode: clockReasonCode,
				Evidence: []string{"CONFIG_HAS_NO_CLOCK_OR_TIMER_HOOK", "RANDOM_TIMEOUT_USES_TIME_AFTER", "RAFT_PATHS_READ_WALL_CLOCK_DIRECTLY"},
			},
			{
				Capability: "sut-entropy-control", Status: "unsupported", ReasonCode: rngReasonCode,
				Evidence: []string{"CONFIG_HAS_NO_RNG_OR_ENTROPY_HOOK", "PACKAGE_INIT_SEEDS_GLOBAL_MATH_RAND", "RANDOM_TIMEOUT_USES_GLOBAL_RAND"},
			},
			{
				Capability: "strict-replay", Status: "unsupported", ReasonCode: "WALL_CLOCK_AND_OPERATIONAL_TIMESTAMP_NOT_NORMALIZED",
				Evidence: []string{"VIRTUAL_CLOCK_UNSUPPORTED", "SUT_ENTROPY_CONTROL_UNSUPPORTED", "LOG_APPENDED_AT_USES_TIME_NOW"},
			},
		},
		Decision: "freeze-unsupported", ProductionGoDelta: 0,
	}
	audit.Digest, err = control.CanonicalDigest(audit)
	if err != nil {
		t.Fatal(err)
	}
	return audit
}

func countBoundaryCalls(t *testing.T, path string, body []byte) (wall, randomTimeout, entropy int) {
	t.Helper()
	text := string(body)
	for _, name := range []string{"After", "AfterFunc", "NewTicker", "NewTimer", "Now", "Since"} {
		wall += strings.Count(text, "time."+name+"(")
	}
	randomTimeout = strings.Count(text, "randomTimeout(")
	if path == "util.go" {
		randomTimeout-- // exclude the function declaration
	}
	entropy = len(regexp.MustCompile(`\b(?:rand|crand)\.[A-Za-z0-9_]+\(`).FindAllStringIndex(text, -1))
	return wall, randomTimeout, entropy
}

func configFields() []string {
	config := reflect.TypeOf(hraft.Config{})
	fields := make([]string, 0, config.NumField())
	for index := 0; index < config.NumField(); index++ {
		field := config.Field(index)
		if field.IsExported() {
			fields = append(fields, field.Name)
		}
	}
	sort.Strings(fields)
	return fields
}

func exportedInjectionIdentifiers(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]string, 0)
	function := regexp.MustCompile(`(?m)^func (?:\([^\n)]*\) )?([A-Z][A-Za-z0-9_]*)`)
	declaration := regexp.MustCompile(`(?m)^(?:type|var|const) ([A-Z][A-Za-z0-9_]*)`)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range function.FindAllSubmatch(body, -1) {
			name := string(match[1])
			if injectionName(name) {
				result = append(result, entry.Name()+":func:"+name)
			}
		}
		for _, match := range declaration.FindAllSubmatch(body, -1) {
			name := string(match[1])
			if injectionName(name) {
				result = append(result, entry.Name()+":declaration:"+name)
			}
		}
	}
	sort.Strings(result)
	return result
}

func injectionName(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "clock") || strings.Contains(lower, "timer") ||
		strings.Contains(lower, "random") || strings.Contains(lower, "entropy")
}
