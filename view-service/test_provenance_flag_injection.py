"""业主 2026-09-11:「飞书头像不见了」「Agent 投递的消息没显示 Agent 头像」。

根因:渲染器把四个来源 SVG 整体挂在 window.PROVENANCE_ICONS_V1 之后,而 VS 从未注入过该变量。
本测试钉住注入本身;图标是否真渲染由 renderer-provenance-icon-regression.js 覆盖(含 flag 关的阳性对照)。
"""
import importlib
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))


def _load(**env):
    for k, v in env.items():
        if v is None:
            os.environ.pop(k, None)
        else:
            os.environ[k] = v
    import view_service
    return importlib.reload(view_service)


HTML = "<html><head><title>t</title></head><body>x</body></html>"


class ProvenanceFlagInjection(unittest.TestCase):
    def test_injected_before_head_close(self):
        """① 默认必须注入,且落在 </head> 之前(晚于渲染器读取就白注入了)。"""
        vs = _load(VS_PROVENANCE_ICONS=None)
        out = vs._inject_provenance_flag(HTML)
        self.assertIn("window.PROVENANCE_ICONS_V1=true", out)
        self.assertLess(out.index("PROVENANCE_ICONS_V1"), out.index("</head>"),
                        "flag 必须在 </head> 之前注入")

    def test_idempotent(self):
        """② 重复注入只能有一份(VS 对每个请求重读文件,防叠加)。"""
        vs = _load(VS_PROVENANCE_ICONS=None)
        once = vs._inject_provenance_flag(HTML)
        self.assertEqual(once, vs._inject_provenance_flag(once))
        self.assertEqual(once.count("PROVENANCE_ICONS_V1"), 1)

    def test_env_can_disable(self):
        """③ 留回退路:不改代码即可关掉。"""
        vs = _load(VS_PROVENANCE_ICONS="0")
        self.assertNotIn("PROVENANCE_ICONS_V1", vs._inject_provenance_flag(HTML))

    def test_missing_anchor_is_fail_open(self):
        """④ 找不到 </head> 时原样返回 —— 宁可没图标,不可白页。"""
        vs = _load(VS_PROVENANCE_ICONS=None)
        broken = "<html><body>no head</body></html>"
        self.assertEqual(vs._inject_provenance_flag(broken), broken)

    def test_real_renderer_gets_flag(self):
        """⑤ 拿**线上那份真渲染器**跑,不是拿玩具 HTML ——
        锚点写错(比如渲染器里没有 </head>)这类问题只有这一格能抓到。"""
        vs = _load(VS_PROVENANCE_ICONS=None)
        with open(vs.RENDERER_PATH, encoding="utf-8") as fh:
            real = fh.read()
        out = vs._inject_provenance_flag(real)
        self.assertIn("window.PROVENANCE_ICONS_V1=true", out)
        self.assertNotEqual(out, real, "真渲染器未被注入,说明锚点在它里面不存在")


if __name__ == "__main__":
    unittest.main(verbosity=2)


class ServeFileInjectsUnconditionally(unittest.TestCase):
    """🔴 上面五格只测了注入函数本身,测不到 _serve_file 是否**无条件**调用它。

    这正是本次特意防的坑:端点路径改写只在带 X-DW-Session 时才做,
    若把注入放进那个 if 里,直连 VS(不经反代)的场景就永远看不到图标,
    而单测全绿 —— 那是「不可能失败的检查」。故此处起真服务、真发请求。
    """

    @classmethod
    def setUpClass(cls):
        import tempfile
        import threading
        os.environ.pop("VS_PROVENANCE_ICONS", None)
        os.environ["VS_SESSION"] = "fixture-dev55-2642"
        os.environ["VS_STATE_FILE"] = os.path.join(tempfile.mkdtemp(), "view-state.json")
        os.environ["VS_PAGE_DB"] = os.path.join(tempfile.mkdtemp(), "view-pages.db")
        vs = _load()
        cls.vs = vs
        from http.server import ThreadingHTTPServer
        cls.srv = ThreadingHTTPServer(("127.0.0.1", 0), vs._Handler)
        cls.port = cls.srv.server_address[1]
        threading.Thread(target=cls.srv.serve_forever, daemon=True).start()

    @classmethod
    def tearDownClass(cls):
        cls.srv.shutdown()

    def _get(self, headers=None):
        import urllib.request
        req = urllib.request.Request(f"http://127.0.0.1:{self.port}/", headers=headers or {})
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.read().decode("utf-8", "replace")

    def test_without_session_header(self):
        """⑥ 直连 VS(无 X-DW-Session):必须也有 flag。"""
        self.assertIn("window.PROVENANCE_ICONS_V1=true", self._get())

    def test_with_session_header(self):
        """⑦ 经反代(带 X-DW-Session)时也必须有 flag —— 两条路径都要覆盖。"""
        body = self._get({"X-DW-Session": "fixture-dev55-2642"})
        self.assertIn("window.PROVENANCE_ICONS_V1=true", body)

    def test_endpoint_rewrite_is_currently_a_noop(self):
        """⑧ 如实记录一处**既有**死代码(非本次引入,已对 git HEAD 核过):

        _serve_file 里那个 for ep in (...) 改写循环找的是 '/events' 这类**字面量**,
        而渲染器早已改走 apiEp() 懒解析(`const apiEp = p => apiBase() + '/' + p`),
        全文零个此类字面量 ⇒ 该循环对当前渲染器恒为空操作。

        本格不是要求维持现状,而是不让它**静默**:哪天有人以为改写仍在生效
        (比如新增一个写死路径的端点),这里会立刻变红并指向此注释。
        """
        with open(self.vs.RENDERER_PATH, encoding="utf-8") as fh:
            real = fh.read()
        literals = {ep: real.count("'" + ep + "'")
                    for ep in ("/events", "/input", "/interrupt", "/control", "/status", "/page")}
        self.assertEqual(set(literals.values()), {0},
                         "渲染器里出现了写死的端点字面量,VS 的改写循环会开始生效 —— "
                         "请确认这是有意的,并同步更新本测试: " + repr(literals))
