from __future__ import annotations

import json
import unittest

import deepseek_control_planner as planner


class DeepSeekControlPlannerTest(unittest.TestCase):
    def test_request_is_bounded_and_deterministic(self) -> None:
        request = {
            "scope": {"digest": "abc", "runs": [1, 2], "decisions_per_run": 32, "require_replay": True},
            "target": {"description": "three-node consensus cluster", "nodes": ["n1", "n2", "n3"]},
            "action_kinds": ["crash", "restart", "deliver-message"],
        }
        left = planner.build_api_request(request, planner.DEFAULT_MODEL)
        right = planner.build_api_request(request, planner.DEFAULT_MODEL)
        self.assertEqual(left, right)
        self.assertEqual(left["temperature"], 0)
        self.assertEqual(left["thinking"], {"type": "disabled"})
        self.assertEqual(left["max_tokens"], 1800)
        encoded = json.dumps(left)
        self.assertNotIn("api_key", encoded.lower())
        self.assertNotIn("seed_hex\":\"6f66", encoded.lower())
        self.assertNotIn("pss_id", encoded.lower())

    def test_generate_makes_one_call_and_records_usage(self) -> None:
        calls = 0

        def fake_call(endpoint, api_key, payload):
            nonlocal calls
            calls += 1
            return {
                "id": "response-1",
                "model": "deepseek-v4-flash",
                "choices": [{"finish_reason": "stop", "message": {"content": json.dumps({
                    "schema_version": "consensus-atlas/planner-proposal/v1",
                    "policies": [{"run": 1, "seed_hex": "01"}],
                })}}],
                "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
            }

        original = planner.common.call_deepseek
        planner.common.call_deepseek = fake_call
        try:
            result = planner.generate(
                {"version": 1, "request": {"scope": {"runs": [1]}}},
                planner.DEFAULT_ENDPOINT,
                planner.DEFAULT_MODEL,
                "test-only-key",
            )
        finally:
            planner.common.call_deepseek = original
        self.assertEqual(calls, 1)
        self.assertEqual(result["audit"]["requested_model"], planner.DEFAULT_MODEL)
        self.assertEqual(result["audit"]["response_model"], planner.DEFAULT_MODEL)
        self.assertEqual(result["audit"]["total_tokens"], 15)
        self.assertEqual(len(result["audit"]["prompt_digest"]), 64)


if __name__ == "__main__":
    unittest.main()
