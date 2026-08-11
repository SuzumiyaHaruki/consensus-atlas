use raft::prelude::*;
use raft::storage::MemStorage;
use raft::{Config, RawNode, StateRole};
use serde::Serialize;
use sha2::{Digest, Sha256};
use slog::{o, Drain, Logger};
use std::collections::{BTreeMap, VecDeque};

const SCHEMA: &str = "consensus-atlas/raft-rs-deterministic-core-probe/v1";
const MAX_STEPS: usize = 256;

struct Node {
    raw: RawNode<MemStorage>,
    applied: u64,
}

#[derive(Clone, Debug, PartialEq, Eq)]
struct RunResult {
    leader: u64,
    committed_index: u64,
    trace: Vec<String>,
}

#[derive(Serialize)]
struct ProbeReport {
    schema_version: &'static str,
    crate_name: &'static str,
    crate_version: &'static str,
    node_count: usize,
    harness_explicit_campaign_calls: usize,
    harness_wall_clock_calls: usize,
    harness_thread_spawns: usize,
    fresh_runs_per_process: usize,
    replay_equal: bool,
    leader: u64,
    committed_index: u64,
    decisions: usize,
    trace_digest: String,
    capabilities_demonstrated: Vec<&'static str>,
    capabilities_not_demonstrated: Vec<&'static str>,
    classification: &'static str,
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let first = run_cluster()?;
    let second = run_cluster()?;
    if first != second {
        return Err("RAFT_RS_FRESH_REPLAY_MISMATCH".into());
    }
    if first.leader != 1 || first.committed_index == 0 {
        return Err("RAFT_RS_EXPECTED_ELECTION_AND_COMMIT_MISSING".into());
    }
    let report = ProbeReport {
        schema_version: SCHEMA,
        crate_name: "raft",
        crate_version: "0.7.0",
        node_count: 3,
        harness_explicit_campaign_calls: 0,
        harness_wall_clock_calls: 0,
        harness_thread_spawns: 0,
        fresh_runs_per_process: 2,
        replay_equal: true,
        leader: first.leader,
        committed_index: first.committed_index,
        decisions: first.trace.len(),
        trace_digest: digest_trace(&first.trace),
        capabilities_demonstrated: vec![
            "synchronous-raw-node",
            "logical-tick-input",
            "explicit-message-step",
            "ready-host-boundary",
            "fixed-election-range",
            "fresh-in-process-trace-equality",
        ],
        capabilities_not_demonstrated: vec![
            "consensus-atlas-adapter",
            "crash-restart-durable-image",
            "control-runtime-strict-replay",
            "audited-sut-entropy-replay",
            "qualification",
            "core-pss-mapping",
            "risk-witness-projector",
            "workload-router",
        ],
        classification: "candidate-core-probe-pass-not-second-target-qualified",
    };
    println!("{}", serde_json::to_string_pretty(&report)?);
    Ok(())
}

fn run_cluster() -> Result<RunResult, Box<dyn std::error::Error>> {
    let logger = Logger::root(slog::Discard.fuse(), o!());
    let mut nodes = BTreeMap::new();
    for id in 1..=3 {
        let mut config = Config::new(id);
        config.heartbeat_tick = 1;
        config.election_tick = 5;
        config.min_election_tick = if id == 1 { 5 } else { 10 + id as usize };
        config.max_election_tick = config.min_election_tick + 1;
        config.max_size_per_msg = 1024 * 1024;
        config.max_inflight_msgs = 32;
        config.validate()?;
        let storage = MemStorage::new_with_conf_state((vec![1, 2, 3], vec![]));
        nodes.insert(
            id,
            Node {
                raw: RawNode::new(&config, storage, &logger)?,
                applied: 0,
            },
        );
    }

    let mut trace = Vec::new();
    let mut messages = VecDeque::new();
    for tick in 1..=5 {
        nodes.get_mut(&1).unwrap().raw.tick();
        trace.push(format!("tick:n1:{tick}"));
        drain_all(&mut nodes, &mut messages, &mut trace)?;
    }
    drive_messages(&mut nodes, &mut messages, &mut trace)?;
    let leader = current_leader(&nodes);
    if leader != 1 {
        return Err(format!("RAFT_RS_UNEXPECTED_LEADER:{leader}").into());
    }

    nodes
        .get_mut(&leader)
        .unwrap()
        .raw
        .propose(Vec::new(), b"consensus-atlas-m5.21r".to_vec())?;
    trace.push(format!("propose:n{leader}:consensus-atlas-m5.21r"));
    drain_all(&mut nodes, &mut messages, &mut trace)?;
    drive_until_commit(&mut nodes, &mut messages, &mut trace)?;
    let committed_index = nodes.values().map(|node| node.applied).max().unwrap_or(0);
    Ok(RunResult {
        leader,
        committed_index,
        trace,
    })
}

fn drive_messages(
    nodes: &mut BTreeMap<u64, Node>,
    messages: &mut VecDeque<Message>,
    trace: &mut Vec<String>,
) -> Result<(), Box<dyn std::error::Error>> {
    for _ in 0..MAX_STEPS {
        if current_leader(nodes) != 0 {
            return Ok(());
        }
        let message = messages
            .pop_front()
            .ok_or("RAFT_RS_ELECTION_QUIESCENT_WITHOUT_LEADER")?;
        deliver(nodes, message, messages, trace)?;
    }
    Err("RAFT_RS_ELECTION_STEP_LIMIT".into())
}

fn drive_until_commit(
    nodes: &mut BTreeMap<u64, Node>,
    messages: &mut VecDeque<Message>,
    trace: &mut Vec<String>,
) -> Result<(), Box<dyn std::error::Error>> {
    for _ in 0..MAX_STEPS {
        if nodes.values().filter(|node| node.applied > 0).count() >= 2 {
            return Ok(());
        }
        let message = messages
            .pop_front()
            .ok_or("RAFT_RS_PROPOSAL_QUIESCENT_BEFORE_COMMIT")?;
        deliver(nodes, message, messages, trace)?;
    }
    Err("RAFT_RS_COMMIT_STEP_LIMIT".into())
}

fn deliver(
    nodes: &mut BTreeMap<u64, Node>,
    message: Message,
    messages: &mut VecDeque<Message>,
    trace: &mut Vec<String>,
) -> Result<(), Box<dyn std::error::Error>> {
    let target = message.to;
    trace.push(format!(
        "deliver:n{}:n{}:{:?}:t{}:i{}",
        message.from,
        message.to,
        message.get_msg_type(),
        message.term,
        message.index
    ));
    nodes
        .get_mut(&target)
        .ok_or("RAFT_RS_MESSAGE_TARGET_UNKNOWN")?
        .raw
        .step(message)?;
    drain_all(nodes, messages, trace)
}

fn drain_all(
    nodes: &mut BTreeMap<u64, Node>,
    messages: &mut VecDeque<Message>,
    trace: &mut Vec<String>,
) -> Result<(), Box<dyn std::error::Error>> {
    loop {
        let ready_id = nodes
            .iter()
            .find_map(|(id, node)| node.raw.has_ready().then_some(*id));
        let Some(id) = ready_id else {
            return Ok(());
        };
        drain_ready(nodes.get_mut(&id).unwrap(), id, messages, trace)?;
    }
}

fn drain_ready(
    node: &mut Node,
    id: u64,
    messages: &mut VecDeque<Message>,
    trace: &mut Vec<String>,
) -> Result<(), Box<dyn std::error::Error>> {
    let mut ready = node.raw.ready();
    if !ready.snapshot().is_empty() {
        node.raw
            .mut_store()
            .wl()
            .apply_snapshot(ready.snapshot().clone())?;
    }
    if !ready.entries().is_empty() {
        node.raw.mut_store().wl().append(ready.entries())?;
    }
    if let Some(hard_state) = ready.hs() {
        node.raw.mut_store().wl().set_hardstate(hard_state.clone());
    }
    enqueue_messages(ready.take_messages(), messages, trace);
    enqueue_messages(ready.take_persisted_messages(), messages, trace);
    apply_committed(id, ready.take_committed_entries(), &mut node.applied, trace);

    let mut light = node.raw.advance(ready);
    enqueue_messages(light.take_messages(), messages, trace);
    apply_committed(id, light.take_committed_entries(), &mut node.applied, trace);
    if node.applied > 0 {
        node.raw.advance_apply_to(node.applied);
    }
    Ok(())
}

fn enqueue_messages(
    mut produced: Vec<Message>,
    messages: &mut VecDeque<Message>,
    trace: &mut Vec<String>,
) {
    produced.sort_by_key(message_key);
    for message in produced {
        trace.push(format!(
            "release:n{}:n{}:{:?}:t{}:i{}",
            message.from,
            message.to,
            message.get_msg_type(),
            message.term,
            message.index
        ));
        messages.push_back(message);
    }
}

fn apply_committed(id: u64, entries: Vec<Entry>, applied: &mut u64, trace: &mut Vec<String>) {
    for entry in entries {
        *applied = (*applied).max(entry.index);
        trace.push(format!(
            "apply:n{id}:i{}:t{}:{:?}:{}",
            entry.index,
            entry.term,
            entry.get_entry_type(),
            entry.data.len()
        ));
    }
}

fn message_key(message: &Message) -> (u64, u64, i32, u64, u64) {
    (
        message.from,
        message.to,
        message.get_msg_type() as i32,
        message.term,
        message.index,
    )
}

fn current_leader(nodes: &BTreeMap<u64, Node>) -> u64 {
    nodes
        .iter()
        .find_map(|(id, node)| (node.raw.raft.state == StateRole::Leader).then_some(*id))
        .unwrap_or(0)
}

fn digest_trace(trace: &[String]) -> String {
    let mut digest = Sha256::new();
    for record in trace {
        digest.update(record.as_bytes());
        digest.update([b'\n']);
    }
    format!("{:x}", digest.finalize())
}
