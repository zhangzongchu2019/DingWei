// 0.2.0.6 A 线功能级验收：**真的用界面传，真的看正文渲染**。
//
// 🔴 为什么是这个形状：0.2.0.5 的六条验收里没有一条是「打开界面试一次」，
//    于是「界面上根本没有上传入口」是业主自己用的时候发现的。
//    ⇒ 本件第一条断言就是「上传入口在界面上看得见」，其余每条都走真浏览器事件
//      （file picker / drop / paste），不是「代码里有这个元素」。
//
// 依赖：Playwright chromium；契约测试替身 renderer-0206-contract-mock.py（HTTPS）。
// 环境变量：MOCK_BASE=https://127.0.0.1:19443  FIXTURES=<样本目录>  SHOTS=<截图目录>
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const { chromium } = require('playwright');

const BASE = process.env.MOCK_BASE || 'https://127.0.0.1:19443';
const FIXTURES = process.env.FIXTURES || '/data/ai-dev-data/work/fixtures';
const SHOTS = process.env.SHOTS || '/tmp/renderer-0206-shots';

const fx = (n) => path.join(FIXTURES, n);
let pass = 0, fail = 0;
async function t(name, fn) {
  try { await fn(); console.log('  ✅', name); pass++; }
  catch (e) { console.log('  🔴', name, '->', e && e.message); fail++; }
}

(async () => {
  fs.mkdirSync(SHOTS, { recursive: true });
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  const uploadCalls = [];
  page.on('request', (req) => { if (req.url().endsWith('/files/upload')) uploadCalls.push(req.url()); });
  page.on('console', (m) => { if (m.type() === 'error') console.log('    [console.error]', m.text()); });

  await page.route('**/status', (route) => route.fulfill({
    status: 200, contentType: 'application/json',
    body: JSON.stringify({ session: 'fixture-a-2642', state: 'idle', readonly: false, owner: 'fixture', model: 'fixture-model' }),
  }));
  await page.route('**/events', (route) => route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
  await page.route('**/taskboard**', (route) => route.fulfill({ status: 404, body: '' }));

  await page.goto(`${BASE}/renderer-canary-sse.html`, { waitUntil: 'domcontentloaded' });
  // stats 是脚本作用域的 const，不挂在 window 上
  await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });

  // ── ① 入口：界面上看得见 —— 这正是 0.2.0.5 漏掉的那条 ────────────
  await t('① 上传入口在界面上可见（文件选择按钮），不是只有隐藏的 paste 监听器', async () => {
    assert.strictEqual(await page.locator('#fileLabel').isVisible(), true, '#fileLabel 必须可见');
    assert.ok((await page.locator('#fileLabel').textContent()).includes('文件'), '按钮要有可读文案');
    assert.strictEqual(await page.locator('#filePicker').count(), 1, '要有 <input type=file>');
    assert.strictEqual(await page.locator('#filePicker').getAttribute('multiple') !== null, true, '必须可多选');
    await page.screenshot({ path: path.join(SHOTS, '01-upload-entry-visible.png') });
  });

  // ── ② 文件选择：一次多个、不限类型 ──────────────────────────────
  // 视频用 webm(VP9/Opus)：headless chromium 是开源构建，不带 h264 专有解码器，
  // 传 mp4 只能证明「元素建出来了」，证明不了「真的能播」。mp4 另在 ③b 单独验降级。
  const picked = ['sample.png', 'sample.webm', 'sample.pdf', 'sample-small.txt', 'sample.docx', 'sample-unknown.bin'];
  await t('② 文件选择一次传 6 个不同类型（图片/视频/pdf/文本/office/未知二进制）', async () => {
    await page.setInputFiles('#filePicker', picked.map(fx));
    await page.waitForFunction(() => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= 6,
      null, { timeout: 60000 });
    const value = await page.inputValue('#humanInput');
    for (const name of picked) assert.ok(value.includes(name), `输入框应回填 ${name}`);
    assert.strictEqual(uploadCalls.length, 6, `应发生 6 次上传，实际 ${uploadCalls.length}`);
  });

  // ── ③ 正文区按 media_type 分派（不经 turn 分组的输入卡路径）──────
  await t('③ 正文区渲染：图片 <img>、视频 <video>、pdf <iframe>、文本 <pre> 内联', async () => {
    const text = await page.inputValue('#humanInput');
    await page.evaluate((body) => handleIncomingEvent({ kind: 'user_input', text: body, ts: Date.now() }), text);
    await page.waitForSelector('.attachments .attachment', { timeout: 20000 });
    await page.waitForFunction(() => document.querySelectorAll('.attachments .attachment').length >= 6, null, { timeout: 30000 });
    assert.strictEqual(await page.locator('.attachment img').count(), 1, '图片应内联 <img>');
    assert.strictEqual(await page.locator('.attachment video').count(), 1, '视频应内联 <video>');
    // 「真的能播」而不只是「元素在」：等元数据到位，断言时长与画面尺寸都是真的。
    const media = await page.locator('.attachment video').first().evaluate((v) => new Promise((resolve) => {
      const done = () => resolve({ duration: v.duration, w: v.videoWidth, h: v.videoHeight, readyState: v.readyState });
      if (v.readyState >= 1) return done();
      v.addEventListener('loadedmetadata', done, { once: true });
      setTimeout(done, 8000);
    }));
    assert.ok(media.duration > 0.5, `视频时长应可读，实际 ${media.duration}`);
    assert.ok(media.w === 320 && media.h === 240, `视频画面尺寸应为 320x240，实际 ${media.w}x${media.h}`);
    assert.strictEqual(await page.locator('.attachment iframe').count(), 1, 'pdf 应内联 <iframe>');
    assert.ok(await page.locator('.attachment pre').count() >= 1, '纯文本应内联 <pre>');
    const preText = await page.locator('.attachment pre').first().textContent();
    assert.ok(preText.includes('DingWei 0.2.0.6'), '文本内容应真的取回来');
    // 🔴 小文本不得被误标「已截断」：Range 覆盖整个文件时服务端也回 206，
    //    拿 206 当截断依据会让 1.7 KiB 的文件显示成截断（截图里一眼看见的那个）。
    assert.strictEqual(await page.locator('.attachments .attachment-note').count(), 0,
      '1736 字节的小文本不该出现截断说明');
    // 证据截图：三种内联各截一张元素图（正文区自身是滚动容器，整页截图截不到上半部）。
    for (const [i, tag] of [[0, 'image'], [1, 'video'], [2, 'pdf']]) {
      const box = page.locator('.attachments .attachment').nth(i);
      await box.scrollIntoViewIfNeeded();
      await box.screenshot({ path: path.join(SHOTS, `02a-inline-${tag}.png`) });
    }
  });

  // ── ④ 🔴 兜底分支：office 与未知二进制 ⇒ 下载链接，不是报错、不是裸 URL ──
  await t('④ 兜底：office 与未知二进制渲染成下载链接（<a download>），不报错也不留裸 URL', async () => {
    const links = page.locator('.attachment a.attachment-dl');
    const n = await links.count();
    assert.ok(n >= 2, `兜底下载链接至少 2 个（docx + bin），实际 ${n}`);
    const hrefs = await links.evaluateAll((els) => els.map((e) => e.getAttribute('href')));
    for (const h of hrefs) assert.ok(/^https:\/\/.*\/f\//.test(h), `下载链接必须指向 /f/ 直链：${h}`);
    const hasDownloadAttr = await links.evaluateAll((els) => els.every((e) => e.hasAttribute('download')));
    assert.ok(hasDownloadAttr, '下载链接必须带 download 属性');
    // 「不是报错」：正文里不得出现错误卡
    assert.strictEqual(await page.locator('.turn-error').count(), 0, '兜底不得走错误卡');
    await page.screenshot({ path: path.join(SHOTS, '02-body-rendered-all-types.png'), fullPage: true });
  });

  // ── ④a 🔴 接收方路径：只有 URL、没有上传响应（附录 B2 真正要解决的那一半）──
  //     刷新页面 ⇒ uploadedFileHints 清空 ⇒ 只能靠 URL 扩展名分派。
  //     这一条覆盖「不同 Agent 之间互传」：接收方从来没见过那个上传响应。
  await t('④a 接收方只拿到 URL（页面刷新后 hints 清空）仍按扩展名正确分派', async () => {
    const carried = await page.inputValue('#humanInput');
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    const hints = await page.evaluate(() => uploadedFileHints.size);
    assert.strictEqual(hints, 0, '刷新后本页上传记录必须是空的，否则这条测不到接收方路径');
    await page.evaluate((body) => handleIncomingEvent({ kind: 'user_input', text: body, ts: Date.now() }), carried);
    await page.waitForFunction(() => document.querySelectorAll('.attachments .attachment').length >= 6, null, { timeout: 30000 });
    assert.strictEqual(await page.locator('.attachment img').count(), 1, '.png 应按扩展名内联 <img>');
    assert.strictEqual(await page.locator('.attachment video').count(), 1, '.webm 应按扩展名内联 <video>');
    assert.strictEqual(await page.locator('.attachment iframe').count(), 1, '.pdf 应按扩展名内联 <iframe>');
    assert.ok(await page.locator('.attachment pre').count() >= 1, '.txt 应按扩展名内联 <pre>');
    const binLinks = await page.locator('.attachment a.attachment-dl[href$=".bin"]').count();
    assert.ok(binLinks >= 2, `.bin 必须走下载链接，实际 ${binLinks}`);
    assert.strictEqual(await page.locator('.attachments .attachment-note').count(), 0, '小文本不该报截断');
    await page.screenshot({ path: path.join(SHOTS, '02b-receiver-side-by-extension.png'), fullPage: true });
  });

  // ── ④b mp4：本浏览器解不了该编码时也要有出路，绝不留裸 URL ─────
  await t('④b mp4（本 chromium 无 h264 解码器）降级为下载链接，不报错、不留裸 URL', async () => {
    const before = await page.locator('.attachment').count();
    await page.evaluate(() => { document.getElementById('humanInput').value = ''; });
    await page.setInputFiles('#filePicker', [fx('sample.mp4')]);
    await page.waitForFunction(() => /\/f\//.test(document.getElementById('humanInput').value), null, { timeout: 60000 });
    const text = await page.inputValue('#humanInput');
    await page.evaluate((body) => handleIncomingEvent({ kind: 'user_input', text: body, ts: Date.now() }), text);
    await page.waitForFunction((n) => document.querySelectorAll('.attachment').length > n, before, { timeout: 30000 });
    const last = page.locator('.attachment').last();
    const hasVideo = await last.locator('video').count();
    const hasLink = await last.locator('a.attachment-dl').count();
    assert.ok(hasVideo + hasLink >= 1, 'mp4 必须要么内联要么给下载链接');
    assert.ok(hasLink >= 1, '解不了码时必须有下载链接兜底');
  });

  // ── ⑤ 拖放：一次拖入多个 ────────────────────────────────────────
  await t('⑤ 拖放一次拖入 2 个文件，两个都上传', async () => {
    const before = uploadCalls.length;
    const refsBefore = (await page.inputValue('#humanInput')).split('/f/').length - 1;
    const payload = ['sample.png', 'sample.pdf'].map((n) => ({
      name: n, type: n.endsWith('.png') ? 'image/png' : 'application/pdf',
      b64: fs.readFileSync(fx(n)).toString('base64'),
    }));
    await page.evaluate((files) => {
      const dt = new DataTransfer();
      for (const f of files) {
        const bin = Uint8Array.from(atob(f.b64), (c) => c.charCodeAt(0));
        dt.items.add(new File([bin], f.name, { type: f.type }));
      }
      dispatchEvent(new DragEvent('dragenter', { dataTransfer: dt, bubbles: true }));
      dispatchEvent(new DragEvent('drop', { dataTransfer: dt, bubbles: true }));
    }, payload);
    await page.waitForFunction((n) => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= n,
      refsBefore + 2, { timeout: 30000 });
    assert.strictEqual(uploadCalls.length, before + 2, '拖放的 2 个文件都应上传');
  });

  // ── ⑥ 粘贴：扩到任意类型（不再只收 image/*）──────────────────────
  await t('⑥ 粘贴一个 pdf（非图片）也上传 —— 原实现只接 image/*', async () => {
    const before = uploadCalls.length;
    const refsBefore = (await page.inputValue('#humanInput')).split('/f/').length - 1;
    const b64 = fs.readFileSync(fx('sample.pdf')).toString('base64');
    await page.evaluate((data) => {
      const bin = Uint8Array.from(atob(data), (c) => c.charCodeAt(0));
      const dt = new DataTransfer();
      dt.items.add(new File([bin], 'pasted.pdf', { type: 'application/pdf' }));
      document.getElementById('humanInput').dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }));
    }, b64);
    await page.waitForFunction((n) => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= n,
      refsBefore + 1, { timeout: 30000 });
    assert.strictEqual(uploadCalls.length, before + 1, '粘贴的 pdf 应上传');
    assert.ok((await page.inputValue('#humanInput')).includes('pasted.pdf'), '输入框应回填粘贴文件名');
  });

  // ── ⑦ 文本超限：截断 + 下载链接 ─────────────────────────────────
  await t('⑦ 大文本内联截断并给下载链接（不是整篇灌进正文）', async () => {
    await page.evaluate(() => { document.getElementById('humanInput').value = ''; });
    await page.setInputFiles('#filePicker', [fx('sample-large.txt')]);
    await page.waitForFunction(() => /\/f\//.test(document.getElementById('humanInput').value), null, { timeout: 60000 });
    const text = await page.inputValue('#humanInput');
    await page.evaluate((body) => handleIncomingEvent({ kind: 'assistant_text', text: body, turn_id: 'turn-large', ts: Date.now() }), text);
    await page.waitForSelector('.turn-group .attachments .attachment-note', { timeout: 30000 });
    const note = await page.locator('.attachments .attachment-note').last().textContent();
    assert.ok(note.includes('已截断'), `应给出截断说明，实际「${note}」`);
    const pre = await page.locator('.attachments pre').last().textContent();
    assert.ok(pre.length <= 262144, `内联文本不得超过上限，实际 ${pre.length}`);
    assert.ok(await page.locator('.attachments .attachment-dl').count() >= 3, '截断后必须另给下载链接');
    await page.screenshot({ path: path.join(SHOTS, '03-large-text-truncated.png'), fullPage: true });
  });

  // ── ⑧ 50 MiB 上限：发请求前就拒 ─────────────────────────────────
  await t('⑧ 超过 50 MiB（52428801 字节）在发请求前拒绝，不发上传请求', async () => {
    const before = uploadCalls.length;
    const kept = await page.inputValue('#humanInput');
    await page.setInputFiles('#filePicker', [fx('oversize-52428801.bin')]);
    await page.waitForTimeout(3000);
    assert.strictEqual(uploadCalls.length, before, '超限文件不得发起上传请求');
    assert.strictEqual(await page.inputValue('#humanInput'), kept, '上传失败不得改动输入框已有文字');
  });

  await browser.close();
  console.log(`\n通过 ${pass} / 失败 ${fail}   截图目录 ${SHOTS}`);
  process.exit(fail ? 1 : 0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
