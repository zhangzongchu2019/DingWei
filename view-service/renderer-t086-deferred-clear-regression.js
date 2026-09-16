// T-086 回归:重连时的清空必须【推迟到第一条重放事件真正到达】。
// 🔴 从待部署文件原样抽取被测代码求值,不复制粘贴。
const fs = require('fs'), assert = require('assert');
const src = fs.readFileSync('renderer-canary-sse.html', 'utf8');

const START = 'let pendingReconnectClear = false;';
const END = 'beginTimelineReplay();\n    }';
const i = src.indexOf(START); assert.ok(i > 0, '抽取失败:起点未命中');
const j = src.indexOf(END, i); assert.ok(j > i, '抽取失败:终点未命中');
const block = src.slice(i, j + END.length);
assert.ok(!/document|fetch|EventSource/.test(block), '🔴 抽取越界:片段内不该出现 DOM/网络调用');

// 替身:只提供被测逻辑真正用到的几个符号
function load(hardLimitMs) {
  let cleared = 0, replayBegun = 0;
  const fn = new Function('REPLAY_HARD_LIMIT_MS', 'resetConversation', 'beginTimelineReplay', `
    ${block}
    return { markStreamReconnect, consumeReconnectClear, cancelPendingReconnectClear,
             peek: () => pendingReconnectClear };
  `);
  const api = fn(hardLimitMs, () => cleared++, () => replayBegun++);
  return { api, counts: () => ({ cleared, replayBegun }) };
}

let pass = 0, fail = 0;
const t = (n, f) => { try { f(); console.log('  ✅', n); pass++; } catch (e) { console.log('  🔴', n, '->', e.message.split('\n')[0]); fail++; } };
const sleep = (ms) => new Promise(r => setTimeout(r, ms));

(async () => {
  console.log('T-086 推迟清空（源自 renderer-canary-sse.html 实际文本）');

  t('① 🔴 重连那一刻【不得】立刻清空', () => {
    const { api, counts } = load(1000);
    api.markStreamReconnect();
    assert.strictEqual(counts().cleared, 0, '重连时清空 ⇒ ring 空就白页,正是本次要消灭的');
    assert.strictEqual(api.peek(), true, '应挂起一次待清空');
  });

  t('② ring 有内容:第一条重放到达 ⇒ 此刻才清空(与旧行为等价)', () => {
    const { api, counts } = load(1000);
    api.markStreamReconnect();
    assert.strictEqual(api.consumeReconnectClear(), true, '第一条应触发清空');
    assert.strictEqual(api.consumeReconnectClear(), false, '🔴 只能清一次,不得每条都清');
  });

  await (async () => {
    const name = '③ 🔴 ring 为空:窗口内一条都没来 ⇒ 取消清空(历史保住)';
    try {
      const { api } = load(60);
      api.markStreamReconnect();
      await sleep(120);
      assert.strictEqual(api.peek(), false, '超时后必须取消挂起');
      assert.strictEqual(api.consumeReconnectClear(), false,
        '🔴 超时后【活事件】不得再触发清空 —— 否则只是把白页推迟,不是消除');
      console.log('  ✅', name); pass++;
    } catch (e) { console.log('  🔴', name, '->', e.message.split('\n')[0]); fail++; }
  })();

  await (async () => {
    const name = '④ 窗口【内】到达仍会清空(不能因为加了窗口就永不清空)';
    try {
      const { api } = load(200);
      api.markStreamReconnect();
      await sleep(50);
      assert.strictEqual(api.consumeReconnectClear(), true, '窗口内到达必须清空,否则重连会叠加重复内容');
      console.log('  ✅', name); pass++;
    } catch (e) { console.log('  🔴', name, '->', e.message.split('\n')[0]); fail++; }
  })();

  t('⑤ 连续两次重连:后一次重置窗口,不得沿用上一次的挂起', () => {
    const { api } = load(1000);
    api.markStreamReconnect();
    api.consumeReconnectClear();
    api.markStreamReconnect();
    assert.strictEqual(api.peek(), true, '第二次重连应重新挂起');
  });

  t('⑥ markStreamReconnect 仍会启动重放窗口', () => {
    const { api, counts } = load(1000);
    api.markStreamReconnect();
    assert.strictEqual(counts().replayBegun, 1);
  });

  console.log(`\n通过 ${pass} / 失败 ${fail}`);
  process.exit(fail ? 1 : 0);
})();
