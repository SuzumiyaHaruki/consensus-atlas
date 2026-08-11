use omnipaxos::macros::Entry;
use omnipaxos::messages::Message;
use omnipaxos::util::LogEntry;
use omnipaxos::{ClusterConfig, OmniPaxos, OmniPaxosConfig, ServerConfig};
use omnipaxos_storage::memory_storage::MemoryStorage;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, VecDeque};

const SCHEMA: &str = "consensus-atlas/omnipaxos-deterministic-core-probe/v1";
const MAX_ELECTION_ROUNDS: u64 = 16;
const MAX_DELIVERIES: usize = 512;
const REQUEST_ID: &str = "request-m5.21u-1";
const REQUEST_VALUE: &str = "opaque-value-m5.21u";

#[derive(Entry, Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
struct ProbeEntry {
    request_id: String,
    value: String,
}

type ProbeNode = OmniPaxos<ProbeEntry, MemoryStorage<ProbeEntry>>;

#[derive(Clone, Debug, PartialEq, Eq)]
struct RunResult {
    leader: u64,
    decided_index: u64,
    decided_nodes: Vec<u64>,
    pending_message_peak: usize,
    trace: Vec<String>,
}

#[derive(Serialize)]
struct ProbeReport {
    schema_version: &'static str,
    crate_name: &'static str,
    crate_version: &'static str,
    protocol_family: &'static str,
    node_count: usize,
    harness_explicit_leader_calls: usize,
    harness_wall_clock_calls: usize,
    harness_thread_spawns: usize,
    harness_random_calls: usize,
    fresh_runs_per_process: usize,
    replay_equal: bool,
    elected_leader: u64,
    append_origin: u64,
    decided_index: u64,
    decided_nodes: Vec<u64>,
    decisions: usize,
    pending_message_peak: usize,
    trace_digest: String,
    capabilities_demonstrated: Vec<&'static str>,
    capabilities_not_demonstrated: Vec<&'static str>,
    classification: &'static str,
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let first = run_cluster()?;
    let second = run_cluster()?;
    if first != second {
        return Err("OMNIPAXOS_FRESH_REPLAY_MISMATCH".into());
    }
    if first.leader != 1 || first.decided_nodes.len() < 2 || first.decided_index == 0 {
        return Err("OMNIPAXOS_EXPECTED_ELECTION_AND_DECISION_MISSING".into());
    }

    let report = ProbeReport {
        schema_version: SCHEMA,
        crate_name: "omnipaxos",
        crate_version: "0.2.2",
        protocol_family: "sequence-paxos-with-ballot-leader-election",
        node_count: 3,
        harness_explicit_leader_calls: 0,
        harness_wall_clock_calls: 0,
        harness_thread_spawns: 0,
        harness_random_calls: 0,
        fresh_runs_per_process: 2,
        replay_equal: true,
        elected_leader: first.leader,
        append_origin: 2,
        decided_index: first.decided_index,
        decided_nodes: first.decided_nodes,
        decisions: first.trace.len(),
        pending_message_peak: first.pending_message_peak,
        trace_digest: digest_trace(&first.trace),
        capabilities_demonstrated: vec![
            "plain-synchronous-state-machine",
            "logical-tick-input",
            "scheduler-owned-pending-message",
            "explicit-message-delivery",
            "non-leader-opaque-append",
            "decided-log-output",
            "fresh-in-process-trace-equality",
        ],
        capabilities_not_demonstrated: vec![
            "consensus-atlas-adapter",
            "crash-restart-durable-image",
            "suspendable-durable-host-effect",
            "control-runtime-strict-replay",
            "audited-sut-entropy-replay",
            "qualification",
            "core-pss-mapping",
            "risk-witness-projector",
            "workload-router",
            "leaderless-dependency-graph",
        ],
        classification: "non-raft-candidate-core-probe-pass-not-adapter-qualified",
    };
    println!("{}", serde_json::to_string_pretty(&report)?);
    Ok(())
}

fn run_cluster() -> Result<RunResult, Box<dyn std::error::Error>> {
    let mut nodes = build_nodes()?;
    let mut messages = VecDeque::new();
    let mut trace = Vec::new();
    let mut pending_message_peak = 0;

    let leader = elect_naturally(
        &mut nodes,
        &mut messages,
        &mut trace,
        &mut pending_message_peak,
    )?;
    if leader != 1 {
        return Err(format!("OMNIPAXOS_UNEXPECTED_LEADER:{leader}").into());
    }

    nodes
        .get_mut(&2)
        .ok_or("OMNIPAXOS_APPEND_ORIGIN_MISSING")?
        .append(ProbeEntry {
            request_id: REQUEST_ID.to_string(),
            value: REQUEST_VALUE.to_string(),
        })
        .map_err(|_| "OMNIPAXOS_APPEND_REJECTED")?;
    trace.push(format!("append:n2:{REQUEST_ID}"));
    drain_outgoing(
        &mut nodes,
        &mut messages,
        &mut trace,
        &mut pending_message_peak,
    );
    drive_until_decided(
        &mut nodes,
        &mut messages,
        &mut trace,
        &mut pending_message_peak,
    )?;

    let decided_nodes = verify_decided(&nodes)?;
    let decided_index = decided_nodes
        .iter()
        .filter_map(|id| nodes.get(id).map(ProbeNode::get_decided_idx))
        .min()
        .unwrap_or(0);
    Ok(RunResult {
        leader,
        decided_index,
        decided_nodes,
        pending_message_peak,
        trace,
    })
}

fn build_nodes() -> Result<BTreeMap<u64, ProbeNode>, Box<dyn std::error::Error>> {
    let mut nodes = BTreeMap::new();
    for id in 1..=3 {
        let config = OmniPaxosConfig {
            cluster_config: ClusterConfig {
                configuration_id: 1,
                nodes: vec![1, 2, 3],
                flexible_quorum: None,
            },
            server_config: ServerConfig {
                pid: id,
                election_tick_timeout: 2,
                resend_message_tick_timeout: 100,
                buffer_size: 1024,
                batch_size: 1,
                leader_priority: 4 - id as u32,
            },
        };
        nodes.insert(id, config.build(MemoryStorage::default())?);
    }
    Ok(nodes)
}

fn elect_naturally(
    nodes: &mut BTreeMap<u64, ProbeNode>,
    messages: &mut VecDeque<Message<ProbeEntry>>,
    trace: &mut Vec<String>,
    pending_message_peak: &mut usize,
) -> Result<u64, Box<dyn std::error::Error>> {
    for round in 1..=MAX_ELECTION_ROUNDS {
        for (id, node) in nodes.iter_mut() {
            node.tick();
            trace.push(format!("tick:n{id}:r{round}"));
        }
        drain_outgoing(nodes, messages, trace, pending_message_peak);
        drive_pending(nodes, messages, trace, pending_message_peak)?;
        let leaders: Vec<u64> = nodes
            .values()
            .filter_map(ProbeNode::get_current_leader)
            .collect();
        if leaders.len() == nodes.len() && leaders.iter().all(|leader| *leader == leaders[0]) {
            return Ok(leaders[0]);
        }
    }
    Err("OMNIPAXOS_ELECTION_ROUND_LIMIT".into())
}

fn drive_until_decided(
    nodes: &mut BTreeMap<u64, ProbeNode>,
    messages: &mut VecDeque<Message<ProbeEntry>>,
    trace: &mut Vec<String>,
    pending_message_peak: &mut usize,
) -> Result<(), Box<dyn std::error::Error>> {
    for _ in 0..MAX_DELIVERIES {
        if nodes
            .values()
            .filter(|node| node.get_decided_idx() > 0)
            .count()
            >= 2
        {
            return Ok(());
        }
        let message = messages
            .pop_front()
            .ok_or("OMNIPAXOS_QUIESCENT_BEFORE_DECISION")?;
        deliver(nodes, message, messages, trace, pending_message_peak);
    }
    Err("OMNIPAXOS_DECISION_DELIVERY_LIMIT".into())
}

fn drive_pending(
    nodes: &mut BTreeMap<u64, ProbeNode>,
    messages: &mut VecDeque<Message<ProbeEntry>>,
    trace: &mut Vec<String>,
    pending_message_peak: &mut usize,
) -> Result<(), Box<dyn std::error::Error>> {
    for _ in 0..MAX_DELIVERIES {
        let Some(message) = messages.pop_front() else {
            return Ok(());
        };
        deliver(nodes, message, messages, trace, pending_message_peak);
    }
    Err("OMNIPAXOS_PENDING_DELIVERY_LIMIT".into())
}

fn deliver(
    nodes: &mut BTreeMap<u64, ProbeNode>,
    message: Message<ProbeEntry>,
    messages: &mut VecDeque<Message<ProbeEntry>>,
    trace: &mut Vec<String>,
    pending_message_peak: &mut usize,
) {
    let from = message.get_sender();
    let to = message.get_receiver();
    trace.push(format!("deliver:n{from}:n{to}:{}", message_kind(&message)));
    nodes
        .get_mut(&to)
        .expect("validated cluster message target")
        .handle_incoming(message);
    drain_outgoing(nodes, messages, trace, pending_message_peak);
}

fn drain_outgoing(
    nodes: &mut BTreeMap<u64, ProbeNode>,
    messages: &mut VecDeque<Message<ProbeEntry>>,
    trace: &mut Vec<String>,
    pending_message_peak: &mut usize,
) {
    for (id, node) in nodes.iter_mut() {
        let outgoing = node.outgoing_messages();
        if !outgoing.is_empty() {
            trace.push(format!("freeze:n{id}:{}", outgoing.len()));
        }
        messages.extend(outgoing);
    }
    *pending_message_peak = (*pending_message_peak).max(messages.len());
}

fn verify_decided(
    nodes: &BTreeMap<u64, ProbeNode>,
) -> Result<Vec<u64>, Box<dyn std::error::Error>> {
    let mut decided_nodes = Vec::new();
    for (id, node) in nodes {
        if node.get_decided_idx() == 0 {
            continue;
        }
        let entries = node
            .read_decided_suffix(0)
            .ok_or("OMNIPAXOS_DECIDED_SUFFIX_MISSING")?;
        let found = entries.iter().any(|entry| {
            matches!(entry, LogEntry::Decided(value)
                if value.request_id == REQUEST_ID && value.value == REQUEST_VALUE)
        });
        if !found {
            return Err(format!("OMNIPAXOS_DECIDED_VALUE_MISMATCH:n{id}").into());
        }
        decided_nodes.push(*id);
    }
    if decided_nodes.len() < 2 {
        return Err("OMNIPAXOS_DECIDED_QUORUM_MISSING".into());
    }
    Ok(decided_nodes)
}

fn message_kind(message: &Message<ProbeEntry>) -> &'static str {
    match message {
        Message::BLE(_) => "ble",
        Message::SequencePaxos(_) => "sequence-paxos",
    }
}

fn digest_trace(trace: &[String]) -> String {
    let mut hasher = Sha256::new();
    for event in trace {
        hasher.update(event.as_bytes());
        hasher.update(b"\n");
    }
    format!("{:x}", hasher.finalize())
}
