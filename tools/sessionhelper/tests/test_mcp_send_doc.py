"""Check caller-visible MCP tools/list guidance; no send, socket, or Hub call."""
import unittest

from sessionhelper.mcp_shim import Request, _dispatch


class SendMessageDocumentationTests(unittest.TestCase):
    def schema(self):
        response = _dispatch(Request(jsonrpc="2.0", method="tools/list", params={}, id=1))
        self.assertIsNone(response.error)
        return next(tool["inputSchema"] for tool in response.result["tools"]
                    if tool["name"] == "send_message")

    def test_to_uses_full_agent_name_and_cross_user_group_scope(self):
        description = self.schema()["properties"]["to"]["description"]
        self.assertIn("Agent 全名", description)
        self.assertIn("名字.后缀", description)
        self.assertIn("oaagent.lbl", description)
        self.assertIn("跨用户", description)
        self.assertIn("同在一个编组", description)
        self.assertNotIn("2642", description)
        self.assertNotIn("#key", description)
        self.assertNotIn("补全", description)

    def test_reply_guidance_requires_explicit_expect_reply(self):
        policy = self.schema()["properties"]["reply_policy"]
        self.assertIn("请明确设为 expect_reply", policy["description"])
        self.assertIn("缺省不产 auto-reply", policy["description"])
        self.assertEqual(policy["enum"], ["expect_reply", "no_reply", "ack_only"])


if __name__ == "__main__":
    unittest.main()
