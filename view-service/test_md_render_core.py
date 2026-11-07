"""markdown 渲染共享件(md-render-core.js)与产品件生成副本之间的**漂移闸**。

为什么有这个件:
  `0.2.1.50` 的交付条件要求 spec 展示面与 timeline **走同一条渲染路径**(组长 ts-No.79 后续裁定,选型 A)。
  两页由不同服务出(renderer 走 view-service、spec 页走 Go),共用一个 HTTP 静态资产要新增静态路由
  (牵 nginx 前缀改写 + 哈希 pin + "非白名单仍 404" 的阴格),代价远大于收益
  ⇒ 采用【单一事实源 + 生成副本】:`md-render-core.js` 是源,`renderer-canary-sse.html` 里那 6 段是副本。
  🔴 而"两份"这件事本身必须有闸,否则就是本仓反复吃过的那个亏:**两份必漂移,漂了没人发现**。

本件钉的:
  1 标记结构(6 段、各恰好一次、顺序是契约、无未登记段名)
  2 🔴 副本与源**逐字节相同**(跑 md-render-core-sync.py --check)
  3 🔴 **本闸自己有牙**(阳性对照):故意造漂移 ⇒ 必须 rc=1 且**指名是哪一段**;
     故意破坏标记 ⇒ 必须 rc=2;跑写入模式 ⇒ 必须把副本刷回一致。
     没有这一条,"同步一致"可能只是**工具根本没在比**(假绿最贵,因为它不产生任何动作)。
  4 导出钩子的名单 == 各段里真实声明的符号(少一个 ⇒ spec 页会**静默**缺一个函数)
  5 图片扩展名那几个独有 token 在**两个文件里各只出现一次**(G0(d) 的跨文件版:谁再写第三份清单就红)
  6 共享件里只允许**一个** IIFE(就是导出钩子)—— 把核心包进 IIFE 会破坏页面作用域,
    进而让 0206 的 ④i(就地改 FILE_EXT_KIND)与 image 闸的 G0b(直接调 isOwnFileUrl)取不到符号
  7 🔴 共享件**能被 Node 独立加载**(这正是 spec 页/别的消费方要用性质),并抽查消毒口径

跑法:  cd view-service && python3 -m unittest test_md_render_core -v
"""
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile
import unittest

HERE = pathlib.Path(__file__).resolve().parent
CORE = HERE / "md-render-core.js"
RENDERER = HERE / "renderer-canary-sse.html"
SYNC = HERE / "md-render-core-sync.py"
SECTION_ORDER = ("escape", "own-url", "ext-kind", "ext-of", "never-inline", "render")
MARK_RE = re.compile(r"^\s*//\s*(?:🔴\s*)?DW-MD-CORE:([a-z-]+)\s+(BEGIN|END)\b")
# 独有 token:只在图片白名单那一行出现(与 renderer-markdown-image-regression.js 的 G0(d) 同一手法)
UNIQUE_TOKENS = ("avif", "heic", "tiff")


def read(path):
    return path.read_text(encoding="utf-8")


def marks(lines):
    """返回 [(段名, BEGIN|END, 行号)],按出现顺序。"""
    out = []
    for idx, line in enumerate(lines, start=1):
        m = MARK_RE.match(line)
        if m:
            out.append((m.group(1), m.group(2), idx))
    return out


def sections(lines):
    """按标记切段 → {段名: 段内行}。结构异常直接 AssertionError(本件自己就是结构闸)。"""
    out, current, buf = {}, None, []
    for line in lines:
        m = MARK_RE.match(line)
        if m:
            name, kind = m.group(1), m.group(2)
            if kind == "BEGIN":
                assert current is None, f"标记嵌套:{name} 在 {current} 里"
                current, buf = name, []
            else:
                assert current == name, f"END 与 BEGIN 不配对:{current} / {name}"
                out[name] = buf
                current = None
            continue
        if current is not None:
            buf.append(line)
    assert current is None, f"段 {current} 没有 END"
    return out


def run_sync(*args, core=None, renderer=None):
    cmd = [sys.executable, str(SYNC), *args]
    if core is not None:
        cmd += ["--core", str(core)]
    if renderer is not None:
        cmd += ["--renderer", str(renderer)]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    return proc.returncode, proc.stdout + proc.stderr


class MarkerStructureTest(unittest.TestCase):
    def test_six_sections_exactly_once_in_contract_order(self):
        for path in (CORE, RENDERER):
            with self.subTest(file=path.name):
                ms = marks(read(path).split("\n"))
                names = [n for n, k, _ in ms if k == "BEGIN"]
                self.assertEqual(names, list(SECTION_ORDER),
                                 f"{path.name} 的段顺序/集合不是契约顺序(顺序变=代码被搬过家)")
                ends = [n for n, k, _ in ms if k == "END"]
                self.assertEqual(ends, list(SECTION_ORDER), f"{path.name} 的 END 标记不齐")
                self.assertEqual(len(ms), 2 * len(SECTION_ORDER),
                                 f"{path.name} 里有重复标记(每个段名应恰好一对)")

    def test_no_unregistered_section_names_anywhere(self):
        # 🔴 未登记的段名会被同步工具硬中止(rc=2);这里先在本件里说清是哪一行,省一次误诊
        known = {f"DW-MD-CORE:{n}" for n in SECTION_ORDER}
        for path in (CORE, RENDERER):
            with self.subTest(file=path.name):
                for idx, line in enumerate(read(path).split("\n"), start=1):
                    for m in re.finditer(r"DW-MD-CORE:([A-Za-z0-9_-]+)", line):
                        self.assertIn(f"DW-MD-CORE:{m.group(1)}", known,
                                      f"{path.name}:{idx} 出现未登记段名 {m.group(1)}"
                                      f"(已登记:{', '.join(SECTION_ORDER)})")

    def test_sections_are_non_empty_and_match_between_files(self):
        core = sections(read(CORE).split("\n"))
        rend = sections(read(RENDERER).split("\n"))
        self.assertEqual(sorted(core), sorted(SECTION_ORDER))
        for name in SECTION_ORDER:
            with self.subTest(section=name):
                self.assertTrue(core[name], f"共享件里 {name} 段是空的")
                self.assertEqual(core[name], rend[name],
                                 f"{name} 段与共享件不一致(先跑 md-render-core-sync.py)")


class DriftGateHasTeethTest(unittest.TestCase):
    """🔴 阳性对照:证明上面那些"一致"不是**工具没在比**造成的假绿。"""

    def _pair(self, tmp):
        core = pathlib.Path(tmp) / "md-render-core.js"
        rend = pathlib.Path(tmp) / "renderer-canary-sse.html"
        shutil.copyfile(CORE, core)
        shutil.copyfile(RENDERER, rend)
        return core, rend

    def test_clean_pair_is_in_sync(self):
        with tempfile.TemporaryDirectory() as tmp:
            core, rend = self._pair(tmp)
            rc, out = run_sync("--check", core=core, renderer=rend)
            self.assertEqual(rc, 0, f"干净的一对竟然报漂移:\n{out}")

    def test_drift_in_core_is_detected_and_named(self):
        with tempfile.TemporaryDirectory() as tmp:
            core, rend = self._pair(tmp)
            text = read(core)
            self.assertEqual(text.count('<div class="md">'), 1, "锚点不唯一,换个更唯一的串")
            core.write_text(text.replace('<div class="md">', '<div class="md-drifted">'), encoding="utf-8")
            rc, out = run_sync("--check", core=core, renderer=rend)
            self.assertEqual(rc, 1, f"改了共享件却没报漂移 ⇒ 本闸没有牙:\n{out}")
            self.assertIn("render", out, f"报了漂移但没指名是哪一段:\n{out}")
            self.assertEqual(read(rend), read(RENDERER), "--check 模式不许写文件")

    def test_drift_in_renderer_copy_is_detected_and_named(self):
        with tempfile.TemporaryDirectory() as tmp:
            core, rend = self._pair(tmp)
            text = read(rend)
            old = ".replaceAll('\"', '&quot;')"
            self.assertEqual(text.count(old), 1, "锚点不唯一")
            rend.write_text(text.replace(old, ".replaceAll('\"', '&quote;')"), encoding="utf-8")
            rc, out = run_sync("--check", core=core, renderer=rend)
            self.assertEqual(rc, 1, f"有人直接改了产品件里的生成副本却没报 ⇒ 本闸没有牙:\n{out}")
            self.assertIn("escape", out, f"没指名段:\n{out}")

    def test_write_mode_repairs_the_copy(self):
        with tempfile.TemporaryDirectory() as tmp:
            core, rend = self._pair(tmp)
            rend.write_text(read(rend).replace("function highlightCode(code, lang) {",
                                               "function highlightCode(code, lang) { /* 手改 */"),
                            encoding="utf-8")
            rc, _ = run_sync("--check", core=core, renderer=rend)
            self.assertEqual(rc, 1, "前置条件:先确认它确实是漂移的")
            rc, out = run_sync(core=core, renderer=rend)
            self.assertEqual(rc, 0, f"写入模式失败:\n{out}")
            rc, out = run_sync("--check", core=core, renderer=rend)
            self.assertEqual(rc, 0, f"写入后仍漂移(修法建议是空的):\n{out}")

    def test_broken_marker_hard_fails_not_silently_skips(self):
        # 🔴 静默跳过 = 假绿:少一个 END 却"同步成功"是最坏的结果
        with tempfile.TemporaryDirectory() as tmp:
            core, rend = self._pair(tmp)
            lines = read(core).split("\n")
            for i, line in enumerate(lines):
                if MARK_RE.match(line) and line.strip().endswith("END") and "escape" in line:
                    del lines[i]
                    break
            else:
                self.fail("找不到 escape 段的 END 标记(锚点过期)")
            core.write_text("\n".join(lines), encoding="utf-8")
            rc, out = run_sync("--check", core=core, renderer=rend)
            self.assertEqual(rc, 2, f"标记坏了却返回 {rc}(应硬中止 rc=2):\n{out}")


class ExportSurfaceTest(unittest.TestCase):
    def test_export_hook_lists_exactly_the_declared_symbols(self):
        core = read(CORE)
        bodies = sections(core.split("\n"))
        declared = set()
        for name in SECTION_ORDER:
            for line in bodies[name]:
                m = re.match(r"^ {4}(?:function|const) ([A-Za-z_$][\w$]*)", line)
                if m:
                    declared.add(m.group(1))
        hook = core.split("导出钩子")[-1]
        exported = set(re.findall(r"^ {4}(\w+): \1,$", hook, flags=re.M))
        self.assertTrue(declared, "一个符号都没抽到 ⇒ 本件的正则过期了(先修正则,别改产品)")
        self.assertEqual(declared - exported, set(),
                         f"这些符号在段里声明了却没导出 ⇒ spec 页会**静默**缺函数:{sorted(declared - exported)}")
        self.assertEqual(exported - declared, set(),
                         f"导出名单里有段内不存在的符号(改名后忘了同步):{sorted(exported - declared)}")

    def test_core_has_exactly_one_iife_and_it_is_the_export_hook(self):
        # 🔴 把核心包进 IIFE 会让这些符号离开页面作用域 ⇒ 0206 的 ④i 与 image 闸的 G0b 都取不到
        core = read(CORE)
        self.assertEqual(core.count("(function ("), 1, "共享件里 IIFE 的数量不是 1(核心被包起来了?)")
        last_end = max(i for i, line in enumerate(core.split("\n"))
                       if MARK_RE.match(line) and line.strip().endswith("END"))
        iife_line = next(i for i, line in enumerate(core.split("\n")) if "(function (" in line)
        self.assertGreater(iife_line, last_end, "唯一的 IIFE 必须出现在所有段**之后**(即导出钩子)")

    def test_unique_image_tokens_appear_once_per_file(self):
        for path in (CORE, RENDERER):
            text = read(path)
            for tok in UNIQUE_TOKENS:
                with self.subTest(file=path.name, token=tok):
                    n = len(re.findall(r"\b" + tok + r"\b", text))
                    self.assertEqual(n, 1,
                                     f"'{tok}' 在 {path.name} 里出现 {n} 次(应恰好 1 次 = FILE_EXT_KIND 图片行)"
                                     f" ⇒ 有人另写了一份图片清单。两份必漂移,请复用 FILE_EXT_KIND/kindFromExt。")


NODE_SMOKE = r'''
const assert = require('assert');
const core = require('%s');
const keys = ['escapeHtml','isOwnFileUrl','canonicalPublicOrigin','localFileUrl','canonicalFileUrl','fileExtOf','kindFromExt','FILE_EXT_KIND','FILE_EXT_NEVER_INLINE',
  'splitFencedBlocks','renderMarkdown','renderMarkdownText','isTableStart','splitTableRow','renderTable',
  'renderList','renderInline','renderCode','highlightCode','isInlineImageUrl','mdImageHtml'];
assert.deepStrictEqual(keys.filter((k) => !(k in core)), [], 'API 缺符号');
// 消毒口径:5 个实体一个都不能少(" → &quot; 是 ts-No.76 认定的承重墙)
assert.strictEqual(core.escapeHtml('<a href="x">&\'</a>'), '&lt;a href=&quot;x&quot;&gt;&amp;&#39;&lt;/a&gt;');
const h1 = core.renderMarkdown('# hi');
assert.ok(h1.includes('<h1>hi</h1>'), 'h1: ' + h1);
assert.ok(h1.startsWith('<div class="md">'), 'wrapper: ' + h1);
const code = core.renderMarkdown('```python\ndef f():\n    return 1\n```');
assert.ok(code.includes('<pre><code>'), 'pre/code: ' + code);
assert.ok(code.includes('class="copy-code"'), 'Copy 按钮的标记(需宿主接线): ' + code);
assert.ok(code.includes('tok-kw'), 'python 高亮: ' + code);
const table = core.renderMarkdown('| a | b |\n| --- | --- |\n| 1 | 2 |');
assert.ok(table.includes('<table>') && table.includes('<td>1</td>'), 'table: ' + table);
const task = core.renderMarkdown('- [x] done');
assert.ok(task.includes('<input type="checkbox" disabled checked>'), 'task list: ' + task);
// 🔴 消毒口径抽查(不是注入审计,那在 ts-No.76/79 两件里):裸 HTML 必须被转义,不得产出活元素
const inj = core.renderMarkdown('<img src=x onerror=alert(1)>');
assert.ok(!/<img/i.test(inj), '裸 <img> 竟然活了: ' + inj);
assert.ok(inj.includes('&lt;img'), '应被转义: ' + inj);
const inj2 = core.renderMarkdown('# <script>window.__p=1</script>');
assert.ok(!/<script/i.test(inj2), '标题里 script 活了: ' + inj2);
// 表与 deny-set 是纯数据,Node 侧可直接读(spec 页那侧也会这么用)
assert.strictEqual(core.FILE_EXT_KIND.png, 'image');
assert.strictEqual(core.FILE_EXT_KIND.svg, undefined, 'svg 不该在表里(靠 deny-set 写死那层挡)');
assert.strictEqual(core.FILE_EXT_NEVER_INLINE.has('svg'), true);
// 🔴 非浏览器环境下 isOwnFileUrl/fileExtOf 是【静默】降级(它们自带 try/catch,吞掉 location 的
//    ReferenceError)⇒ 不抛错。下面是**实测值**,钉住它,免得下一个人以为会响亮失败、或以为是纯函数。
//    🔵 这一腿的由来:我原先断言"必须抛",**实测红了** —— 错的是我读码得出的结论,不是产品。
assert.strictEqual(typeof globalThis.location, 'undefined',
  '这个 Node 竟然有全局 location ⇒ 本腿前提变了,先重判再说(别顺手改断言)');
const OWN = 'https://dw.example.invalid/f/aa/bb/26/0921/0123456789abcdef0123456789abcdef.png';
assert.strictEqual(core.isOwnFileUrl(OWN), false, 'isOwnFileUrl 在无 location 环境下的实测值变了');
assert.strictEqual(core.fileExtOf(OWN), '', 'fileExtOf 同上');
assert.strictEqual(core.kindFromExt(OWN), 'download', 'kindFromExt 同上');
assert.strictEqual(core.isInlineImageUrl(OWN), false, 'isInlineImageUrl 同上');
const degraded = core.renderMarkdown('![图](' + OWN + ')');
assert.ok(!/<img/i.test(degraded), 'Node 里竟然出图了 ⇒ 上面那组静默降级的结论过期了: ' + degraded);
assert.ok(degraded.includes('<a href="' + OWN + '"'), '应静默降级成链接: ' + degraded);
console.log('md-render-core node smoke: PASS(' + keys.length + ' 个导出符号 · 消毒口径 · '
  + '无 location 时静默降级为链接已钉住实测值)');
'''


class NodeLoadableTest(unittest.TestCase):
    def test_core_loads_standalone_in_node(self):
        node = shutil.which("node")
        if not node:
            self.skipTest("没有 node ⇒ 这一格测不到(不是通过)。装 node 后重跑,别把 SKIP 读成绿")
        script = NODE_SMOKE % str(CORE).replace("\\", "\\\\")
        proc = subprocess.run([node, "-e", script], capture_output=True, text=True)
        self.assertEqual(proc.returncode, 0,
                         f"共享件不能被 Node 独立加载/断言失败:\n{proc.stdout}\n{proc.stderr}")
        self.assertIn("node smoke: PASS", proc.stdout)


if __name__ == "__main__":
    unittest.main(verbosity=2)
