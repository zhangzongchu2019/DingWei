import unittest

from sessionhelper.usage_probe import (
    CLAUDE_CONTEXT_LIMITS_SOURCE,
    CLAUDE_CONTEXT_LIMITS_VERSION,
    ContextLimitRecord,
    probe_claude_frame,
)


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
        model = "claude-haiku-4-5-20251001"
        result = probe_claude_frame({
            "type": "assistant",
            "message": {
                "model": model,
                "usage": {
                    "input_tokens": -1,
                    "cache_creation_input_tokens": 0,
                    "cache_read_input_tokens": 0,
                    "output_tokens": 1,
                },
            },
        }, context_limits={model: ContextLimitRecord(200_000, "official", "v1")})
        self.assertEqual(result.state, "unknown")
        self.assertEqual(result.confidence, "unknown")
        self.assertEqual(result.reason, "invalid_token_fields")

    def test_result_usage_is_retained_only_as_raw_evidence(self):
        result = probe_claude_frame({
            "type": "result",
            "session_id": "s1",
            "usage": {"input_tokens": 12, "output_tokens": 3},
        }, model="claude-haiku-4-5-20251001")
        self.assertEqual(result.state, "unknown")
        self.assertEqual(result.raw_kind, "result_usage")
        self.assertEqual(result.raw_value["input_tokens"], 12)
        self.assertIsNone(result.normalized_used_tokens)

    def test_empty_model_is_explicitly_unknown(self):
        result = probe_claude_frame({
            "type": "assistant",
            "message": {"usage": {
                "input_tokens": 1,
                "cache_creation_input_tokens": 0,
                "cache_read_input_tokens": 0,
                "output_tokens": 1,
            }},
        })
        self.assertEqual(result.reason, "model_missing")
        self.assertEqual(result.confidence, "unknown")

    def test_unknown_model_cannot_use_family_fallback(self):
        limits = {
            "claude-haiku-4-5-20251001": ContextLimitRecord(
                max_context=200_000,
                source="official-model-table",
                version="2026-07-26",
            ),
        }
        result = probe_claude_frame({
            "type": "assistant",
            "message": {
                "model": "claude-haiku-4-5-future",
                "usage": {
                    "input_tokens": 1,
                    "cache_creation_input_tokens": 0,
                    "cache_read_input_tokens": 0,
                    "output_tokens": 1,
                },
            },
        }, context_limits=limits)
        self.assertEqual(result.reason, "max_context_source_missing_for_exact_model")
        self.assertIsNone(result.max_context)

    def test_known_model_limit_does_not_promote_unverified_context_usage(self):
        model = "claude-haiku-4-5-20251001"
        result = probe_claude_frame({
            "type": "assistant",
            "message": {
                "model": model,
                "usage": {
                    "input_tokens": 1,
                    "cache_creation_input_tokens": 0,
                    "cache_read_input_tokens": 0,
                    "output_tokens": 1,
                },
            },
        }, context_limits={model: ContextLimitRecord(200_000, "official", "v1")})
        self.assertEqual(result.max_context, 200_000)
        self.assertEqual(result.reason, "context_semantics_unverified")
        self.assertIsNone(result.normalized_used_tokens)
        self.assertEqual(result.state, "unknown")

    def test_partial_token_schema_is_invalid_after_model_resolution(self):
        model = "claude-haiku-4-5-20251001"
        result = probe_claude_frame({
            "type": "assistant",
            "message": {"model": model, "usage": {"input_tokens": 1}},
        }, context_limits={model: ContextLimitRecord(200_000, "official", "v1")})
        self.assertEqual(result.reason, "invalid_token_fields")
        self.assertEqual(result.state, "unknown")

    def test_builtin_exact_model_table_is_versioned_and_auditable(self):
        model = "claude-haiku-4-5-20251001"
        result = probe_claude_frame({
            "type": "assistant",
            "message": {
                "model": model,
                "usage": {
                    "input_tokens": 1,
                    "cache_creation_input_tokens": 0,
                    "cache_read_input_tokens": 0,
                    "output_tokens": 1,
                },
            },
        })
        self.assertEqual(result.max_context, 200_000)
        self.assertEqual(result.max_context_source, "static-table")
        self.assertEqual(result.max_context_version, CLAUDE_CONTEXT_LIMITS_VERSION)
        self.assertIn("platform.claude.com", CLAUDE_CONTEXT_LIMITS_SOURCE)
        self.assertEqual(result.state, "unknown")

    def test_native_conflict_uses_smaller_denominator(self):
        model = "claude-haiku-4-5-20251001"
        frame = {
            "type": "assistant",
            "message": {
                "model": model,
                "usage": {
                    "input_tokens": 1,
                    "cache_creation_input_tokens": 0,
                    "cache_read_input_tokens": 0,
                    "output_tokens": 1,
                },
            },
        }
        smaller = probe_claude_frame(frame, native_max_context=150_000)
        self.assertEqual(smaller.max_context, 150_000)
        self.assertEqual(smaller.max_context_source, "min(native,static-table)")
        larger = probe_claude_frame(frame, native_max_context=250_000)
        self.assertEqual(larger.max_context, 200_000)

    def test_context_variant_suffix_does_not_match_static_table(self):
        result = probe_claude_frame({
            "type": "assistant",
            "message": {
                "model": "claude-haiku-4-5-20251001[1m]",
                "usage": {
                    "input_tokens": 1,
                    "cache_creation_input_tokens": 0,
                    "cache_read_input_tokens": 0,
                    "output_tokens": 1,
                },
            },
        })
        self.assertIsNone(result.max_context)
        self.assertEqual(result.reason, "max_context_source_missing_for_exact_model")


if __name__ == "__main__":
    unittest.main()
