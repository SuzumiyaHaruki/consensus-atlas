import importlib.util
import json
import pathlib
import sys
import unittest


AGENTS_DIR = pathlib.Path(__file__).parent
sys.path.insert(0, str(AGENTS_DIR))
MODULE_PATH = AGENTS_DIR / "deepseek_contractonly.py"
SPEC = importlib.util.spec_from_file_location("deepseek_contractonly", MODULE_PATH)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class ContractOnlyDeepSeekTest(unittest.TestCase):
    def test_request_is_bounded_structured_generation(self):
        payload = MODULE.build_api_request({"contract_digest": "abc"}, "deepseek-v4-flash")
        encoded = json.dumps(payload)
        self.assertEqual(payload["response_format"], {"type": "json_object"})
        self.assertEqual(payload["thinking"], {"type": "disabled"})
        self.assertEqual(payload["temperature"], 0)
        self.assertEqual(payload["max_tokens"], 16000)
        self.assertIn("integration/*.go", encoded)
        self.assertNotIn("api_key", encoded)
        self.assertNotIn("Authorization", encoded)

    def test_generate_wraps_proposal_and_audit(self):
        proposal = {"version": 1, "id": "fixture", "files": [], "binding": {}}
        response = {
            "id": "response-1",
            "model": "deepseek-v4-flash",
            "choices": [{"finish_reason": "stop", "message": {"content": json.dumps(proposal)}}],
            "usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
        }
        original = MODULE.common.call_deepseek
        MODULE.common.call_deepseek = lambda endpoint, api_key, payload: response
        try:
            result = MODULE.generate(
                {"version": 1, "request": {"contract_digest": "abc"}},
                MODULE.DEFAULT_ENDPOINT,
                MODULE.DEFAULT_MODEL,
                "not-a-real-key",
            )
        finally:
            MODULE.common.call_deepseek = original
        self.assertEqual(result["proposal"], proposal)
        self.assertEqual(result["audit"]["total_tokens"], 30)
        self.assertEqual(len(result["audit"]["prompt_digest"]), 64)

    def test_repair_prompt_prioritizes_mechanical_failure(self):
        payload = MODULE.build_api_request(
            {
                "attempt": 2,
                "contract": {"id": "contract"},
                "driver_api": [{"path": "api.go", "content": "large"}],
                "target": {"sources": [{"path": "rawnode.go", "content": "large"}]},
                "previous_proposal": {"id": "previous"},
                "previous_result": {"status": "validation_invalid", "findings": [{"code": "failure"}]},
            },
            MODULE.DEFAULT_MODEL,
        )
        content = payload["messages"][1]["content"]
        self.assertLess(content.index("repair_instruction"), content.index("driver_api"))
        self.assertLess(content.index("previous_result"), content.index("previous_proposal"))


if __name__ == "__main__":
    unittest.main()
