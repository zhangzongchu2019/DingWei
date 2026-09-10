import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parent


class RendererIter2ContractTest(unittest.TestCase):
    def test_shared_waiting_and_progress_markup(self):
        for name in ("renderer-canary-sse.html",):
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
        for name in ("renderer-canary-sse.html",):
            with self.subTest(renderer=name):
                text = (ROOT / name).read_text()
                self.assertIn("function setHumanInputDisabled(disabled)", text)
                self.assertIn("document.activeElement === humanInput", text)
                self.assertIn("copyUnlockBtn.textContent !== '复制解锁指令'", text)

    def test_activity_uses_extensible_dock_outside_conversation_timeline(self):
        text = (ROOT / "renderer-canary-sse.html").read_text()
        self.assertIn('class="workspace-dock"', text)
        self.assertIn('data-dock-section="activity"', text)
        self.assertIn("function registerDockSection(id)", text)
        self.assertIn("function appendDockCard(sectionID, card)", text)
        self.assertIn("registerDockSection('activity')", text)
        self.assertIn("appendDockCard('activity', card)", text)
        self.assertNotIn('className = `event-card ${cls}`', text)

    def test_error_toast_does_not_expand_dock_and_history_is_retained(self):
        text = (ROOT / "renderer-canary-sse.html").read_text()
        self.assertIn("function showErrorToast(text)", text)
        self.assertIn("appendDockCard('activity', card);\n      if (!replayInProgress) showErrorToast(text);", text)
        self.assertIn("setDockExpanded(false);", text)

    def test_view_only_change_keeps_event_transport_out_of_scope(self):
        text = (ROOT / "renderer-canary-sse.html").read_text()
        self.assertIn("function handleIncomingEvent(ev)", text)
        self.assertIn("function renderTurnEvent(turnKey, ev)", text)


if __name__ == "__main__":
    unittest.main()
