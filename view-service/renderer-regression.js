const { chromium } = require('playwright');
const assert = require('assert');
const baseUrl = process.env.RENDERER_URL || 'http://127.0.0.1:19312/renderer.html';

(async () => {
  const browser = await chromium.launch({ headless: true });
  try {
    for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
      const page = await browser.newPage({ viewport });
      await page.goto(`${baseUrl}?mock=1&unlocked=1`, { waitUntil: 'domcontentloaded' });
      await page.evaluate(() => {
        resetConversation();
        handleIncomingEvent({ kind: 'user_input', text: '排队中的外部任务', delivery_id: 'd-wait', cursor: '1' });
      });
      assert.strictEqual(await page.locator('#waitingArea').isVisible(), true);
      assert.strictEqual(await page.locator('#waitingList > .waiting-item').count(), 1);
      assert.match(await page.locator('#waitingList').innerText(), /排队中的外部任务/);
      assert.strictEqual(await page.locator('#timeline > .turn-group').count(), 0);
      await page.evaluate(() => {
        handleIncomingEvent({ kind: 'state_change', data: { change: 'accepted' }, delivery_id: 'd-wait', turn_id: 't-wait', cursor: '2' });
      });
      assert.strictEqual(await page.locator('#waitingArea').isVisible(), false);
      assert.strictEqual(await page.locator('#waitingList > .waiting-item').count(), 0);
      assert.strictEqual(await page.locator('#timeline > .turn-group').count(), 1);
      assert.match(await page.locator('#timeline > .turn-group .turn-user').innerText(), /排队中的外部任务/);
      await page.evaluate(() => {
        resetConversation();
        [
          { kind: 'user_input', text: '诊断 Milvus', delivery_id: 'd-pw', turn_id: 't-pw', cursor: '10' },
          { kind: 'tool_call', data: { name: 'Read', input: { file_path: '/tmp/a' } }, delivery_id: 'd-pw', turn_id: 't-pw', cursor: '20' },
          { kind: 'tool_call', data: { name: 'Bash', input: { command: 'milvus-check --token shencedeepseek-very-long-inline-token-without-breaks-00112233445566778899' } }, delivery_id: 'd-pw', turn_id: 't-pw', cursor: '30' },
          { kind: 'assistant_text', text: '诊断完成。', delivery_id: 'd-pw', turn_id: 't-pw', cursor: '40' },
        ].forEach(handleIncomingEvent);
      });
      assert.strictEqual(await page.locator('#timeline > .turn-group').count(), 1);
      assert.strictEqual(await page.locator('#timeline > .event-card').count(), 0);
      const group = page.locator('#timeline > .turn-group');
      const tools = group.locator('.turn-tools');
      assert.strictEqual(await tools.getAttribute('open'), null);
      assert.strictEqual(await group.locator('.turn-tool-item').first().isVisible(), false);
      await tools.locator(':scope > summary').click();
      assert.notStrictEqual(await tools.getAttribute('open'), null);
      assert.strictEqual(await group.locator('.turn-tool-item').count(), 2);
      assert.match(await group.locator('.turn-tool-item').first().innerText(), /Read/);
      assert.match(await group.locator('.turn-tool-item').last().innerText(), /Bash/);
      assert.match(await group.locator('.turn-user').innerText(), /诊断 Milvus/);
      assert.match(await group.locator('.turn-assistant').innerText(), /诊断完成/);
      await page.evaluate(() => {
        handleIncomingEvent({ kind: 'state_change', data: { change: 'turn_completed', turn_cost_usd: 0.1234 }, delivery_id: 'd-pw', turn_id: 't-pw', cursor: '50' });
      });
      assert.strictEqual(await group.locator('.turn-tools').count(), 0);
      assert.match(await group.locator('.turn-completed').innerText(), /本回合 \$0\.1234/);
      assert.deepStrictEqual(await group.locator(':scope > *').evaluateAll(nodes => nodes.map(n => n.className)), [
        'event-body turn-section turn-user',
        'event-body turn-section turn-assistant',
        'turn-completed',
      ]);
      const layout = await page.evaluate(() => ({
        scrollWidth: document.documentElement.scrollWidth,
        clientWidth: document.documentElement.clientWidth,
      }));
      assert.strictEqual(layout.scrollWidth, layout.clientWidth);
      await page.screenshot({ path: `/tmp/renderer-pw/turn-group-${viewport.width}.png`, fullPage: true });
      await page.close();
    }
    console.log('renderer regression PASS: waiting admission + live tool folding + completed cleanup (1280x900, 390x844)');
  } finally {
    await browser.close();
  }
})().catch(err => {
  console.error(err);
  process.exit(1);
});
