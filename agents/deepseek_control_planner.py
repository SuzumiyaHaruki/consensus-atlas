#!/usr/bin/env python3
"""One-call untrusted Planner for Control Runtime v2 policies."""

from __future__ import annotations

import hashlib
import json
import os
import sys
import time
from typing import Any

import deepseek_onboarding as common


PROTOCOL_VERSION = 1
DEFAULT_MODEL = "deepseek-v4-flash"
DEFAULT_ENDPOINT = "https://api.deepseek.com/chat/completions"
MAX_TOKENS = 1800

SYSTEM_PROMPT = r"""
You are a restricted ConsensusAtlas scheduling Planner. Produce one bounded
proposal for a deterministic consensus-system simulation. Trusted code owns the
Runtime, PSS mapping, replay, budgets, and evaluation. You only choose policies
over the public Action kinds and node IDs in the immutable request.

Return only one JSON object, without markdown or explanations:
{
  "schema_version": "consensus-atlas/planner-proposal/v1",
  "policies": [
    {
      "run": 1,
      "rules": [{"decision": 1, "kind": "crash", "node": "n3"}],
      "priority": ["complete-effect", "deliver-message", "fire-temporal-event"]
    }
  ]
}

Hard rules:
- Output exactly one policy for every request.scope.runs entry, with no extra
  run and no unknown field. Use the exact run numbers.
- A fixed policy has a non-empty unique priority list and optional rules. A
  random policy has only run plus a non-empty hexadecimal seed_hex; it has no
  priority or rules. Do not mix the two forms.
- Use only request.action_kinds. A rule uses a one-based decision no greater
  than decisions_per_run and an optional node from request.target.nodes.
- Rules are attempted only at their exact decision. If a rule refers to crash
  and restart, crash must precede restart for the same node.
- A fixed policy should retain progress fallbacks such as complete-effect,
  deliver-message, and fire-temporal-event when those kinds are allowed.
- Prefer different but coherent policies across runs to explore different
  schedules. Do not output Runtime seeds, PSS fields, scores, Oracle claims,
  coverage claims, confidence, prose, or protocol-specific message contents.
""".strip()


def build_api_request(request_payload: dict[str, Any], model: str) -> dict[str, Any]:
    return {
        "model": model,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {
                "role": "user",
                "content": "Generate one policy proposal from this immutable input:\n"
                + json.dumps(request_payload, ensure_ascii=False, separators=(",", ":")),
            },
        ],
        "response_format": {"type": "json_object"},
        "thinking": {"type": "disabled"},
        "temperature": 0,
        "max_tokens": MAX_TOKENS,
        "stream": False,
    }


def generate(envelope: dict[str, Any], endpoint: str, model: str, api_key: str) -> dict[str, Any]:
    if envelope.get("version") != PROTOCOL_VERSION or not isinstance(envelope.get("request"), dict):
        raise ValueError("unsupported Control Planner command request")
    common.validate_endpoint(endpoint)
    payload = build_api_request(envelope["request"], model)
    prompt_digest = hashlib.sha256(
        json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    ).hexdigest()
    started = time.monotonic()
    response = common.call_deepseek(endpoint, api_key, payload)
    proposal, audit = common.parse_completion(response)
    audit["requested_model"] = model
    audit["response_model"] = audit.pop("model")
    audit["endpoint"] = endpoint
    audit["prompt_digest"] = prompt_digest
    audit["thinking_mode"] = payload["thinking"]["type"]
    audit["temperature"] = payload["temperature"]
    audit["max_tokens"] = payload["max_tokens"]
    audit["duration_millis"] = int((time.monotonic() - started) * 1000)
    return {"version": PROTOCOL_VERSION, "proposal": proposal, "audit": audit}


def main() -> int:
    try:
        envelope = json.load(sys.stdin)
        key_path = os.environ.get("CONSENSUS_ATLAS_DEEPSEEK_KEY_FILE", "")
        if not key_path:
            raise ValueError("CONSENSUS_ATLAS_DEEPSEEK_KEY_FILE is required")
        endpoint = os.environ.get("CONSENSUS_ATLAS_DEEPSEEK_ENDPOINT", DEFAULT_ENDPOINT)
        model = os.environ.get("CONSENSUS_ATLAS_DEEPSEEK_MODEL", DEFAULT_MODEL)
        result = generate(envelope, endpoint, model, common.read_api_key(key_path))
        json.dump(result, sys.stdout, ensure_ascii=False, separators=(",", ":"))
        sys.stdout.write("\n")
        return 0
    except Exception as error:
        print(f"deepseek-control-planner: {type(error).__name__}: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
