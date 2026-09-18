// T-147 → 0.2.0.6 A 线：从真实 renderer 源码抽取上传函数；不复制产品实现。
//
// 0.2.0.6 把「只接 image/*、16 MiB、回填 dwfile: 引用」改成
// 「任意类型、50 MiB、回填契约 §2 的绝对 https url」，本件随之改写。
// 契约：tasks/v0.1-B 锚 d26bfd7002f70b7a9c9b5dcec6a1682ccbef974f §2 · §3.1 · 附录 A4
const fs = require('fs'), assert = require('assert');
const src = fs.readFileSync('renderer-canary-sse.html', 'utf8');
const start = src.indexOf('    function appendInputReference');
const end = src.indexOf("    humanInput.addEventListener('input'", start);
assert.ok(start > 0 && end > start, '未命中 T-147 产品函数');
const block = src.slice(start, end);
assert.ok(block.includes('uploadOneFile') && block.includes('file-upload-ticket'), '抽取范围错误');

// 上限值本身也从源码取，防「测试写死 52428800、产品写了别的数」这类假绿。
const limitMatch = src.match(/const FILE_MAX_UPLOAD_BYTES = (\d+);/);
assert.ok(limitMatch, '源码里必须有 FILE_MAX_UPLOAD_BYTES');
const LIMIT = Number(limitMatch[1]);

function input(value = '已有文字') {
  return {
    value, selectionStart: value.length, selectionEnd: value.length,
    setRangeText(text, start, end) {
      this.value = this.value.slice(0, start) + text + this.value.slice(end);
      this.selectionStart = this.selectionEnd = start + text.length;
    },
    dispatchEvent() {},
  };
}
// 产品把上传 POST 从 fetch 换成了 XMLHttpRequest —— 因为 fetch 不上报上传进度，
// 而业主 2026-09-18 验收第 1 条要的正是「已传/总字节」。
// ⇒ 替身也必须跟上：fetch 只服务票据请求，上传走 fakeXHR。
// 🔴 替身不跟着产品走，测的就是上一版产品（与今天 ④h 那场误诊同源）。
function fakeXHRFactory(onSend) {
  return class FakeXHR {
    constructor() {
      this.upload = { _h: {}, addEventListener(t, f) { this._h[t] = f; } };
      this._h = {};
      this.headers = {};
    }
    open(method, url) { this.method = method; this.url = url; }
    setRequestHeader(k, v) { this.headers[k] = v; }
    addEventListener(t, f) { this._h[t] = f; }
    send(body) {
      const res = onSend({ url: this.url, headers: this.headers, body });
      if (res.networkError) { setTimeout(() => this._h.error && this._h.error(), 0); return; }
      if (this.upload._h.progress) {
        this.upload._h.progress({ lengthComputable: true, loaded: body.size || 0, total: body.size || 0 });
      }
      if (this.upload._h.load) this.upload._h.load();
      this.status = res.status;
      this.responseText = typeof res.body === 'string' ? res.body : JSON.stringify(res.body);
      setTimeout(() => this._h.load && this._h.load(), 0);
    }
  };
}

// document / uploadQueue 的最小替身：只为 newUploadRow 能跑起来。
function fakeDom() {
  const make = () => ({
    className: '', style: {}, textContent: '', children: [], hidden: true,
    dataset: {}, title: '', src: '', alt: '', loading: '',
    addEventListener() {}, replaceWith() {},
    classList: { add() {}, remove() {}, toggle() {} },
    append(...xs) { this.children.push(...xs); },
    appendChild(x) { this.children.push(x); return x; },
    remove() {},
  });
  return { createElement: make, queue: make() };
}

function load(humanInput, fetch, opts = {}) {
  const errors = [];
  const dom = fakeDom();
  const fn = new Function(
    'humanInput', 'fetch', 'stats', 'Event', 'FILE_MAX_UPLOAD_BYTES',
    'formatFileBytes', 'uploadedFileHints', 'handleIncomingEvent',
    'XMLHttpRequest', 'document', 'uploadQueue', 'setTimeout',
    // 抽取范围里现在也含 renderDraftAttachments(业主验收第 2 条),它引用这几个符号 ——
    // 一并注入,让被测的仍是**产品那一份实现**,而不是替换成空函数糊过去。
    'FILE_URL_RE', 'trimUrlTail', 'isOwnFileUrl', 'draftAttachments', 'fileNameFromUrl', 'draftIconFor',
    `${block}; return {uploadOneFile, uploadFiles};`,
  );
  const api = fn(
    humanInput, fetch, { sessionName: 'agent-a', readonly: Boolean(opts.readonly) }, class Event {},
    LIMIT, (n) => `${n} B`, new Map(), (ev) => errors.push(ev),
    fakeXHRFactory(opts.onUpload || (() => ({ status: 503, body: {} }))),
    dom, dom.queue, setTimeout,
    /https?:\/\/[^\s<>"'`）】]+/gu,
    (raw) => String(raw).replace(/[.,;:!?，。；：！？、)\]}」』>]+$/u, ''),
    (raw) => /^https:\/\/[^/]+\/f\//u.test(String(raw)),
    dom.queue,
    (u) => String(u).split('/').pop(),
    () => '📄',
  );
  return { ...api, errors };
}
const response = (ok, body, status = ok ? 200 : 503) => ({ ok, status, json: async () => body });
const ticket = { upload_url: 'https://h/files/upload', ticket: 'ticket', session: 'agent-a' };
const file = (over = {}) => ({ type: 'application/pdf', size: 601, name: 'a.pdf', ...over });

let pass = 0, fail = 0;
async function t(name, fn) {
  try { await fn(); console.log('  ✅', name); pass++; }
  catch (e) { console.log('  🔴', name, '->', e.message); fail++; }
}

// 票据端点统一替身：只回票据，不再承载上传响应（上传走 onUpload）。
const ticketFetch = (override) => async () => response(true, { ...ticket, ...(override || {}) });

(async () => {
  await t('① 🔴 任意类型都可上传（原实现只接 image/*），回填 §2 的 url 而不是 dwfile 引用', async () => {
    for (const f of [file(), file({ type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', name: 'a.docx' }),
                     file({ type: '', name: 'a.unknown' }), file({ type: 'video/mp4', name: 'a.mp4' })]) {
      const box = input('原文');
      let seen = null;
      const api = load(box, ticketFetch(), {
        onUpload: (req) => { seen = req; return { status: 201, body: { url: 'https://dw2.cmjrt.com:56443/f/o/s/26/0918/abc.pdf', name: f.name, size: f.size, media_type: f.type } }; },
      });
      await api.uploadOneFile(f);
      assert.strictEqual(box.value, `原文 https://dw2.cmjrt.com:56443/f/o/s/26/0918/abc.pdf (${f.name}, ${f.size} bytes)`);
      // 浏览器给不出类型时按二进制传，不猜、也不拒
      assert.strictEqual(seen.headers['Content-Type'], f.type || 'application/octet-stream');
      assert.strictEqual(seen.headers['X-DingWei-Session'], 'agent-a');
    }
  });

  await t(`② 上限取自源码常量 ${LIMIT}（= 50 MiB），超限在发请求前拒绝且不改文字`, async () => {
    assert.strictEqual(LIMIT, 52428800, '附录 A4：50 MiB = 50<<20，不是 50×1000²');
    const box = input('保留');
    let calls = 0;
    await assert.rejects(load(box, async () => { calls++; }).uploadOneFile(file({ size: LIMIT + 1 })));
    assert.strictEqual(calls, 0, '超限不得发起任何请求');
    assert.strictEqual(box.value, '保留');
    // 边界另一侧：正好等于上限必须放行
    const ok = input('');
    let uploads = 0;
    await load(ok, ticketFetch(), {
      onUpload: () => { uploads++; return { status: 201, body: { url: 'https://h/f/x.bin', name: 'x', size: LIMIT } }; },
    }).uploadOneFile(file({ size: LIMIT }));
    assert.strictEqual(uploads, 1, '正好 50 MiB 必须允许');
  });

  await t('③ 上传失败不改输入框，且把服务端 {"error"} 的可读原因带出来', async () => {
    const box = input('不可丢');
    const api = load(box, ticketFetch(), { onUpload: () => ({ status: 401, body: { error: '票据无效' } }) });
    await assert.rejects(api.uploadOneFile(file()), /票据无效/);
    assert.strictEqual(box.value, '不可丢');
  });

  await t('④ 响应只给旧的 file_ref、没有 §2 要求的 url ⇒ 拒绝（不得回落旧字段）', async () => {
    const box = input('原文');
    const api = load(box, ticketFetch(), {
      onUpload: () => ({ status: 201, body: { file_ref: 'dwfile:0123456789abcdef0123456789abcdef', name: 'a.pdf', size: 601 } }),
    });
    await assert.rejects(api.uploadOneFile(file()), /url/);
    assert.strictEqual(box.value, '原文');
  });

  await t('⑤ 上传票据的 session 不属于当前 Agent 时拒绝且不改文字', async () => {
    const box = input('不可串席');
    let uploads = 0;
    const api = load(box, ticketFetch({ session: 'other-agent' }), { onUpload: () => { uploads++; return { status: 201, body: {} }; } });
    await assert.rejects(api.uploadOneFile(file()));
    assert.strictEqual(uploads, 0, '错 session 票据不得继续上传');
    assert.strictEqual(box.value, '不可串席');
  });

  await t('⑥ 多文件：中间一个失败，其余仍要传完（不因一个坏文件整批中断）', async () => {
    const box = input('');
    let seq = 0;
    const api = load(box, ticketFetch(), {
      onUpload: () => {
        seq += 1;
        if (seq === 2) return { status: 503, body: { error: '第二个坏了' } };
        return { status: 201, body: { url: `https://h/f/o/s/26/0918/n${seq}.pdf`, name: `n${seq}.pdf`, size: 10 } };
      },
    });
    await api.uploadFiles([file({ name: 'n1.pdf' }), file({ name: 'n2.pdf' }), file({ name: 'n3.pdf' })]);
    assert.strictEqual((box.value.match(/https:\/\/h\/f\//g) || []).length, 2, '成功的两个都要回填');
    assert.strictEqual(api.errors.length, 1, '失败的那个要报一次错');
    assert.ok(api.errors[0].text.includes('第二个坏了'), '错误文案要带服务端可读原因');
  });

  await t('⑦ 只读会话不上传；空文件在发请求前拒绝', async () => {
    const box = input('只读');
    const ro = load(box, async () => { throw new Error('只读不该发请求'); }, { readonly: true });
    await ro.uploadFiles([file()]);
    assert.strictEqual(box.value, '只读');
    assert.strictEqual(ro.errors.length, 1);
    let calls = 0;
    await assert.rejects(load(input(), async () => { calls++; }).uploadOneFile(file({ size: 0 })));
    assert.strictEqual(calls, 0);
  });

  // 🔴 业主 2026-09-18 验收第 1 条的单元级守卫：上传各阶段必须把状态报出去。
  await t('⑧ 上传全程必须有进度回调：票据等待 → 字节进度 → 完成', async () => {
    const box = input('');
    const seenStates = [];
    const row = {
      waiting: (t2) => seenStates.push(`waiting:${t2}`),
      progress: (l, t2) => seenStates.push(`progress:${l}/${t2}`),
      done: (sz) => seenStates.push(`done:${sz}`),
      failed: (r) => seenStates.push(`failed:${r}`),
    };
    const api = load(box, ticketFetch(), {
      onUpload: () => ({ status: 201, body: { url: 'https://h/f/o/s/26/0918/p.pdf', name: 'p.pdf', size: 601 } }),
    });
    await api.uploadOneFile(file(), row);
    assert.ok(seenStates.some((x) => x.startsWith('waiting:')), `缺票据阶段反馈: ${seenStates}`);
    // 🔴 产品在开跑前会先写一次 progress(0, size) 作为初始值 —— 那一条**不算**字节进度。
    //    必须出现 loaded>0 的那一次,它只可能来自 xhr.upload 的 progress 监听器。
    //    去掉那个监听器 ⇒ 本断言红(浏览器级验不到:同机 mock 一个 chunk 就发完)。
    assert.ok(seenStates.some((x) => /^progress:([1-9]\d*)\//.test(x)),
      `缺真实字节进度(只有初始 0 不算): ${seenStates}`);
    assert.ok(seenStates.some((x) => x.startsWith('done:')), `缺完成反馈: ${seenStates}`);
    assert.ok(seenStates.indexOf('done:601') > seenStates.findIndex((x) => x.startsWith('progress:')),
      `顺序不对（完成应在进度之后）: ${seenStates}`);
  });

  console.log(`\n通过 ${pass} / 失败 ${fail}`);
  process.exit(fail ? 1 : 0);
})();
