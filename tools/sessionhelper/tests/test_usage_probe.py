import unittest

from sessionhelper.usage_probe import probe_claude_frame


class ClaudeUsageProbeTest(unittest.TestCase):
    def test_init_records_session_and_model_without_claiming_usage(self):
        result = probe_claude_frame({
            "type": "system",
            "subtype": "init",
            "session_id": "s1",
            "model": "claude-haiku-4-5-20251001",
        })
        self.assertEqual(result.state, "unknown")
        self.assertEqual(result.session_id, "s1")
        self.assertEqual(result.model, "claude-haiku-4-5-20251001")
        self.assertIsNone(result.normalized_used_tokens)

    def test_native_usage_stays_unknown_without_verified_max_context(self):
        result = probe_claude_frame({
            "type": "assistant",
            "session_id": "s1",
            "message": {
                "model": "claude-sonnet-4-5-20250929",
                "usage": {
                    "input_tokens": 12,
                    "cache_creation_input_tokens": 3,
                    "cache_read_input_tokens": 40,
                    "output_tokens": 7,
                },
            },
        })
        self.assertEqual(result.state, "unknown")
        self.assertEqual(result.confidence, "partial")
        self.assertIsNone(result.normalized_used_tokens)
        self.assertIsNone(result.max_context)
        self.assertEqual(result.raw_value["cache_read_input_tokens"], 40)

    def test_invalid_token_field_fails_closed(self):
        result = probe_claude_frame({
            "type": "assistant",
            "message": {
                "usage": {
                    "input_tokens": -1,
                    "cache_creation_input_tokens": 0,
                    "cache_read_input_tokens": 0,
                    "output_tokens": 1,
                },
            },
        })
        self.assertEqual(result.state, "unknown")
        self.assertEqual(result.confidence, "unknown")
        self.assertEqual(result.reason, "invalid_token_fields")


if __name__ == "__main__":
    unittest.main()
