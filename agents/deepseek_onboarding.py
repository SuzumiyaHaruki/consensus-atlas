#!/usr/bin/env python3
"""Untrusted DeepSeek onboarding proposal generator.

The process accepts one versioned JSON request on stdin and emits one JSON
response on stdout. API credentials are read from a permission-restricted file
and are never included in prompts, responses, exceptions, or audit metadata.
"""

from __future__ import annotations

import json
import hashlib
import os
import stat
import sys
import time
import urllib.error
import urllib.request
from typing import Any


PROTOCOL_VERSION = 1
DEFAULT_ENDPOINT = "https://api.deepseek.com/chat/completions"
DEFAULT_MODEL = "deepseek-v4-flash"

SYSTEM_PROMPT = r"""
You are the ConsensusAtlas Onboarding Agent. Produce one implementation Binding
for the immutable protocol contract and runtime context in the user message.
Your proposal is untrusted and will be checked by a deterministic validator.

Return only one JSON object with exactly this shape (no markdown and no wrapper):
{
  "version": 1,
  "id": "short-stable-id",
  "contract_id": "exact contract.id",
  "contract_digest": "exact request.contract_digest",
  "protocol": "exact contract.protocol",
  "driver": "exact context.driver.driver",
  "capabilities": [
    {"contract_id": "contract capability id", "runtime_id": "same runtime manifest id"}
  ],
  "operations": [
    {"operation_id": "contract operation id", "runtime_kind": "exact contract event_kind"}
  ],
  "witnesses": [
    {
      "id": "short-stable-id",
      "scenario": "an exact context.scenarios[].path",
      "covers": ["contract obligation id"],
      "validates_capabilities": ["contract capability id"]
    }
  ]
}

Rules:
- Never alter, reinterpret, add, or omit contract capabilities and operations.
- Capability IDs use one shared vocabulary: contract_id must equal runtime_id.
- Map capabilities even when the runtime manifest marks them unsupported.
- Select witness paths only from context.scenarios; you cannot create files.
- Every runtime-supported contract capability must be claimed by witnesses whose
  scenarios collectively exercise its contract witness_labels.
- Every obligation whose required capabilities are runtime-supported must occur
  in covers at least once. Do not claim unsupported obligations.
- A witness may omit covers or validates_capabilities, but not both.
- Use previous_binding, previous_findings, and previous_report.witnesses to
  repair the prior proposal. Match missing capability evidence labels to the
  observed_labels of scenarios that actually produced them.
- Do not add explanations, confidence, status, assertions, or unknown fields.
- The output must be valid JSON.
""".strip()


def read_api_key(path: str) -> str:
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(path, flags)
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode):
            raise ValueError("API key path must be a regular file")
        if info.st_mode & 0o077:
            raise ValueError("API key file must not be accessible by group or others")
        raw_key = os.read(descriptor, 4097)
    finally:
        os.close(descriptor)
    if len(raw_key) > 4096:
        raise ValueError("API key file is too large")
    key = raw_key.decode("utf-8").strip()
    if not key or any(character.isspace() for character in key):
        raise ValueError("API key file is empty or malformed")
    return key


def build_api_request(request_payload: dict[str, Any], model: str) -> dict[str, Any]:
    return {
        "model": model,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {
                "role": "user",
                "content": "Generate the JSON Binding from this deterministic input:\n"
                + json.dumps(request_payload, ensure_ascii=False, separators=(",", ":")),
            },
        ],
        "response_format": {"type": "json_object"},
        "thinking": {"type": "disabled"},
        "temperature": 0,
        "max_tokens": 8000,
        "stream": False,
    }


def parse_completion(response: dict[str, Any]) -> tuple[dict[str, Any], dict[str, Any]]:
    choices = response.get("choices")
    if not isinstance(choices, list) or len(choices) != 1:
        raise ValueError("DeepSeek response must contain exactly one choice")
    choice = choices[0]
    finish_reason = choice.get("finish_reason")
    if finish_reason != "stop":
        raise ValueError(f"DeepSeek completion did not stop normally: {finish_reason!r}")
    message = choice.get("message")
    content = message.get("content") if isinstance(message, dict) else None
    if not isinstance(content, str) or not content.strip():
        raise ValueError("DeepSeek returned empty JSON content")
    binding = json.loads(content, object_pairs_hook=_unique_object)
    if not isinstance(binding, dict):
        raise ValueError("DeepSeek JSON content is not an object")
    usage = response.get("usage") or {}
    completion_details = usage.get("completion_tokens_details") or {}
    audit = {
        "provider": "deepseek",
        "model": str(response.get("model") or DEFAULT_MODEL),
        "response_id": str(response.get("id") or ""),
        "system_fingerprint": str(response.get("system_fingerprint") or ""),
        "finish_reason": str(finish_reason),
        "prompt_tokens": int(usage.get("prompt_tokens") or 0),
        "prompt_cache_hit_tokens": int(usage.get("prompt_cache_hit_tokens") or 0),
        "prompt_cache_miss_tokens": int(usage.get("prompt_cache_miss_tokens") or 0),
        "completion_tokens": int(usage.get("completion_tokens") or 0),
        "reasoning_tokens": int(completion_details.get("reasoning_tokens") or 0),
        "total_tokens": int(usage.get("total_tokens") or 0),
    }
    return binding, audit


def _unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def validate_endpoint(endpoint: str) -> None:
    allowed = {
        "https://api.deepseek.com/chat/completions",
        "https://api.deepseek.com/v1/chat/completions",
    }
    if endpoint not in allowed:
        raise ValueError("DeepSeek endpoint must be an official Chat Completions URL")


def call_deepseek(endpoint: str, api_key: str, payload: dict[str, Any]) -> dict[str, Any]:
    encoded = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    request = urllib.request.Request(
        endpoint,
        data=encoded,
        method="POST",
        headers={
            "Authorization": "Bearer " + api_key,
            "Content-Type": "application/json",
            "Accept": "application/json",
            "User-Agent": "ConsensusAtlas-Onboarding/1",
        },
    )
    with urllib.request.urlopen(request, timeout=180) as response:
        body = response.read(16 << 20)
    parsed = json.loads(body)
    if not isinstance(parsed, dict):
        raise ValueError("DeepSeek API response is not a JSON object")
    return parsed


def generate(envelope: dict[str, Any], endpoint: str, model: str, api_key: str) -> dict[str, Any]:
    if envelope.get("version") != PROTOCOL_VERSION or not isinstance(envelope.get("request"), dict):
        raise ValueError("unsupported command protocol request")
    validate_endpoint(endpoint)
    payload = build_api_request(envelope["request"], model)
    prompt_digest = hashlib.sha256(
        json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    ).hexdigest()
    started = time.monotonic()
    last_error: Exception | None = None
    for attempt in range(3):
        try:
            response = call_deepseek(endpoint, api_key, payload)
            binding, audit = parse_completion(response)
            audit["endpoint"] = endpoint
            audit["prompt_digest"] = prompt_digest
            audit["thinking_mode"] = payload["thinking"]["type"]
            audit["temperature"] = payload["temperature"]
            audit["max_tokens"] = payload["max_tokens"]
            audit["duration_millis"] = int((time.monotonic() - started) * 1000)
            return {"version": PROTOCOL_VERSION, "binding": binding, "audit": audit}
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
        f"DeepSeek request failed after bounded retries: {type(last_error).__name__}: {detail}"
    )


def main() -> int:
    try:
        envelope = json.load(sys.stdin)
        key_path = os.environ.get("CONSENSUS_ATLAS_DEEPSEEK_KEY_FILE", "")
        if not key_path:
            raise ValueError("CONSENSUS_ATLAS_DEEPSEEK_KEY_FILE is required")
        endpoint = os.environ.get("CONSENSUS_ATLAS_DEEPSEEK_ENDPOINT", DEFAULT_ENDPOINT)
        model = os.environ.get("CONSENSUS_ATLAS_DEEPSEEK_MODEL", DEFAULT_MODEL)
        api_key = read_api_key(key_path)
        result = generate(envelope, endpoint, model, api_key)
        json.dump(result, sys.stdout, ensure_ascii=False, separators=(",", ":"))
        sys.stdout.write("\n")
        return 0
    except Exception as error:  # Keep stderr credential-free.
        print(f"deepseek-onboarding: {type(error).__name__}: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
