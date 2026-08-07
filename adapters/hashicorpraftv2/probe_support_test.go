package hashicorpraftv2

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	hraft "github.com/hashicorp/raft"
)

const (
	ProbeSchema = "consensus-atlas/hashicorp-raft-probe/v1"
	RPCSchema   = "consensus-atlas/hashicorp-raft-rpc/v1"
)

type SUTIdentity struct {
	Module      string `json:"module"`
	Version     string `json:"version"`
	Revision    string `json:"revision"`
	GoModuleSum string `json:"go_module_sum"`
}

type ClusterIdentity struct {
	LocalNode        string   `json:"local_node"`
	Voters           []string `json:"voters"`
	ProtocolVersion  int      `json:"protocol_version"`
	HeartbeatTimeout string   `json:"heartbeat_timeout"`
	ElectionTimeout  string   `json:"election_timeout"`
	ConfigDigest     string   `json:"config_digest"`
}

type Finding struct {
	Capability string `json:"capability"`
	Status     string `json:"status"`
	ReasonCode string `json:"reason_code,omitempty"`
}

type Report struct {
	SchemaVersion string                 `json:"schema_version"`
	ProbeID       string                 `json:"probe_id"`
	SUT           SUTIdentity            `json:"sut"`
	Cluster       ClusterIdentity        `json:"cluster"`
	Items         []control.ProducedItem `json:"items"`
	Findings      []Finding              `json:"findings"`
	Qualified     bool                   `json:"qualified"`
	StrictReplay  bool                   `json:"strict_replay"`
	Digest        string                 `json:"digest"`
}

// RunProbe is retained only to replay the frozen M5.4a exploratory artifact.
// The production Adapter and qualification runner supersede this surface.
func RunProbe(ctx context.Context) (Report, error) {
	outbound := make(chan *rpcCall, 8)
	cancel := make(chan struct{})
	transport := newRuntimeTransport(control.NodeRef{Node: "n1", Incarnation: 1}, outbound, cancel)
	node, conf, err := startOfficialNode("n1", raftTiming{10 * time.Millisecond, 10 * time.Millisecond, 5 * time.Millisecond},
		recordingFSM{"n1", make(chan appliedRecord, 1)}, transport, newRaftStores(), true)
	if err != nil {
		return Report{}, err
	}
	defer func() {
		close(cancel)
		_ = node.Shutdown().Error()
	}()

	byTarget := make(map[control.NodeID]control.ProducedItem, 2)
	for len(byTarget) < 2 {
		select {
		case call := <-outbound:
			item, err := probeVoteItem(call)
			if err != nil {
				return Report{}, err
			}
			byTarget[item.Message.Target] = item
		case <-ctx.Done():
			return Report{}, fmt.Errorf("capture outbound votes: %w", ctx.Err())
		}
	}
	items := make([]control.ProducedItem, 0, len(byTarget))
	for _, item := range byTarget {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Message.Target < items[j].Message.Target })
	return sealReport(items, conf)
}

func sealReport(items []control.ProducedItem, conf *hraft.Config) (Report, error) {
	for _, item := range items {
		if err := item.Validate(); err != nil {
			return Report{}, fmt.Errorf("validate frozen RPC: %w", err)
		}
	}
	cluster := ClusterIdentity{
		LocalNode: "n1", Voters: []string{"n1", "n2", "n3"},
		ProtocolVersion: int(conf.ProtocolVersion), HeartbeatTimeout: conf.HeartbeatTimeout.String(),
		ElectionTimeout: conf.ElectionTimeout.String(),
	}
	var err error
	cluster.ConfigDigest, err = control.CanonicalDigest(cluster)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		SchemaVersion: ProbeSchema, ProbeID: "hashicorp-raft-v1.7.3-m5.4a",
		SUT: SUTIdentity{modulePath, moduleVersion, moduleRevision, moduleSum}, Cluster: cluster,
		Items: items, Qualified: false, StrictReplay: false,
		Findings: []Finding{
			{"official-module", "observed", ""},
			{"transport-outbound-message", "observed", ""},
			{"runtime-item-freeze", "observed", ""},
			{"virtual-clock", "unsupported", clockReasonCode},
			{"entropy-control", "unsupported", rngReasonCode},
			{"runtime-delivery-replay", "not-tested", "M5_4A_CAPTURE_ONLY"},
		},
	}
	report.Digest, err = control.CanonicalDigest(report)
	return report, err
}

type requestVotePayload struct {
	Target             string `json:"target"`
	ProtocolVersion    int    `json:"protocol_version"`
	SenderID           string `json:"sender_id"`
	SenderAddress      string `json:"sender_address"`
	Term               uint64 `json:"term"`
	LastLogIndex       uint64 `json:"last_log_index"`
	LastLogTerm        uint64 `json:"last_log_term"`
	LeadershipTransfer bool   `json:"leadership_transfer"`
}

func probeVoteItem(call *rpcCall) (control.ProducedItem, error) {
	req, ok := call.request.(*hraft.RequestVoteRequest)
	if !ok || call.item.Message == nil {
		return control.ProducedItem{}, fmt.Errorf("HASHICORP_RAFT_PROBE_VOTE_REQUIRED")
	}
	target := call.item.Message.Target
	envelope, err := control.NewJSONPayload(RPCSchema, requestVotePayload{
		Target: string(target), ProtocolVersion: int(req.ProtocolVersion),
		SenderID: string(req.ID), SenderAddress: string(req.Addr), Term: req.Term,
		LastLogIndex: req.LastLogIndex, LastLogTerm: req.LastLogTerm,
		LeadershipTransfer: req.LeadershipTransfer,
	})
	if err != nil {
		return control.ProducedItem{}, err
	}
	id, err := control.StableID("hashicorp-rpc", "n1", string(target), "request-vote", envelope.Digest)
	if err != nil {
		return control.ProducedItem{}, err
	}
	owner := control.NodeRef{Node: "n1", Incarnation: 1}
	return control.ProducedItem{
		ID: control.ItemID(id), Kind: control.ItemMessage, Owner: owner,
		Message: &control.MessageEnvelope{
			ID: control.MessageID(id), Source: owner, Target: control.NodeID(target),
			TypeHint: "request-vote", Payload: envelope,
			Metadata: map[string]string{"implementation": modulePath, "version": moduleVersion},
		},
	}, nil
}
