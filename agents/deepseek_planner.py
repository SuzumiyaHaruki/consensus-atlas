#!/usr/bin/env python3
"""Untrusted DeepSeek Test Plan proposal generator."""

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
You are the ConsensusAtlas Planner Agent. Propose exactly one bounded Test Plan
that targets current Coverage Debt. Your proposal is untrusted: deterministic
Go code validates it, concretizes it, executes official consensus code through
Random/DFS, forces replay, runs Oracles, and alone updates the Coverage Ledger.

Return only one JSON object with exactly this shape, without markdown:
{
  "version": 1,
  "id": "proposal-a1",
  "plan": {
    "id": "agent-plan-a1",
    "targets": ["exact current coverage_debt obligation id"],
    "prepare": [
      {"op": "inject", "kind": "campaign", "target": "n1"},
      {"op": "execute", "match": {"kind": "campaign", "target": "n1"}}
    ],
    "stimuli": [
      {"kind": "propose", "target": "n1", "payload": {"value": "agent-a1"}}
    ],
    "search": {
      "strategy": "random",
      "config": {
        "max_runs": 4,
        "budget_per_run": 64,
        "decision_budget": 128,
        "seed": 101,
        "actions": {
          "drop_messages": false,
          "duplicate_messages": false,
          "max_duplicates_per_run": 0
        }
      }
    }
  }
}

Hard rules:
- Select only exact IDs currently present in coverage_debt. Prefer critical or
  high-risk debt with few attempts, and no more than six coherent targets.
- Use a new lowercase proposal id and plan id on every attempt. Do not evade
  duplicate detection by merely renaming an unchanged plan.
- Obey remaining_budget. decision_budget must be positive, no greater than
  max_plan_decisions, and no greater than max_runs * budget_per_run. max_runs
  must not exceed max_plan_runs. Use modest budgets so later repair attempts
  retain resources.
- Runtime automatically starts all profile nodes and drains bootstrap Ready
  work before prepare. Never inject start.
- Allowed protocol inputs are campaign, propose, timeout, crash, restart.
  Message, persist, sync, emit, apply and acknowledge may be selected for
  execute/drop/duplicate only after the real implementation produced them;
  never inject them.
- prepare is deterministic and runs before search. `inject` schedules an input;
  `execute` requires a stable match with kind and optional source/target/
  type_hint; `drop` and `duplicate` select only message; `drain` requires count;
  partition requires complete disjoint groups containing all nodes; heal has no
  arguments. Event IDs and host batch groups are forbidden selectors.
- stimuli must contain at least one protocol input and are left pending for
  Random/DFS. Proposal payload must be JSON and contain a non-empty value for
  etcd/raft. A proposal becomes enabled only when its target is leader.
- To expose Raft Ready host work, inject and execute campaign/proposal/message;
  then real persist/sync/emit/apply/acknowledge events become selectable. Do not
  assume every Ready contains every operation: over-specific prepare selectors
  can fail mechanically.
- Random/DFS may drop or duplicate only when the corresponding action flag is
  enabled. Set max_duplicates_per_run positive iff duplicate_messages is true.
- Read previous_proposals and previous_findings. Repair exact validation or
  execution failures. If a valid plan made no progress, change the causal setup
  or targeted debt, not just ids, seed, or budget.
- A target is intent, never evidence. Do not output score, covered status,
  Oracle conclusions, explanations, confidence, or unknown fields.
""".strip()


def build_api_request(request_payload: dict[str, Any], model: str) -> dict[str, Any]:
    remaining = request_payload.get("remaining_budget") or {}
    remaining_tokens = int(remaining.get("tokens") or 6000)
    max_tokens = min(6000, max(256, remaining_tokens))
    return {
        "model": model,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {
                "role": "user",
                "content": "Generate one JSON Test Plan proposal from this immutable input:\n"
                + json.dumps(request_payload, ensure_ascii=False, separators=(",", ":")),
            },
        ],
        "response_format": {"type": "json_object"},
        "thinking": {"type": "disabled"},
        "temperature": 0,
        "max_tokens": max_tokens,
        "stream": False,
    }


def generate(envelope: dict[str, Any], endpoint: str, model: str, api_key: str) -> dict[str, Any]:
    if envelope.get("version") != PROTOCOL_VERSION or not isinstance(envelope.get("request"), dict):
        raise ValueError("unsupported Planner command protocol request")
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
        f"DeepSeek Planner request failed after bounded retries: {type(last_error).__name__}: {detail}"
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
        print(f"deepseek-planner: {type(error).__name__}: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
