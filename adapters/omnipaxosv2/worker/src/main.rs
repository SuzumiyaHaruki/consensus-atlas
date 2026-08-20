use base64::engine::general_purpose::STANDARD as BASE64;
use base64::Engine;
use omnipaxos::ballot_leader_election::Ballot;
use omnipaxos::macros::Entry;
use omnipaxos::messages::ballot_leader_election::HeartbeatMsg;
use omnipaxos::messages::sequence_paxos::{Compaction, PaxosMsg};
use omnipaxos::messages::Message;
use omnipaxos::util::{LogEntry, SequenceNumber};
use omnipaxos::{ClusterConfig, OmniPaxos, OmniPaxosConfig, ServerConfig};
use omnipaxos_storage::memory_storage::MemoryStorage;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::collections::BTreeMap;
use std::io::{self, BufRead, Write};

const SCHEMA: &str = "consensus-atlas/omnipaxos-worker/v5";
const DEFAULT_NODE_COUNT: u64 = 3;
const MAX_NODE_COUNT: u64 = 64;
const ELECTION_TICK_TIMEOUT: u64 = 2;
const RESEND_MESSAGE_TICK_TIMEOUT: u64 = 100;
const BUFFER_SIZE: usize = 1024;
const BATCH_SIZE: usize = 1;

#[derive(Entry, Clone, Debug, Serialize, Deserialize)]
struct WorkerEntry {
    request_id: String,
    origin: u64,
    value: String,
}

type Node = OmniPaxos<WorkerEntry, MemoryStorage<WorkerEntry>>;

struct Cluster {
    nodes: BTreeMap<u64, Node>,
    reported: BTreeMap<u64, u64>,
    prefix_caches: BTreeMap<u64, DecisionPrefixCache>,
    configuration: WorkerConfiguration,
}

struct DecisionPrefixCache {
    decided: u64,
    digest: Sha256,
    prefixes: Vec<DecisionPrefixView>,
}

#[derive(Deserialize)]
struct Request {
    id: u64,
    op: String,
    #[serde(default)]
    node: u64,
    #[serde(default)]
    node_count: u64,
    #[serde(default)]
    payload: String,
}

#[derive(Serialize)]
struct Response {
    schema_version: &'static str,
    id: u64,
    ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    configuration: Option<WorkerConfiguration>,
    nodes: Vec<NodeView>,
    messages: Vec<MessageView>,
    decisions: Vec<DecisionView>,
}

#[derive(Clone, Debug, PartialEq, Serialize)]
struct WorkerConfiguration {
    node_count: u64,
    election_tick_timeout: u64,
    resend_message_tick_timeout: u64,
    buffer_size: usize,
    batch_size: usize,
    leader_priorities: Vec<u32>,
}

#[derive(Serialize)]
struct NodeView {
    id: u64,
    leader: u64,
    decided_index: u64,
    decided_prefix_digest: String,
    decided_prefixes: Vec<DecisionPrefixView>,
    promise_number: u32,
    promise_priority: u32,
    promise_pid: u64,
}

#[derive(Clone, Serialize)]
struct DecisionPrefixView {
    index: u64,
    digest: String,
}

#[derive(Serialize)]
struct MessageView {
    from: u64,
    to: u64,
    family: &'static str,
    type_hint: &'static str,
    metadata: BTreeMap<String, String>,
    bytes: String,
}

#[derive(Serialize)]
struct DecisionView {
    node: u64,
    index: u64,
    #[serde(flatten)]
    entry: WorkerEntry,
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let stdin = io::stdin();
    let mut stdout = io::BufWriter::new(io::stdout().lock());
    let mut cluster: Option<Cluster> = None;
    for line in stdin.lock().lines() {
        let line = line?;
        let request: Request = serde_json::from_str(&line)?;
        let id = request.id;
        let response = match handle(&mut cluster, request) {
            Ok(response) => response,
            Err(error) => Response {
                schema_version: SCHEMA,
                id,
                ok: false,
                error: Some(error.to_string()),
                configuration: None,
                nodes: Vec::new(),
                messages: Vec::new(),
                decisions: Vec::new(),
            },
        };
        serde_json::to_writer(&mut stdout, &response)?;
        stdout.write_all(b"\n")?;
        stdout.flush()?;
    }
    Ok(())
}

fn handle(
    cluster: &mut Option<Cluster>,
    request: Request,
) -> Result<Response, Box<dyn std::error::Error>> {
    match request.op.as_str() {
        "reset" => *cluster = Some(Cluster::new(request.node_count)?),
        "tick" => cluster
            .as_mut()
            .ok_or("OMNIPAXOS_WORKER_RESET_REQUIRED")?
            .tick(request.node)?,
        "step" => cluster
            .as_mut()
            .ok_or("OMNIPAXOS_WORKER_RESET_REQUIRED")?
            .step(request.node, &request.payload)?,
        "append" => cluster
            .as_mut()
            .ok_or("OMNIPAXOS_WORKER_RESET_REQUIRED")?
            .append(request.node, &request.payload)?,
        other => return Err(format!("OMNIPAXOS_WORKER_OPERATION_UNSUPPORTED:{other}").into()),
    }
    let cluster = cluster.as_mut().ok_or("OMNIPAXOS_WORKER_RESET_REQUIRED")?;
    Ok(Response {
        schema_version: SCHEMA,
        id: request.id,
        ok: true,
        error: None,
        configuration: Some(cluster.configuration.clone()),
        nodes: cluster.views()?,
        messages: cluster.take_messages()?,
        decisions: cluster.take_decisions()?,
    })
}

impl Cluster {
    fn new(requested_node_count: u64) -> Result<Self, Box<dyn std::error::Error>> {
        let configuration = normalized_configuration(requested_node_count)?;
        let node_count = configuration.node_count;
        let node_ids: Vec<u64> = (1..=node_count).collect();
        let mut nodes = BTreeMap::new();
        for id in node_ids.iter().copied() {
            let config = OmniPaxosConfig {
                cluster_config: ClusterConfig {
                    configuration_id: 1,
                    nodes: node_ids.clone(),
                    flexible_quorum: None,
                },
                server_config: ServerConfig {
                    pid: id,
                    election_tick_timeout: configuration.election_tick_timeout,
                    resend_message_tick_timeout: configuration.resend_message_tick_timeout,
                    buffer_size: configuration.buffer_size,
                    batch_size: configuration.batch_size,
                    leader_priority: configuration.leader_priorities[(id - 1) as usize],
                },
            };
            nodes.insert(id, config.build(MemoryStorage::default())?);
        }
        Ok(Self {
            nodes,
            reported: BTreeMap::new(),
            prefix_caches: BTreeMap::new(),
            configuration,
        })
    }

    fn tick(&mut self, id: u64) -> Result<(), Box<dyn std::error::Error>> {
        self.nodes
            .get_mut(&id)
            .ok_or("OMNIPAXOS_WORKER_NODE_UNKNOWN")?
            .tick();
        Ok(())
    }

    fn step(&mut self, id: u64, encoded: &str) -> Result<(), Box<dyn std::error::Error>> {
        let bytes = BASE64.decode(encoded)?;
        let message: Message<WorkerEntry> = serde_json::from_slice(&bytes)?;
        if message.get_receiver() != id {
            return Err("OMNIPAXOS_WORKER_MESSAGE_TARGET_MISMATCH".into());
        }
        self.nodes
            .get_mut(&id)
            .ok_or("OMNIPAXOS_WORKER_NODE_UNKNOWN")?
            .handle_incoming(message);
        Ok(())
    }

    fn append(&mut self, id: u64, encoded: &str) -> Result<(), Box<dyn std::error::Error>> {
        let entry: WorkerEntry = serde_json::from_slice(&BASE64.decode(encoded)?)?;
        if entry.request_id.is_empty() || entry.origin != id {
            return Err("OMNIPAXOS_WORKER_APPEND_IDENTITY_INVALID".into());
        }
        self.nodes
            .get_mut(&id)
            .ok_or("OMNIPAXOS_WORKER_NODE_UNKNOWN")?
            .append(entry)
            .map_err(|_| "OMNIPAXOS_WORKER_APPEND_REJECTED")?;
        Ok(())
    }

    fn views(&mut self) -> Result<Vec<NodeView>, Box<dyn std::error::Error>> {
        let nodes = &self.nodes;
        let caches = &mut self.prefix_caches;
        let mut views = Vec::with_capacity(nodes.len());
        for (id, node) in nodes {
            let promise = node.get_promise();
            let cache = caches.entry(*id).or_insert_with(new_decision_prefix_cache);
            let decided_prefixes = decided_prefixes(node, cache)?;
            let decided_prefix_digest = decided_prefixes
                .last()
                .map(|prefix| prefix.digest.clone())
                .unwrap_or_else(empty_decided_prefix_digest);
            views.push(NodeView {
                id: *id,
                leader: node.get_current_leader().unwrap_or(0),
                decided_index: node.get_decided_idx(),
                decided_prefix_digest,
                decided_prefixes,
                promise_number: promise.n,
                promise_priority: promise.priority,
                promise_pid: promise.pid,
            });
        }
        Ok(views)
    }

    fn take_messages(&mut self) -> Result<Vec<MessageView>, Box<dyn std::error::Error>> {
        let mut messages = Vec::new();
        for node in self.nodes.values_mut() {
            for message in node.outgoing_messages() {
                messages.push(message_view(message)?);
            }
        }
        messages.sort_by(|left, right| {
            (&left.from, &left.to, &left.family, &left.bytes).cmp(&(
                &right.from,
                &right.to,
                &right.family,
                &right.bytes,
            ))
        });
        Ok(messages)
    }

    fn take_decisions(&mut self) -> Result<Vec<DecisionView>, Box<dyn std::error::Error>> {
        let mut decisions = Vec::new();
        for (id, node) in &self.nodes {
            let from = *self.reported.get(id).unwrap_or(&0);
            let decided = node.get_decided_idx();
            if decided <= from {
                continue;
            }
            let entries = node
                .read_decided_suffix(from)
                .ok_or("OMNIPAXOS_WORKER_DECIDED_SUFFIX_MISSING")?;
            for (offset, entry) in entries.into_iter().enumerate() {
                if let LogEntry::Decided(value) = entry {
                    decisions.push(DecisionView {
                        node: *id,
                        index: from + offset as u64 + 1,
                        entry: value,
                    });
                }
            }
            self.reported.insert(*id, decided);
        }
        Ok(decisions)
    }
}

fn normalized_configuration(
    requested_node_count: u64,
) -> Result<WorkerConfiguration, Box<dyn std::error::Error>> {
    let node_count = if requested_node_count == 0 {
        DEFAULT_NODE_COUNT
    } else {
        requested_node_count
    };
    if !(DEFAULT_NODE_COUNT..=MAX_NODE_COUNT).contains(&node_count) {
        return Err("OMNIPAXOS_WORKER_NODE_COUNT_INVALID".into());
    }
    Ok(WorkerConfiguration {
        node_count,
        election_tick_timeout: ELECTION_TICK_TIMEOUT,
        resend_message_tick_timeout: RESEND_MESSAGE_TICK_TIMEOUT,
        buffer_size: BUFFER_SIZE,
        batch_size: BATCH_SIZE,
        leader_priorities: (1..=node_count)
            .map(|id| (node_count + 1 - id) as u32)
            .collect(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn configuration_is_normalized_and_bounded() {
        let default = normalized_configuration(0).expect("default configuration");
        assert_eq!(default.node_count, 3);
        assert_eq!(default.leader_priorities, vec![3, 2, 1]);
        let five = normalized_configuration(5).expect("five-node configuration");
        assert_eq!(five.leader_priorities, vec![5, 4, 3, 2, 1]);
        assert!(normalized_configuration(2).is_err());
        assert!(normalized_configuration(MAX_NODE_COUNT + 1).is_err());
    }
}

fn new_decision_prefix_cache() -> DecisionPrefixCache {
    let mut digest = Sha256::new();
    digest.update(b"consensus-atlas/omnipaxos-decided-prefix/v2\0");
    DecisionPrefixCache {
        decided: 0,
        digest,
        prefixes: Vec::new(),
    }
}

fn decided_prefixes(
    node: &Node,
    cache: &mut DecisionPrefixCache,
) -> Result<Vec<DecisionPrefixView>, Box<dyn std::error::Error>> {
    let decided = node.get_decided_idx();
    if decided < cache.decided {
        *cache = new_decision_prefix_cache();
    }
    if decided == cache.decided {
        return Ok(cache.prefixes.clone());
    }
    let entries = node
        .read_decided_suffix(cache.decided)
        .ok_or("OMNIPAXOS_WORKER_DECIDED_PREFIX_MISSING")?;
    if entries.len() as u64 != decided - cache.decided {
        return Err("OMNIPAXOS_WORKER_DECIDED_PREFIX_LENGTH_MISMATCH".into());
    }
    for (offset, entry) in entries.into_iter().enumerate() {
        let LogEntry::Decided(value) = entry else {
            return Err("OMNIPAXOS_WORKER_DECIDED_PREFIX_NOT_EXACT".into());
        };
        digest_field(&mut cache.digest, value.request_id.as_bytes());
        cache.digest.update(value.origin.to_be_bytes());
        digest_field(&mut cache.digest, value.value.as_bytes());
        let index = cache.decided + offset as u64 + 1;
        let mut prefix = cache.digest.clone();
        prefix.update(index.to_be_bytes());
        cache.prefixes.push(DecisionPrefixView {
            index,
            digest: format!("{:x}", prefix.finalize()),
        });
    }
    cache.decided = decided;
    Ok(cache.prefixes.clone())
}

fn empty_decided_prefix_digest() -> String {
    let mut digest = Sha256::new();
    digest.update(b"consensus-atlas/omnipaxos-decided-prefix/v2\0");
    format!("{:x}", digest.finalize())
}

fn digest_field(digest: &mut Sha256, value: &[u8]) {
    digest.update((value.len() as u64).to_be_bytes());
    digest.update(value);
}

fn message_view(message: Message<WorkerEntry>) -> Result<MessageView, Box<dyn std::error::Error>> {
    let (family, type_hint, metadata) = describe_message(&message);
    Ok(MessageView {
        from: message.get_sender(),
        to: message.get_receiver(),
        family,
        type_hint,
        metadata,
        bytes: BASE64.encode(serde_json::to_vec(&message)?),
    })
}

fn describe_message(
    message: &Message<WorkerEntry>,
) -> (&'static str, &'static str, BTreeMap<String, String>) {
    let mut metadata = BTreeMap::new();
    let type_hint = match message {
        Message::BLE(message) => match &message.msg {
            HeartbeatMsg::Request(request) => {
                metadata.insert("heartbeat_round".into(), request.round.to_string());
                "ble/heartbeat-request"
            }
            HeartbeatMsg::Reply(reply) => {
                metadata.insert("heartbeat_round".into(), reply.round.to_string());
                insert_ballot(&mut metadata, reply.ballot);
                "ble/heartbeat-reply"
            }
        },
        Message::SequencePaxos(message) => match &message.msg {
            PaxosMsg::PrepareReq(request) => {
                insert_ballot(&mut metadata, request.n);
                "sequence-paxos/prepare-req"
            }
            PaxosMsg::Prepare(prepare) => {
                insert_ballot(&mut metadata, prepare.n);
                metadata.insert("decided_index".into(), prepare.decided_idx.to_string());
                metadata.insert("accepted_index".into(), prepare.accepted_idx.to_string());
                "sequence-paxos/prepare"
            }
            PaxosMsg::Promise(promise) => {
                insert_ballot(&mut metadata, promise.n);
                metadata.insert("decided_index".into(), promise.decided_idx.to_string());
                metadata.insert("accepted_index".into(), promise.accepted_idx.to_string());
                metadata.insert("entry_count".into(), promise.suffix.len().to_string());
                insert_single_request(&mut metadata, &promise.suffix);
                "sequence-paxos/promise"
            }
            PaxosMsg::AcceptSync(accept) => {
                insert_ballot(&mut metadata, accept.n);
                insert_sequence(&mut metadata, accept.seq_num);
                metadata.insert("decided_index".into(), accept.decided_idx.to_string());
                metadata.insert("sync_index".into(), accept.sync_idx.to_string());
                metadata.insert("entry_count".into(), accept.suffix.len().to_string());
                insert_single_request(&mut metadata, &accept.suffix);
                "sequence-paxos/accept-sync"
            }
            PaxosMsg::AcceptDecide(accept) => {
                insert_ballot(&mut metadata, accept.n);
                insert_sequence(&mut metadata, accept.seq_num);
                metadata.insert("decided_index".into(), accept.decided_idx.to_string());
                metadata.insert("entry_count".into(), accept.entries.len().to_string());
                insert_single_request(&mut metadata, &accept.entries);
                "sequence-paxos/accept-decide"
            }
            PaxosMsg::Accepted(accepted) => {
                insert_ballot(&mut metadata, accepted.n);
                metadata.insert("accepted_index".into(), accepted.accepted_idx.to_string());
                "sequence-paxos/accepted"
            }
            PaxosMsg::NotAccepted(rejected) => {
                insert_ballot(&mut metadata, rejected.n);
                "sequence-paxos/not-accepted"
            }
            PaxosMsg::Decide(decide) => {
                insert_ballot(&mut metadata, decide.n);
                insert_sequence(&mut metadata, decide.seq_num);
                metadata.insert("decided_index".into(), decide.decided_idx.to_string());
                "sequence-paxos/decide"
            }
            PaxosMsg::ProposalForward(entries) => {
                metadata.insert("entry_count".into(), entries.len().to_string());
                insert_single_request(&mut metadata, entries);
                "sequence-paxos/proposal-forward"
            }
            PaxosMsg::Compaction(Compaction::Trim(_)) => "sequence-paxos/compaction-trim",
            PaxosMsg::Compaction(Compaction::Snapshot(_)) => "sequence-paxos/compaction-snapshot",
            PaxosMsg::AcceptStopSign(stop) => {
                insert_ballot(&mut metadata, stop.n);
                insert_sequence(&mut metadata, stop.seq_num);
                "sequence-paxos/accept-stop-sign"
            }
            PaxosMsg::ForwardStopSign(_) => "sequence-paxos/forward-stop-sign",
        },
    };
    let family = if matches!(message, Message::BLE(_)) {
        "ble"
    } else {
        "sequence-paxos"
    };
    (family, type_hint, metadata)
}

fn insert_ballot(metadata: &mut BTreeMap<String, String>, ballot: Ballot) {
    metadata.insert("ballot_config_id".into(), ballot.config_id.to_string());
    metadata.insert("ballot_number".into(), ballot.n.to_string());
    metadata.insert("ballot_priority".into(), ballot.priority.to_string());
    metadata.insert("ballot_pid".into(), ballot.pid.to_string());
}

fn insert_sequence(metadata: &mut BTreeMap<String, String>, sequence: SequenceNumber) {
    metadata.insert("sequence_session".into(), sequence.session.to_string());
    metadata.insert("sequence_counter".into(), sequence.counter.to_string());
}

fn insert_single_request(metadata: &mut BTreeMap<String, String>, entries: &[WorkerEntry]) {
    if let [entry] = entries {
        metadata.insert("request_id".into(), entry.request_id.clone());
    }
}
