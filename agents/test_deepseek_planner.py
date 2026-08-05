from __future__ import annotations

import unittest

import deepseek_planner


class DeepSeekPlannerTest(unittest.TestCase):
    def test_request_is_deterministic_json_output_without_thinking(self) -> None:
        request = {
            "attempt": 1,
            "remaining_budget": {"tokens": 4000},
            "coverage_debt": [{"obligation": {"id": "fault.crash"}}],
        }
        left = deepseek_planner.build_api_request(request, "deepseek-v4-flash")
        right = deepseek_planner.build_api_request(request, "deepseek-v4-flash")
        self.assertEqual(left, right)
        self.assertEqual(left["temperature"], 0)
        self.assertEqual(left["thinking"], {"type": "disabled"})
        self.assertEqual(left["response_format"], {"type": "json_object"})
        self.assertEqual(left["max_tokens"], 4000)
        self.assertNotIn("api_key", str(left).lower())

    def test_completion_token_bound_is_capped(self) -> None:
        request = {"remaining_budget": {"tokens": 100000}}
        payload = deepseek_planner.build_api_request(request, "deepseek-v4-flash")
        self.assertEqual(payload["max_tokens"], 6000)


if __name__ == "__main__":
    unittest.main()
