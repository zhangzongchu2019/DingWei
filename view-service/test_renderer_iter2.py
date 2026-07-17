import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parent


class RendererIter2ContractTest(unittest.TestCase):
    def test_shared_waiting_and_progress_markup(self):
        for name in ("renderer.html", "renderer-canary-sse.html"):
            with self.subTest(renderer=name):
                text = (ROOT / name).read_text()
                self.assertNotIn('class="waiting-title"', text)
                self.assertNotIn("content: '⏳'", text)
                self.assertIn("@keyframes dw-waiting-spin", text)
                self.assertIn('id="taskProgress" hidden aria-hidden="true"', text)
                self.assertIn("taskProgress.hidden = stats.sessionState !== 'running'", text)
                self.assertIn("@keyframes dw-task-indeterminate", text)
                self.assertIn("prefers-reduced-motion: reduce", text)

    def test_canary_revalidates_bfcache_and_terminal_page_state(self):
        text = (ROOT / "renderer-canary-sse.html").read_text()
        self.assertIn("addEventListener('pageshow', event =>", text)
        self.assertIn("if (!event.persisted || stats.authMode !== 'view_page_v2') return", text)
        self.assertIn("if (!['locked', 'unlocked'].includes(resumed.state))", text)
        self.assertIn("resetPageIdentityChannel()", text)

    def test_v2_status_cannot_override_page_write_authority(self):
        text = (ROOT / "renderer-canary-sse.html").read_text()
        self.assertIn("const pageAuthorized = stats.authMode === 'view_page_v2'", text)
        self.assertIn("if (!pageAuthorized && data.readonly !== undefined)", text)
        self.assertIn("if (!pageAuthorized && incomingState === 'readonly')", text)

    def test_shared_lock_rendering_is_idempotent_and_focus_safe(self):
        for name in ("renderer.html", "renderer-canary-sse.html"):
            with self.subTest(renderer=name):
                text = (ROOT / name).read_text()
                self.assertIn("function setHumanInputDisabled(disabled)", text)
                self.assertIn("document.activeElement === humanInput", text)
                self.assertIn("copyUnlockBtn.textContent !== '复制解锁指令'", text)


if __name__ == "__main__":
    unittest.main()
