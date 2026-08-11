use base64::engine::general_purpose::STANDARD as BASE64;
use base64::Engine;
use protobuf::Message as ProtobufMessage;
use raft::prelude::*;
use raft::storage::MemStorage;
use raft::{Config, RawNode};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use slog::{o, Drain, Logger};
use std::collections::BTreeMap;
use std::io::{self, BufRead, Write};

const SCHEMA: &str = "consensus-atlas/raft-rs-worker/v1";

struct Node {
    raw: RawNode<MemStorage>,
    applied: u64,
    pending: Option<Ready>,
}

struct Cluster {
    nodes: BTreeMap<u64, Node>,
}

#[derive(Deserialize)]
struct Request {
    id: u64,
    op: String,
    #[serde(default)]
    node: u64,
    #[serde(default)]
    ready_id: u64,
    #[serde(default)]
    message: String,
    #[serde(default)]
    data: String,
}

#[derive(Serialize)]
struct Response {
    schema_version: &'static str,
    id: u64,
    ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
    nodes: Vec<NodeView>,
    messages: Vec<MessageView>,
    commits: Vec<CommitView>,
}

#[derive(Serialize)]
struct NodeView {
    id: u64,
    role: String,
    term: u64,
    vote: u64,
    lead: u64,
    commit: u64,
    applied: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    ready: Option<ReadyView>,
}

#[derive(Clone, Serialize)]
struct ReadyView {
    ready_id: u64,
    digest: String,
}

#[derive(Serialize)]
struct MessageView {
    from: u64,
    to: u64,
    type_hint: String,
    term: u64,
    index: u64,
    commit: u64,
    bytes: String,
}

#[derive(Serialize)]
struct CommitView {
    node: u64,
    index: u64,
    term: u64,
    data: String,
}

#[derive(Default)]
struct ReadyOutput {
    messages: Vec<MessageView>,
    commits: Vec<CommitView>,
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let stdin = io::stdin();
    let mut stdout = io::BufWriter::new(io::stdout().lock());
    let mut cluster: Option<Cluster> = None;
    for line in stdin.lock().lines() {
        let line = line?;
        let request: Request = serde_json::from_str(&line)?;
        let id = request.id;
        let result = handle(&mut cluster, request);
        let response = match result {
            Ok(response) => response,
            Err(error) => Response {
                schema_version: SCHEMA,
                id,
                ok: false,
                error: Some(error.to_string()),
                nodes: Vec::new(),
                messages: Vec::new(),
                commits: Vec::new(),
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
    let output = match request.op.as_str() {
        "reset" => {
            *cluster = Some(Cluster::new()?);
            ReadyOutput::default()
        }
        "tick" => {
            let cluster = cluster.as_mut().ok_or("RAFT_RS_WORKER_RESET_REQUIRED")?;
            cluster.tick(request.node)?;
            ReadyOutput::default()
        }
        "step" => {
            let cluster = cluster.as_mut().ok_or("RAFT_RS_WORKER_RESET_REQUIRED")?;
            cluster.step(request.node, &request.message)?;
            ReadyOutput::default()
        }
        "propose" => {
            let cluster = cluster.as_mut().ok_or("RAFT_RS_WORKER_RESET_REQUIRED")?;
            cluster.propose(request.node, &request.data)?;
            ReadyOutput::default()
        }
        "complete-ready" => {
            let cluster = cluster.as_mut().ok_or("RAFT_RS_WORKER_RESET_REQUIRED")?;
            cluster.complete_ready(request.node, request.ready_id)?
        }
        other => return Err(format!("RAFT_RS_WORKER_OPERATION_UNSUPPORTED:{other}").into()),
    };
    let cluster = cluster.as_mut().ok_or("RAFT_RS_WORKER_RESET_REQUIRED")?;
    cluster.capture_all();
    Ok(Response {
        schema_version: SCHEMA,
        id: request.id,
        ok: true,
        error: None,
        nodes: cluster.views()?,
        messages: output.messages,
        commits: output.commits,
    })
}

impl Cluster {
    fn new() -> Result<Self, Box<dyn std::error::Error>> {
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
                    pending: None,
                },
            );
        }
        Ok(Self { nodes })
    }

    fn tick(&mut self, id: u64) -> Result<(), Box<dyn std::error::Error>> {
        let node = self
            .nodes
            .get_mut(&id)
            .ok_or("RAFT_RS_WORKER_NODE_UNKNOWN")?;
        if node.pending.is_some() {
            return Err("RAFT_RS_WORKER_READY_PENDING".into());
        }
        node.raw.tick();
        Ok(())
    }

    fn step(&mut self, id: u64, encoded: &str) -> Result<(), Box<dyn std::error::Error>> {
        let node = self
            .nodes
            .get_mut(&id)
            .ok_or("RAFT_RS_WORKER_NODE_UNKNOWN")?;
        if node.pending.is_some() {
            return Err("RAFT_RS_WORKER_READY_PENDING".into());
        }
        let bytes = BASE64.decode(encoded)?;
        let message = Message::parse_from_bytes(&bytes)?;
        if message.to != id {
            return Err("RAFT_RS_WORKER_MESSAGE_TARGET_MISMATCH".into());
        }
        node.raw.step(message)?;
        Ok(())
    }

    fn propose(&mut self, id: u64, encoded: &str) -> Result<(), Box<dyn std::error::Error>> {
        let node = self
            .nodes
            .get_mut(&id)
            .ok_or("RAFT_RS_WORKER_NODE_UNKNOWN")?;
        if node.pending.is_some() {
            return Err("RAFT_RS_WORKER_READY_PENDING".into());
        }
        node.raw.propose(Vec::new(), BASE64.decode(encoded)?)?;
        Ok(())
    }

    fn complete_ready(
        &mut self,
        id: u64,
        ready_id: u64,
    ) -> Result<ReadyOutput, Box<dyn std::error::Error>> {
        let node = self
            .nodes
            .get_mut(&id)
            .ok_or("RAFT_RS_WORKER_NODE_UNKNOWN")?;
        let mut ready = node.pending.take().ok_or("RAFT_RS_WORKER_READY_REQUIRED")?;
        if ready.number() != ready_id {
            node.pending = Some(ready);
            return Err("RAFT_RS_WORKER_READY_ID_MISMATCH".into());
        }
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
        let mut released = ready.take_messages();
        released.extend(ready.take_persisted_messages());
        let mut commits = Vec::new();
        apply_committed(
            id,
            ready.take_committed_entries(),
            &mut node.applied,
            &mut commits,
        );
        let mut light = node.raw.advance(ready);
        released.extend(light.take_messages());
        apply_committed(
            id,
            light.take_committed_entries(),
            &mut node.applied,
            &mut commits,
        );
        if node.applied > 0 {
            node.raw.advance_apply_to(node.applied);
        }
        released.sort_by_key(message_key);
        Ok(ReadyOutput {
            messages: released
                .into_iter()
                .map(message_view)
                .collect::<Result<_, _>>()?,
            commits,
        })
    }

    fn capture_all(&mut self) {
        for node in self.nodes.values_mut() {
            if node.pending.is_none() && node.raw.has_ready() {
                node.pending = Some(node.raw.ready());
            }
        }
    }

    fn views(&self) -> Result<Vec<NodeView>, Box<dyn std::error::Error>> {
        self.nodes
            .iter()
            .map(|(id, node)| {
                let status = node.raw.status();
                Ok(NodeView {
                    id: *id,
                    role: format!("{:?}", status.ss.raft_state),
                    term: status.hs.term,
                    vote: status.hs.vote,
                    lead: status.ss.leader_id,
                    commit: status.hs.commit,
                    applied: node.applied,
                    ready: node.pending.as_ref().map(ready_view).transpose()?,
                })
            })
            .collect()
    }
}

fn ready_view(ready: &Ready) -> Result<ReadyView, Box<dyn std::error::Error>> {
    let mut digest = Sha256::new();
    digest.update(ready.number().to_be_bytes());
    if let Some(hard_state) = ready.hs() {
        digest.update(hard_state.write_to_bytes()?);
    }
    for entry in ready.entries() {
        digest.update(entry.write_to_bytes()?);
    }
    if !ready.snapshot().is_empty() {
        digest.update(ready.snapshot().write_to_bytes()?);
    }
    for entry in ready.committed_entries() {
        digest.update(entry.write_to_bytes()?);
    }
    for message in ready.messages().iter().chain(ready.persisted_messages()) {
        digest.update(message.write_to_bytes()?);
    }
    Ok(ReadyView {
        ready_id: ready.number(),
        digest: format!("{:x}", digest.finalize()),
    })
}

fn message_view(message: Message) -> Result<MessageView, Box<dyn std::error::Error>> {
    Ok(MessageView {
        from: message.from,
        to: message.to,
        type_hint: format!("{:?}", message.get_msg_type()),
        term: message.term,
        index: message.index,
        commit: message.commit,
        bytes: BASE64.encode(message.write_to_bytes()?),
    })
}

fn apply_committed(id: u64, entries: Vec<Entry>, applied: &mut u64, commits: &mut Vec<CommitView>) {
    for entry in entries {
        *applied = (*applied).max(entry.index);
        if !entry.data.is_empty() {
            commits.push(CommitView {
                node: id,
                index: entry.index,
                term: entry.term,
                data: BASE64.encode(entry.data),
            });
        }
    }
}

fn message_key(message: &Message) -> (u64, u64, i32, u64, u64, u64) {
    (
        message.from,
        message.to,
        message.get_msg_type() as i32,
        message.term,
        message.index,
        message.commit,
    )
}
