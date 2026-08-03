import unittest

from sessionhelper.reply_origin import (
    KIND_ACK,
    KIND_MIRROR,
    classify_reply_decision,
    is_control_envelope,
    reply_carryforward_meta,
)


def env(body="", meta=None, from_addr="b#k", eid="e1"):
    return {"id": eid, "to": "a#k", "from": from_addr, "body": body, "meta": meta or {}}


class ReplyOriginTest(unittest.TestCase):
    def test_is_control_envelope(self):
        self.assertTrue(is_control_envelope(env(body='{"kind":"mcs_dispatch_v1"}')))
        self.assertTrue(is_control_envelope(env(body='{"kind":"mcs_receipt_v1"}')))
        self.assertTrue(is_control_envelope(env(meta={"kind": KIND_ACK})))
        self.assertTrue(is_control_envelope(env(meta={"type": "delivery_ack"})))
        self.assertTrue(is_control_envelope(env(meta={"protocol_class": "control"})))
        self.assertFalse(is_control_envelope(env(body="hello")))

    def test_reply_suppressed_for_control(self):
        # B1: ack/control 绝不生成 reply
        self.assertFalse(classify_reply_decision(env(meta={"kind": KIND_ACK})).allow)
        self.assertEqual(classify_reply_decision(env(body='{"kind":"mcs_dispatch_v1"}')).reason, "control")

    def test_reply_suppressed_for_mirror(self):
        d = classify_reply_decision(env(meta={"kind": KIND_MIRROR, "reply_policy": "expect_reply"}))
        self.assertFalse(d.allow)
        self.assertEqual(d.reason, "mirror")

    def test_reply_suppressed_for_conversation_terminal(self):
        d = classify_reply_decision(env(meta={"conversation_terminal": True, "reply_policy": "expect_reply"}))
        self.assertFalse(d.allow)
        self.assertEqual(d.reason, "conversation_terminal")

    def test_reply_suppressed_for_hop_backstop(self):
        d = classify_reply_decision(env(meta={"hop_count": 16, "reply_policy": "expect_reply"}))
        self.assertFalse(d.allow)
        self.assertEqual(d.reason, "hop_backstop")

    def test_reply_suppressed_default_no_expect(self):
        # 默认(无 reply_policy)=保守不回,不制造新噪声(R2)
        d = classify_reply_decision(env(meta={}))
        self.assertFalse(d.allow)
        self.assertEqual(d.reason, "no_expect_reply")

    def test_reply_allowed_expect_reply_clean(self):
        d = classify_reply_decision(env(meta={"reply_policy": "expect_reply", "hop_count": 3}))
        self.assertTrue(d.allow)

    def test_carryforward_meta(self):
        m = reply_carryforward_meta(
            env(meta={"hop_count": 4, "conversation_id": "cid1", "reply_policy": "expect_reply"}, eid="msg9")
        )
        self.assertEqual(m["kind"], "reply")
        self.assertTrue(m["conversation_terminal"])  # R3 打断 A<->B
        self.assertEqual(m["hop_count"], 4)  # carry-forward(hub 递增)
        self.assertEqual(m["conversation_id"], "cid1")
        self.assertEqual(m["in_reply_to"], "msg9")

    def test_two_hop_termination_semantics(self):
        # A→B content(expect_reply)→ B 生成 reply(带 conversation_terminal=True)
        a_to_b = env(meta={"reply_policy": "expect_reply", "hop_count": 1}, eid="a1")
        self.assertTrue(classify_reply_decision(a_to_b).allow)
        b_reply_meta = reply_carryforward_meta(a_to_b)
        # A 收到 B 的 reply(conversation_terminal=True)→ A 不再回 = 2 跳终止
        b_to_a = env(meta=b_reply_meta, eid="b1")
        self.assertFalse(classify_reply_decision(b_to_a).allow)
        self.assertEqual(classify_reply_decision(b_to_a).reason, "conversation_terminal")


if __name__ == "__main__":
    unittest.main()
