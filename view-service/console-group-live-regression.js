// S-GROUP-LIVE:从 Go 页面抽取真实脚本求值，不依赖浏览器或复制生产实现。
const assert = require('assert');
const { agentRecord } = require('./fixture-agents');
const fs = require('fs');
const path = require('path');

const source = fs.readFileSync(path.join(__dirname, '..', 'internal', 'm8', 'console_auth.go'), 'utf8');
const start = source.indexOf('<script>'), end = source.indexOf('</script>', start);
assert.ok(start > 0 && end > start, '未找到控制台页面脚本');
let script = source.slice(start + '<script>'.length, end);
for (const re of [/^\s*refreshPlatform\(\);\s*$/m, /^\s*refreshAgents\(\);\s*$/m,
                  /^\s*refreshGroups\(\);\s*$/m, /^\s*setInterval\(refreshAgents,15000\);\s*$/m]) {
  const before = script;
  script = script.replace(re, '\n');
  assert.notStrictEqual(script, before, `页面自举锚点未命中: ${re}`);
}
if (process.argv.includes('--mutate=drop-fresh-online-overlay')) {
  const before = script;
  script = script.replace(
    'function agentTreeMemberOnline(member){var fresh=agentListSnapshot.find(function(agent){return agent.full_name===memberName(member);});return fresh?fresh.online===true:member.online===true;}',
    'function agentTreeMemberOnline(member){return member.online===true;}',
  );
  assert.notStrictEqual(script, before, '反向变异未改动任何生产字节');
}

function element(tag) {
  return {
    tag, children: [], style: {}, attrs: {}, dataset: {}, className: '', hidden: false, disabled: false,
    classList: { toggle() {}, contains() { return false; } },
    append(...nodes) { this.children.push(...nodes); },
    replaceChildren(...nodes) { this.children = nodes; },
    setAttribute(name, value) { this.attrs[name] = String(value); },
    insertBefore(node) { this.children.push(node); },
    querySelector() { return null; },
    remove() {},
  };
}
const ids = { a: element('nav'), tabs: element('div'), panes: element('div'), hint: element('div'),
  retryPlatform: element('button'), retryAgents: element('button'), agentNavMode: element('button'),
  'agents-status': element('div'), 'platform-items': element('div'), 'platform-status': element('div') };
const document = { createElement: element, getElementById: id => ids[id] || null };
let agents = [agentRecord('zzc-alpha-2642', true), agentRecord('zzc-unassigned-2642', true)];
const fetchImpl = url => {
  if (url !== '/api/console/agents') throw new Error(`unexpected fetch ${url}`);
  return Promise.resolve({ ok: true, json: () => Promise.resolve({ agents }), text: () => Promise.resolve('') });
};
const load = new Function('document', 'fetch', 'setInterval', 'setTimeout', 'clearTimeout', 'alert', 'encodeURIComponent', `
  ${script}
  return {
    refreshAgents,
    online: agentTreeMemberOnline,
    setGroups(value) { groupSnapshot=value; },
    getGroups() { return groupSnapshot.groups; },
    flatButtons() { agentNavMode='flat';renderAgentNav();return document.getElementById('a').children; },
  };
`);
const api = load(document, fetchImpl, () => { throw new Error('不得新增或运行第二个轮询'); },
  fn => { fn(); return 1; }, () => {}, () => {}, encodeURIComponent);

(async () => {
  const staleMember = { agent_id: 'agent-a', session_name: 'zzc-alpha-2642', online: true };
  const groups = [{ code: 'group-a', members: [staleMember] }];
  api.setGroups({ groups, owned_agents: [] });
  await api.refreshAgents();
  assert.strictEqual(api.online(staleMember), true, '前置:新鲜列表在线时应为在线');

  agents = [agentRecord('zzc-alpha-2642', false), agentRecord('zzc-unassigned-2642', true)];
  await api.refreshAgents();
  assert.strictEqual(api.online(staleMember), false,
    '新鲜列表已离线，但编组成员仍使用陈旧 online=true');
  assert.strictEqual(api.getGroups(), groups,
    'online 覆盖不得用普通 Agent 列表替换编组集合对象');
  assert.deepStrictEqual(api.getGroups()[0].members, [staleMember],
    'online 覆盖不得增删编组成员');

  const buttons = api.flatButtons();
  const findByTitle = (nodes, title) => {
    for (const node of nodes) {
      if (node.title === title) return node;
      const found = findByTitle(node.children || [], title);
      if (found) return found;
    }
    return null;
  };
  const alpha = findByTitle(buttons, 'zzc-alpha-2642');
  assert.ok(alpha && alpha.disabled === true,
    '阳性对照:普通 Agent 列表仍由同一份新鲜快照更新');
  assert.strictEqual((source.match(/setInterval\(refreshAgents,15000\)/g) || []).length, 1,
    '只能复用现有 15 秒轮询，不得新增轮询');
  console.log('PASS S-GROUP-LIVE fresh online overlay; members unchanged; existing flat list unchanged');
})().catch(error => { console.error(error.stack || error); process.exit(1); });
