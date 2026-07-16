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


if __name__ == "__main__":
    unittest.main()
