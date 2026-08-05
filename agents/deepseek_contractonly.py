#!/usr/bin/env python3
"""DeepSeek generator for isolated Contract-only integration proposals."""

from __future__ import annotations

import hashlib
import json
import os
import sys
import time
import urllib.error
from typing import Any

import deepseek_onboarding as common


PROTOCOL_VERSION = 1
DEFAULT_MODEL = "deepseek-v4-flash"
DEFAULT_ENDPOINT = "https://api.deepseek.com/chat/completions"

SYSTEM_PROMPT = r"""
You are the ConsensusAtlas Contract-only Onboarding Agent. You receive an
immutable protocol Contract, public generic Driver APIs, selected official SUT
source files, and (after attempt 1) the complete previous proposal and exact
mechanical build/validation result. You do not have a reference Driver,
Binding, witness, Profile, or expected answer.

Return only one JSON object with exactly this shape:
{
  "version": 1,
  "id": "stable-proposal-id",
  "files": [
    {"path": "integration/driver.go", "content": "complete Go source"},
    {"path": "scenarios/witness.json", "content": "complete scenario JSON"}
  ],
  "binding": {
    "version": 1,
    "id": "stable-binding-id",
    "contract_id": "exact contract.id",
    "contract_digest": "exact request.contract_digest",
    "protocol": "exact contract.protocol",
    "driver": "exact generated Manifest.driver",
    "capabilities": [
      {"contract_id": "every contract capability id", "runtime_id": "same id"}
    ],
    "operations": [
      {"operation_id": "every contract operation id", "runtime_kind": "exact event_kind"}
    ],
    "witnesses": [
      {
        "id": "stable-id",
        "scenario": "scenarios/exact-generated-file.json",
        "covers": ["supported obligation ids"],
        "validates_capabilities": ["supported capability ids"]
      }
    ]
  }
}

Generated-code contract:
- All files are a complete snapshot on every attempt, not a patch.
- Paths are restricted to integration/*.go and scenarios/*.json. At most 20
  files and 512 KiB total. Go package name must be integration.
- Export exactly:
    func New(coverage.Profile) (adapter.Adapter, driver.Manifest, error)
- New must construct a driver.ProtocolDriver and wrap it with host.New.
- The integration must call the official go.etcd.io/raft/v3 v3.6.0 APIs; do not
  reimplement Raft and do not import a pre-existing ConsensusAtlas Driver.
- Honor contract.initial_membership. For this fresh etcd/raft cluster, create
  the declared initial voters with RawNode.Bootstrap before the first Poll so
  their configuration entries enter the normal Ready persistence pipeline.
- New must return native nodes already running and bootstrapped. The generic
  Host consumes a scenario `start` event itself and responds by calling Poll;
  it never passes start to Driver.Invoke. Therefore the first Poll after start
  must be able to expose the bootstrap Ready.
- The generated Manifest must list every Contract capability exactly once.
  Mark a capability unsupported when the official API cannot faithfully expose
  it; never pretend a label exists.
- Allowed imports are only standard bytes/context/crypto-sha256/encoding
  base64-hex-json/errors/fmt/io/math/sort/strconv/strings/sync, the supplied
  ConsensusAtlas adapter/core/coverage/driver/host packages, go.etcd.io/raft/v3,
  and go.etcd.io/raft/v3/raftpb. No init/build/generate/embed/linkname directive.
- Each native Ready becomes one immutable OutputBatch. Persist updates visible
  storage, sync freezes a separately recoverable durable image, emit releases a
  complete serialized raftpb.Message only after its declared storage barrier,
  apply records committed application entries, and exactly one acknowledge
  transitively depends on every operation before calling RawNode.Advance.
- Invoke only supplies input to RawNode. It must not consume Ready. Poll alone
  calls RawNode.Ready, freezes that exact Ready and stores both the exact batch
  and Ready as outstanding before returning. ExecuteHostOp must recognize that
  batch token, and acknowledge calls Advance with the stored Ready before
  clearing it. A repeated Host poll is not a second Ready.
- Crash must discard RawNode, visible unsynchronized state, and outstanding
  Ready. Restart must reconstruct only from the synchronized durable image.
- Snapshot/Nodes/Protocol/Capabilities/CheckConformance must be deterministic.
- Snapshot must satisfy the supplied `families/raft/state.go` Raft Family PSS
  evidence schema: host.Adapter provides the outer `driver` field and the
  generated Driver snapshot must provide `nodes` with every required node,
  running/role/term/vote/lead/commit/applied, durable frontier/log, and voters.
  The hidden evaluator projects this schema initially and after every event.
- Use raftpb.Message Marshal/Unmarshal for wire payload and SHA-256 for payload
  digests. Node IDs in the Contract must map deterministically to uint64 IDs.
- Scenarios use only the supplied scenario operations. They must start all
  nodes and explicitly drive enough events to produce the Contract labels they
  claim. Keep selectors strict; do not assert labels inside scenario JSON.
- Never inject persist/sync/emit/apply/acknowledge directly in a scenario;
  those are scheduled automatically from Driver OutputBatch operations. Use
  bounded `run`, strict message selectors, and the scenario fault operations.
- Binding cannot alter the Contract, add assertions, or reference ungenerated
  files. Map every capability and operation exactly once. Do not claim
  unsupported obligations/capabilities in witnesses.
- On retries, preserve working code and repair every actionable compiler or
  validator finding from previous_result. The validator, not you, decides
  support and coverage.
- Output valid JSON only, without markdown or explanations.
""".strip()


def build_api_request(request_payload: dict[str, Any], model: str) -> dict[str, Any]:
    if request_payload.get("attempt", 1) > 1:
        ordered_input = {
            "repair_instruction": (
                "This is a repair attempt. Change the previous files to fix the primary "
                "actionable failure and failure_event below. Do not merely rename ids."
            ),
            "attempt": request_payload.get("attempt"),
            "previous_result": request_payload.get("previous_result"),
            "previous_proposal": request_payload.get("previous_proposal"),
            "contract_digest": request_payload.get("contract_digest"),
            "contract": request_payload.get("contract"),
            "driver_api": request_payload.get("driver_api"),
            "target": request_payload.get("target"),
        }
    else:
        ordered_input = request_payload
    return {
        "model": model,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {
                "role": "user",
                "content": "Generate the complete Contract-only proposal from this input:\n"
                + json.dumps(ordered_input, ensure_ascii=False, separators=(",", ":")),
            },
        ],
        "response_format": {"type": "json_object"},
        "thinking": {"type": "disabled"},
        "temperature": 0,
        "max_tokens": 16000,
        "stream": False,
    }


def generate(envelope: dict[str, Any], endpoint: str, model: str, api_key: str) -> dict[str, Any]:
    if envelope.get("version") != PROTOCOL_VERSION or not isinstance(envelope.get("request"), dict):
        raise ValueError("unsupported Contract-only command protocol request")
    common.validate_endpoint(endpoint)
    payload = build_api_request(envelope["request"], model)
    prompt_digest = hashlib.sha256(
        json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    ).hexdigest()
    started = time.monotonic()
    last_error: Exception | None = None
    for attempt in range(3):
        try:
            response = common.call_deepseek(endpoint, api_key, payload)
            proposal, audit = common.parse_completion(response)
            audit["endpoint"] = endpoint
            audit["prompt_digest"] = prompt_digest
            audit["thinking_mode"] = payload["thinking"]["type"]
            audit["temperature"] = payload["temperature"]
            audit["max_tokens"] = payload["max_tokens"]
            audit["duration_millis"] = int((time.monotonic() - started) * 1000)
            return {"version": PROTOCOL_VERSION, "proposal": proposal, "audit": audit}
        except urllib.error.HTTPError as error:
            last_error = error
            if error.code not in (408, 429, 500, 502, 503, 504):
                break
        except (urllib.error.URLError, TimeoutError, ValueError, json.JSONDecodeError) as error:
            last_error = error
        if attempt < 2:
            time.sleep(1 << attempt)
    if isinstance(last_error, urllib.error.HTTPError):
        detail = f"HTTP status {last_error.code}"
    else:
        detail = str(last_error)[:500]
    raise RuntimeError(
        f"DeepSeek Contract-only request failed after bounded retries: "
        f"{type(last_error).__name__}: {detail}"
    )


def main() -> int:
    try:
        envelope = json.load(sys.stdin)
        key_path = os.environ.get("CONSENSUS_ATLAS_DEEPSEEK_KEY_FILE", "")
        if not key_path:
            raise ValueError("CONSENSUS_ATLAS_DEEPSEEK_KEY_FILE is required")
        endpoint = os.environ.get("CONSENSUS_ATLAS_DEEPSEEK_ENDPOINT", DEFAULT_ENDPOINT)
        model = os.environ.get("CONSENSUS_ATLAS_DEEPSEEK_MODEL", DEFAULT_MODEL)
        api_key = common.read_api_key(key_path)
        result = generate(envelope, endpoint, model, api_key)
        json.dump(result, sys.stdout, ensure_ascii=False, separators=(",", ":"))
        sys.stdout.write("\n")
        return 0
    except Exception as error:
        print(f"deepseek-contract-only: {type(error).__name__}: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
