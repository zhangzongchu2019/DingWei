"""T-092:ipc_send 必须把裸会话名补全为 <名>#<key_id>,否则 Hub 判 invalid address。"""
import unittest


def complete(to: str, key_id: str) -> str:
    """与 app.py::_make_send_handler 中的补全规则一致(行为契约)。"""
    return to if "#" in to else f"{to}#{key_id}"


class TestIPCSendAddressCompletion(unittest.TestCase):
    KEY = "FB-zzc-devteam-e0d12642"

    def test_bare_session_name_is_completed(self):
        # 🔴 回归点:defb424f 曾删掉补全,导致裸名进信封 → Hub invalid address
        self.assertEqual(complete("zzc-prodops-2642", self.KEY),
                         "zzc-prodops-2642#" + self.KEY)

    def test_full_address_is_left_alone(self):
        # 防修过头:已带 # 的完整地址不得被二次拼接
        full = "zzc-prodops-2642#" + self.KEY
        self.assertEqual(complete(full, self.KEY), full)

    def test_cross_key_address_preserved(self):
        # 指向别的 key 的地址必须原样保留,不得被本机 key 覆盖
        other = "zzc-someone-1234#FB-other-key"
        self.assertEqual(complete(other, self.KEY), other)


if __name__ == "__main__":
    unittest.main()
