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
function load(humanInput, fetch, opts = {}) {
  const errors = [];
  const fn = new Function(
    'humanInput', 'fetch', 'stats', 'Event', 'FILE_MAX_UPLOAD_BYTES',
    'formatFileBytes', 'uploadedFileHints', 'handleIncomingEvent',
    `${block}; return {uploadOneFile, uploadFiles};`,
  );
  const api = fn(
    humanInput, fetch, { sessionName: 'agent-a', readonly: Boolean(opts.readonly) }, class Event {},
    LIMIT, (n) => `${n} B`, new Map(), (ev) => errors.push(ev),
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

(async () => {
  await t('① 🔴 任意类型都可上传（原实现只接 image/*），回填 §2 的 url 而不是 dwfile 引用', async () => {
    for (const f of [file(), file({ type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', name: 'a.docx' }),
                     file({ type: '', name: 'a.unknown' }), file({ type: 'video/mp4', name: 'a.mp4' })]) {
      const box = input('原文');
      const calls = [];
      const fetch = async (url, init) => {
        calls.push({ url, init });
        return calls.length === 1 ? response(true, ticket)
          : response(true, { url: 'https://dw2.cmjrt.com:56443/f/o/s/26/0918/abc.pdf', name: f.name, size: f.size, media_type: f.type }, 201);
      };
      await load(box, fetch).uploadOneFile(f);
      assert.strictEqual(box.value, `原文 https://dw2.cmjrt.com:56443/f/o/s/26/0918/abc.pdf (${f.name}, ${f.size} bytes)`);
      // 浏览器给不出类型时按二进制传，不猜、也不拒
      assert.strictEqual(calls[1].init.headers['Content-Type'], f.type || 'application/octet-stream');
    }
  });

  await t(`② 上限取自源码常量 ${LIMIT}（= 50 MiB），超限在发请求前拒绝且不改文字`, async () => {
    assert.strictEqual(LIMIT, 52428800, '附录 A4：50 MiB = 50<<20，不是 50×1000²');
    const box = input('保留'); let calls = 0;
    await assert.rejects(load(box, async () => { calls++; }).uploadOneFile(file({ size: LIMIT + 1 })));
    assert.strictEqual(calls, 0, '超限不得发起任何请求');
    assert.strictEqual(box.value, '保留');
    // 边界另一侧：正好等于上限必须放行
    const ok = input('');
    let n = 0;
    const fetch = async () => { n++; return n === 1 ? response(true, ticket) : response(true, { url: 'https://h/f/x.bin', name: 'x', size: LIMIT }, 201); };
    await load(ok, fetch).uploadOneFile(file({ size: LIMIT }));
    assert.strictEqual(n, 2, '正好 50 MiB 必须允许');
  });

  await t('③ 上传失败不改输入框，且把服务端 {"error"} 的可读原因带出来', async () => {
    const box = input('不可丢');
    const fetch = async (url) => (String(url).includes('ticket') ? response(true, ticket) : response(false, { error: '票据无效' }, 401));
    await assert.rejects(load(box, fetch).uploadOneFile(file()), /票据无效/);
    assert.strictEqual(box.value, '不可丢');
  });

  await t('④ 响应只给旧的 file_ref、没有 §2 要求的 url ⇒ 拒绝（不得回落旧字段）', async () => {
    const box = input('原文');
    const fetch = async (url) => (String(url).includes('ticket') ? response(true, ticket)
      : response(true, { file_ref: 'dwfile:0123456789abcdef0123456789abcdef', name: 'a.pdf', size: 601 }, 201));
    await assert.rejects(load(box, fetch).uploadOneFile(file()), /url/);
    assert.strictEqual(box.value, '原文');
  });

  await t('⑤ 上传票据的 session 不属于当前 Agent 时拒绝且不改文字', async () => {
    const box = input('不可串席'); let calls = 0;
    const fetch = async () => { calls++; return response(true, { ...ticket, session: 'other-agent' }); };
    await assert.rejects(load(box, fetch).uploadOneFile(file()));
    assert.strictEqual(calls, 1, '错 session 票据不得继续上传');
    assert.strictEqual(box.value, '不可串席');
  });

  await t('⑥ 多文件：中间一个失败，其余仍要传完（不因一个坏文件整批中断）', async () => {
    const box = input('');
    let seq = 0;
    const fetch = async (url) => {
      if (String(url).includes('ticket')) return response(true, ticket);
      seq += 1;
      if (seq === 2) return response(false, { error: '第二个坏了' }, 503);
      return response(true, { url: `https://h/f/o/s/26/0918/n${seq}.pdf`, name: `n${seq}.pdf`, size: 10 }, 201);
    };
    const api = load(box, fetch);
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

  console.log(`\n通过 ${pass} / 失败 ${fail}`);
  process.exit(fail ? 1 : 0);
})();
