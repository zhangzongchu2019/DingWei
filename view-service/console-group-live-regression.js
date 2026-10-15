// S-GROUP-LIVE:从 Go 页面抽取真实脚本求值，不依赖浏览器或复制生产实现。
const assert = require('assert');
const { agentRecord } = require('./fixture-agents');
const fs = require('fs');
const path = require('path');

const source = fs.readFileSync(path.join(__dirname, '..', 'internal', 'm8', 'console_auth.go'), 'utf8');
// Select the application block by a named declaration, not the first script tag
// (the preceding tag embeds the broker). Compile each actual named declaration
// to find its end without counting braces inside strings, comments or regexes.
function extract(goSource) {
  const blocks = Array.from(goSource.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script\s*>/g), m => m[1]);
  const matches = blocks.filter(block => /^[ \t]*async\s+function\s+refreshGroups\s*\(/m.test(block));
  assert.strictEqual(matches.length, 1, 'expected one Console application script defining refreshGroups');
  const block = matches[0];
  function declaration(name, isFunction) {
    const re = isFunction
      ? new RegExp('^[ \t]*(?:async\\s+)?function\\s+' + name + '\\s*\\(', 'm')
      : new RegExp('^[ \t]*(?:const|let|var)\\s+' + name + '\\b', 'm');
    const found = re.exec(block);
    assert.ok(found, 'actual Console declaration missing: ' + name);
    const lines = block.slice(found.index).split('\n');
    for (let end = 1; end <= lines.length; end++) {
      const candidate = lines.slice(0, end).join('\n');
      try { new (require('vm').Script)(candidate); return candidate; }
      catch (error) { if (!(error instanceof SyntaxError)) throw error; }
    }
    throw new Error('incomplete Console declaration: ' + name);
  }
  const state = ['tabs', 'agentSnapshot', 'agentListSnapshot', 'agentsRequestSeq', 'consoleFetchTimeoutMs'];
  const functions = ['dwShortName', 'memberName', 'memberShort', 'agentTreeMemberLabel',
    'agentTreeMemberOnline', 'agentTreeMemberDisabled', 'agentTreeCanOpen',
    'renderAgentList', 'renderAgentNav', 'agentsClock', 'applyAgentStale',
    'applyAgentSnapshot', 'refreshAgents', 'refreshGroups'];
  return state.map(name => declaration(name, false)).concat(functions.map(name => declaration(name, true))).join('\n');
}
let script = extract(source);
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
    querySelectorAll(selector) {
        assert.strictEqual(selector, 'button', 'unexpected fixture selector');
        const found = [];
        function visit(node) { (node.children || []).forEach(child => { if (child.tag === 'button') found.push(child); visit(child); }); }
        visit(this); return found;
      },
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
