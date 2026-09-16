// T-147:从真实 renderer 源码抽取贴图函数；不复制产品实现。
const fs = require('fs'), assert = require('assert');
const src = fs.readFileSync('renderer-canary-sse.html', 'utf8');
const start = src.indexOf('    function appendInputReference');
const end = src.indexOf("    humanInput.addEventListener('input'", start);
assert.ok(start > 0 && end > start, '未命中 T-147 产品函数');
const block = src.slice(start, end);
assert.ok(block.includes('uploadPastedImage') && block.includes('file-upload-ticket'), '抽取范围错误');

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
function load(humanInput, fetch) {
  const fn = new Function('humanInput', 'fetch', 'stats', 'Event', `${block}; return {uploadPastedImage};`);
  return fn(humanInput, fetch, {sessionName: 'agent-a'}, class Event {});
}
const response = (ok, body, status = ok ? 200 : 503) => ({ok, status, json: async () => body});

let pass = 0, fail = 0;
async function t(name, fn) {
  try { await fn(); console.log('  ✅', name); pass++; }
  catch (e) { console.log('  🔴', name, '->', e.message); fail++; }
}

(async () => {
  await t('① 图片上传成功后回填 dwfile 引用且保留原文字', async () => {
    const box = input();
    const calls = [];
    const fetch = async (url, init) => {
      calls.push({url, init});
      if (calls.length === 1) return response(true, {upload_url:'https://files/upload',ticket:'ticket',session:'agent-a'});
      return response(true, {file_ref:'dwfile:0123456789abcdef0123456789abcdef'});
    };
    await load(box, fetch).uploadPastedImage({type:'image/png',size:4,name:'p.png'});
    assert.strictEqual(box.value, '已有文字 dwfile:0123456789abcdef0123456789abcdef');
    assert.strictEqual(calls.length, 2);
    assert.strictEqual(calls[1].init.headers.Authorization, 'Bearer ticket');
  });
  await t('② 上传失败不改变输入框已有文字', async () => {
    const box = input('不可丢');
    const fetch = async () => response(false, {}, 503);
    await assert.rejects(load(box, fetch).uploadPastedImage({type:'image/png',size:4,name:'p.png'}));
    assert.strictEqual(box.value, '不可丢');
  });
  await t('③ 超过 16 MiB 在发请求前拒绝且不改文字', async () => {
    const box = input('保留'); let calls = 0;
    await assert.rejects(load(box, async () => { calls++; }).uploadPastedImage({type:'image/png',size:16*1024*1024+1,name:'p.png'}));
    assert.strictEqual(calls, 0); assert.strictEqual(box.value, '保留');
  });
  await t('④ 非图片在发请求前拒绝且不改文字', async () => {
    const box = input('普通粘贴'); let calls = 0;
    await assert.rejects(load(box, async () => { calls++; }).uploadPastedImage({type:'text/plain',size:4,name:'x.txt'}));
    assert.strictEqual(calls, 0); assert.strictEqual(box.value, '普通粘贴');
  });
  await t('⑤ 上传票据的 session 不属于当前 Agent 时拒绝且不改文字', async () => {
    const box = input('不可串席'); let calls = 0;
    const fetch = async () => {
      calls++;
      return response(true, {upload_url:'https://files/upload',ticket:'ticket',session:'other-agent'});
    };
    await assert.rejects(load(box, fetch).uploadPastedImage({type:'image/png',size:4,name:'p.png'}));
    assert.strictEqual(calls, 1, '错 session 票据不得继续上传');
    assert.strictEqual(box.value, '不可串席');
  });
  console.log(`\n通过 ${pass} / 失败 ${fail}`);
  process.exit(fail ? 1 : 0);
})();
