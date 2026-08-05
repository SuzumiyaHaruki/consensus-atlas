import importlib.util
import json
import os
import pathlib
import tempfile
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("deepseek_onboarding.py")
SPEC = importlib.util.spec_from_file_location("deepseek_onboarding", MODULE_PATH)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class DeepSeekOnboardingTest(unittest.TestCase):
    def test_request_uses_json_mode_without_credentials(self):
        payload = MODULE.build_api_request({"contract_digest": "abc"}, "deepseek-v4-flash")
        encoded = json.dumps(payload)
        self.assertEqual(payload["response_format"], {"type": "json_object"})
        self.assertEqual(payload["model"], "deepseek-v4-flash")
        self.assertEqual(payload["thinking"], {"type": "disabled"})
        self.assertEqual(payload["temperature"], 0)
        self.assertNotIn("api_key", encoded)
        self.assertNotIn("Authorization", encoded)

    def test_parse_completion_returns_binding_and_usage(self):
        binding = {"version": 1, "id": "generated"}
        parsed, audit = MODULE.parse_completion(
            {
                "id": "response-1",
                "model": "deepseek-v4-flash",
                "system_fingerprint": "fp-1",
                "choices": [{"finish_reason": "stop", "message": {"content": json.dumps(binding)}}],
                "usage": {
                    "prompt_tokens": 10,
                    "prompt_cache_hit_tokens": 6,
                    "prompt_cache_miss_tokens": 4,
                    "completion_tokens": 4,
                    "total_tokens": 14,
                    "completion_tokens_details": {"reasoning_tokens": 0},
                },
            }
        )
        self.assertEqual(parsed, binding)
        self.assertEqual(audit["total_tokens"], 14)
        self.assertEqual(audit["prompt_cache_hit_tokens"], 6)

    def test_key_file_must_be_private(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "key.txt"
            path.write_text("secret-value", encoding="utf-8")
            os.chmod(path, 0o644)
            with self.assertRaises(ValueError):
                MODULE.read_api_key(str(path))
            os.chmod(path, 0o600)
            self.assertEqual(MODULE.read_api_key(str(path)), "secret-value")

    def test_only_official_endpoint_is_allowed(self):
        MODULE.validate_endpoint("https://api.deepseek.com/chat/completions")
        MODULE.validate_endpoint("https://api.deepseek.com/v1/chat/completions")
        with self.assertRaises(ValueError):
            MODULE.validate_endpoint("https://example.com/chat/completions")

    def test_generation_audit_records_prompt_policy(self):
        response = {
            "id": "response-1",
            "model": "deepseek-v4-flash",
            "choices": [
                {
                    "finish_reason": "stop",
                    "message": {"content": json.dumps({"version": 1, "id": "generated"})},
                }
            ],
            "usage": {},
        }
        original = MODULE.call_deepseek
        MODULE.call_deepseek = lambda endpoint, api_key, payload: response
        try:
            result = MODULE.generate(
                {"version": 1, "request": {"contract_digest": "abc"}},
                MODULE.DEFAULT_ENDPOINT,
                MODULE.DEFAULT_MODEL,
                "not-a-real-key",
            )
        finally:
            MODULE.call_deepseek = original
        audit = result["audit"]
        self.assertEqual(len(audit["prompt_digest"]), 64)
        self.assertEqual(audit["thinking_mode"], "disabled")
        self.assertEqual(audit["temperature"], 0)
        self.assertEqual(audit["max_tokens"], 8000)


if __name__ == "__main__":
    unittest.main()
