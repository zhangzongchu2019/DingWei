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

    def test_t7_ack_with_in_reply_to_never_creates_obligation(self):
        # B1/T7:ack 即便误带 in_reply_to + reply_policy=expect_reply,仍不生成 reply(control 短路)
        ack = env(meta={"kind": KIND_ACK, "in_reply_to": "m1", "reply_policy": "expect_reply"})
        d = classify_reply_decision(ack)
        self.assertFalse(d.allow)
        self.assertEqual(d.reason, "control")

    def test_t8_t10_mcs_terminal_independent_of_conversation(self):
        # T8/T10:mcs done(mcs_receipt_v1)=control,不产 conversation reply;且不影响独立 conversation 消息
        mcs_done = env(body='{"kind":"mcs_receipt_v1","phase":"done"}', meta={"reply_policy": "expect_reply"})
        self.assertFalse(classify_reply_decision(mcs_done).allow)
        self.assertEqual(classify_reply_decision(mcs_done).reason, "control")
        # 另一条独立 conversation 消息(expect_reply)仍可回 = 两条线独立
        conv = env(meta={"reply_policy": "expect_reply"})
        self.assertTrue(classify_reply_decision(conv).allow)

    def test_t9_conversation_terminal_stays_conversation_class(self):
        # T9:conversation_terminal 只关 reply obligation,不把消息变 control、不碰 M5 线
        ct = env(meta={"conversation_terminal": True, "reply_policy": "expect_reply"})
        self.assertFalse(is_control_envelope(ct))  # 仍是 conversation 类
        self.assertFalse(classify_reply_decision(ct).allow)  # 但 R3 不再回
        self.assertEqual(classify_reply_decision(ct).reason, "conversation_terminal")

    def test_t6_session_reply_routes_to_origin(self):
        # T6:session-to-session 回复回到发件人(reply-to-origin);reply_target 对 session 源回落 from_addr
        from sessionhelper.protocol import AddressBook, reply_target

        book = AddressBook(session_name="me", key_id="k")
        to, _ = reply_target(env(from_addr="peer#k"), book)
        self.assertEqual(to, "peer#k")

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
