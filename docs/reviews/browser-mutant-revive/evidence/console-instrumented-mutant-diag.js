// Console browser harness: serves the real consolePageHTML embedded in
// internal/m8/console_auth.go and replaces only its HTTP dependencies.
const assert = require('assert');
const { agentRecord } = require('./fixture-agents');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const GO_SOURCE = path.join(__dirname, '..', 'internal', 'm8', 'console_auth.go');
// 业主 2026-09-24 批的版本角标:值必须来自服务端。夹具喂一个明显不是真版本的号,
// 这样「显示的是服务端喂的那一版」与「显示的是写死的字符串」两种情况【读数不同】。
const HARNESS_VERSION = '7.7.7.7-harness';
const HARNESS_COMMIT = 'c0ffee1234567890';
const START = 'const consolePageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleAgents';
const MUTATION = process.argv.find((arg) => arg.startsWith('--mutate='))?.slice(9) || '';
const CASE_FILTER = process.argv.find((arg) => arg.startsWith('--case='))?.slice(7) || '';
const CONDITION_WAIT_MS = 30000;
let caseFilterReached = false;

function consoleHTMLFromProductionSource() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(START);
  assert.notStrictEqual(start, -1, 'consolePageHTML start marker missing');
  const contentStart = start + START.length;
  const end = source.indexOf(END, contentStart);
  assert.notStrictEqual(end, -1, 'consolePageHTML end marker missing');
  let html = source.slice(contentStart, end);
  function mutate(from, to) {
    const next = html.replace(from, to);
    if (next === html) throw new Error(`mutation anchor missing: ${from}`);
    html = next;
  }
  if (MUTATION === 'stale-groups') html = html.replace('if(generation!==groupRequestGeneration)return null;', '');
  if (MUTATION === 'expose-group-code') html = html.replace("box.append(copy);", "box.append(copy);var leaked=document.createElement('code');leaked.className='group-code';leaked.textContent=group.code;box.append(leaked);");
  if (MUTATION === 'manual-agent-fields') html = html.replace("option.textContent=agent.session_name;option.dataset.keyId=agent.key_id||'';select.append(option)", "option.textContent=agent.id;option.dataset.keyId=agent.key_id||'';select.append(option)");
  if (MUTATION === 'legacy-specs-entry') html = html.replace("if(item.id==='specs') a.onclick=function(ev){ev.preventDefault();openSpecsWorkspace();};", "");
  if (MUTATION === 'silent-group-load') html = html.replace("groupStatus(message);if(source==='retry')groupFeedback('groupRetryFeedback',message);", "groupStatus('');if(source==='retry')groupFeedback('groupRetryFeedback','');");
  if (MUTATION === 'silent-platform-load') html = html.replace("status.textContent='工作台加载失败: '+String(err&&err.name==='AbortError'?'请求超时':err&&err.message||err);", "status.textContent='';");
  if (MUTATION === 'silent-agent-load') html = html.replace("status.textContent='Agent 列表加载失败: '+String(err&&err.name==='AbortError'?'请求超时':err&&err.message||err);", "status.textContent='';");
  if (MUTATION === 'silent-offline-tab') html = html.replace("state.textContent=online?'':'离线，暂不可交互';", "state.textContent='';").replace("offlineReason.textContent='该 Agent 已离线，暂不可交互';", "offlineReason.textContent='';");
  if (MUTATION === 'disable-platform-tabs-on-agent-refresh') html = html.replace("if(String(session).startsWith('__')) return;", '');
  if (MUTATION === 'keep-created-group-name') html = html.replace(".then(function(ok){if(ok)input.value='';});", ';');
  if (MUTATION === 'reset-group-leader-selection') html = html.replace("if(groupLeaderSelection&&Array.from(leaders.options).some(function(o){return o.value===groupLeaderSelection;}))leaders.value=groupLeaderSelection;", '');
  if (MUTATION === 'full-code-join-agent-names') html = html.replace("o.textContent=groupShortName(agent.session_name);codeJoin.append(o);", "o.textContent=agent.session_name;codeJoin.append(o);");
  if (MUTATION === 'filter-offline-group-candidates') {
    mutate("if(!owned.length){", "var online=owned.filter(function(a){return a.online;});if(!online.length){");
    mutate("}else{owned.forEach(function(agent){var o=document.createElement('option');o.value=agent.id;o.textContent=groupShortName(agent.session_name)+(agent.online?'':'（离线）');codeJoin.append(o);", "}else{online.forEach(function(agent){var o=document.createElement('option');o.value=agent.id;o.textContent=groupShortName(agent.session_name);codeJoin.append(o);");
    mutate("var candidates=owned.filter(function(a){return !memberIDs.has(a.id);});", "var candidates=owned.filter(function(a){return a.online&&!memberIDs.has(a.id);});");
  }
  if (MUTATION === 'preserve-stale-load-data') {
    mutate("}catch(err){renderPlatform({items:[],role:document.documentElement.dataset.platformRole||''});status.textContent='工作台加载失败: '", "}catch(err){status.textContent='工作台加载失败: '");
  }
  // R-V2-0105-P1-LIST-UPLOAD 六刀：失败改回清空／去成功侧序号守卫／去失败侧序号守卫／在线点不加过期样式／首次失败也标过期／401 也当过期
  if (MUTATION === 'clear-agents-on-failure') mutate("else{agentsStale=true;applyAgentStale();}", "else{agentsStale=false;agentListSnapshot=[];renderAgentList([]);}");
  if (MUTATION === 'agents-ignore-request-order') mutate("if(seq<agentsLastOkSeq)return;\n    var agents=x.agents;", "var agents=x.agents;");
  if (MUTATION === 'agents-stale-ignore-order') mutate("if(seq<agentsLastOkSeq)return;\n    var authFailed", "var authFailed");
  if (MUTATION === 'agents-stale-no-dot') mutate("var stale=agentsStale&&!b.disabled;", "var stale=false;");
  if (MUTATION === 'agents-stale-first-failure') mutate("if(authFailed||!agentsLastSuccessAt){", "if(authFailed){");
  if (MUTATION === 'agents-stale-on-auth') mutate("var authFailed=httpStatus===401||httpStatus===403;", "var authFailed=false;");
  if (MUTATION === 'show-retry-after-success') {
    mutate("status.textContent=document.getElementById('platform-items').childElementCount?'':'没有可用工作台入口';retry.hidden=true;", "status.textContent=document.getElementById('platform-items').childElementCount?'':'没有可用工作台入口';retry.hidden=false;");
    mutate("status.textContent=agents.length?'':'没有自己的 Agent';retry.hidden=true;", "status.textContent=agents.length?'':'没有自己的 Agent';retry.hidden=false;");
  }
  if (MUTATION === 'show-retry-on-empty') {
    mutate("status.textContent=document.getElementById('platform-items').childElementCount?'':'没有可用工作台入口';retry.hidden=true;", "status.textContent=document.getElementById('platform-items').childElementCount?'':'没有可用工作台入口';retry.hidden=document.getElementById('platform-items').childElementCount>0;");
    mutate("status.textContent=agents.length?'':'没有自己的 Agent';retry.hidden=true;", "status.textContent=agents.length?'':'没有自己的 Agent';retry.hidden=agents.length>0;");
  }
  // S-BROWSER-MUTANT-REVIVE：unhandled-group-action 复活。旧锚（status.textContent='编组操作失败: …'）
  // 已随产品 groupAction 重构为 groupFeedback（console_auth.go:797）而漂移，静默 replace 假绿
  // （S-BROWSER-FOLLOWUP REPORT §四披露）。重定锚到现行 catch 路径：网络失败时不再写
  // 「请求失败: 」反馈而是抛出未处理异常；格3 expectText('#groupNameFeedback','请求失败:')
  // 的业务断言红（条件等待超时被吞、断言本身不改）。mutate 锚缺失即 throw，与其它刀同口径。
  if (MUTATION === 'unhandled-group-action') {
    mutate("groupFeedback(feedback||'group-status','请求失败: '+String(e&&e.message||e));return false;", "throw e;");
  }
  // S-BROWSER-FOLLOWUP：格2（零组审计流程）判别力刀。静默 groupAction 的 HTTP 错误反馈：
  // 审计流程的「该编组已关闭添加」与格3 的失败文案均永不出现（与上一把 catch 路径刀互补）。
  if (MUTATION === 'silent-group-action-feedback') {
    mutate("groupFeedback(feedback||'group-status',msg||('HTTP '+r.status));return false;", "return false;");
  }
  if (MUTATION === 'legacy-group-deadlock') {
    html = html.replace('detail.before(retryArea);', 'detail.append(retryArea);');
  }
  // 🔴 阴性对照:把角标显示值写死。写死之后页面显示的就不再是服务端喂进来的那一版,
  //    下面那条断言必须红 —— 否则这条判据等于没立。
  // R-V2-0102-GROUP-FIXES B 三刀：去添加编组预检／去清洗／groupAction 不读 JSON 的 message。
  if (MUTATION === 'group-no-precheck') mutate("if(!NEW_GROUP_RE.test(name)){groupFeedback('groupNameFeedback',GROUP_NAME_RULE_TEXT);return;}", '');
  if (MUTATION === 'group-no-normalize') mutate("name=normalizeGroupName(input.value),", "name=input.value.trim(),");
  if (MUTATION === 'group-json-message-ignored') mutate("if(j&&typeof j.message==='string'&&j.message)msg=j.message;", '');
  if (MUTATION === 'hardcoded-version') mutate('>{{CONSOLE_VERSION_TEXT}}<', '>0.2.1.68<');
  // 🔴 版本角标由服务端填值。夹具在这里【故意喂一个不可能是真版本的号】:
  //    若哪天有人把版本号写死在模板里,页面显示的就不会是这个号 ⇒ 下面那条断言会红。
  html = html.split('{{CONSOLE_VERSION_TEXT}}').join(HARNESS_VERSION + ' · ' + HARNESS_COMMIT.slice(0, 7))
    .split('{{CONSOLE_VERSION}}').join(HARNESS_VERSION)
    .split('{{CONSOLE_COMMIT}}').join(HARNESS_COMMIT);
  if (MUTATION && !['group-no-precheck','group-no-normalize','group-json-message-ignored','hardcoded-version','stale-groups','expose-group-code','manual-agent-fields','legacy-group-entry','legacy-specs-entry','silent-group-load','silent-platform-load','silent-agent-load','silent-offline-tab','disable-platform-tabs-on-agent-refresh','keep-created-group-name','reset-group-leader-selection','full-code-join-agent-names','filter-offline-group-candidates','preserve-stale-load-data','show-retry-after-success','show-retry-on-empty','restore-inert-agents-entry','unhandled-group-action','legacy-group-deadlock','clear-agents-on-failure','agents-ignore-request-order','agents-stale-ignore-order','agents-stale-no-dot','agents-stale-first-failure','agents-stale-on-auth','silent-group-action-feedback'].includes(MUTATION)) throw new Error(`unknown mutation: ${MUTATION}`);
  return html;
}

function listen(server) {
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => resolve(server.address().port));
  });
}

async function main() {
  const html = consoleHTMLFromProductionSource();
  const agents = [
    agentRecord('owner-alpha-2642', true),
    agentRecord('owner-offline-2642', false),
  ];
  let raceGroupResponses = false;
  let raceGroupRequest = 0;
  let failGroupLoad = '';
  let failGroupAction = '';
  let slowGroupLoad = false;
  let emptyGroupInventory = false;
  let auditGroupMode = false;
  let auditGroup = null;
  let groupGETs = 0;
  let failPlatformLoad = '';
  let failAgentsLoad = '';
  let agentGETs = 0;
  const agentsQueue = [];
  let includeOfflineOwned = false;
  const groupWrites = [];
  const groupJoinWrites = [];
  function groupPayload(closed) {
    const ownedAgents = [{ id: 'agent-a', key_id: 'fixture-key-a', session_name: 'owner-alpha-2642', online: true }, { id: 'agent-b', key_id: 'fixture-key-b', session_name: 'owner-beta-2642', online: true }];
    if (includeOfflineOwned) ownedAgents.push({ id: 'agent-c', key_id: 'fixture-key-c', session_name: 'owner-gamma-2642', online: false });
    return { principal: 'owner-a', owned_agents: ownedAgents, groups: [{
      code: 'team-001AAAAAAAAAAAAAAAAAAAAAAAA', short_name: 'team-001', creator_principal: 'owner-a', owner_display_name: '业主甲', can_manage: true, additions_closed: closed,
      leader_key_id: 'fixture-key-a', leader_session_name: 'owner-alpha-2642',
      members: [{ agent_id: 'agent-a', key_id: 'fixture-key-a', session_name: 'owner-alpha-2642', online: true }],
    }] };
  }
  const server = http.createServer((req, res) => {
    if (req.url === '/console') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      res.end(html);
      return;
    }
    if (req.url === '/api/console/agents') {
      agentGETs += 1;
      if (agentsQueue.length) { agentsQueue.shift()(res); return; }
      if (failAgentsLoad === '401') { res.writeHead(401, {'Content-Type':'text/plain'}); res.end('fixture login required'); return; }
      if (failAgentsLoad === '500') { res.writeHead(500, {'Content-Type':'text/plain'}); res.end('fixture agents unavailable'); return; }
      if (failAgentsLoad === 'timeout') { setTimeout(() => { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify({ agents })); }, 200); return; }
      if (failAgentsLoad === 'empty') { res.writeHead(200, {'Content-Type':'application/json'}); res.end('{"agents":[]}'); return; }
      res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
      res.end(JSON.stringify({ agents }));
      return;
    }
    if (req.url === '/api/console/platform') {
      if (failPlatformLoad === '500') { res.writeHead(500, {'Content-Type':'text/plain'}); res.end('fixture platform unavailable'); return; }
      if (failPlatformLoad === 'timeout') { setTimeout(() => { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify({role:'management',actions:[],items:[]})); }, 200); return; }
      if (failPlatformLoad === 'empty') { res.writeHead(200, {'Content-Type':'application/json'}); res.end('{"role":"management","actions":[],"items":[]}'); return; }
      const management = String(req.headers.cookie || '').includes('fixture-role=management');
      res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
      res.end(JSON.stringify({
        role: management ? 'management' : 'owner',
        actions: management ? ['read', 'submit', 'add', 'edit', 'delete', 'approve'] : ['read', 'submit'],
        items: [
          ...(MUTATION === 'restore-inert-agents-entry' ? [{ id: 'agents', kind: 'agents', title: 'Agents', href: '#agents', status: 'approved' }] : []),
          { id: 'specs', kind: 'spec', title: 'Specs', href: MUTATION === 'legacy-specs-entry' ? '/console/specs' : '#specs', ...(MUTATION === 'legacy-specs-entry' ? {target:'_blank'} : {}), status: 'approved' },
          { id: 'task-board', kind: 'task-board', title: '任务管理', href: '#task-board', status: 'approved' },
          ...(management ? [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 编组', href: MUTATION === 'legacy-group-entry' ? '/admin/agent-groups' : '#agent-groups', ...(MUTATION === 'legacy-group-entry' ? {target:'_blank'} : {}), status: 'approved' }] : []),
        ],
      }));
      return;
    }
    // The real approval-list API returns an empty requests array when no grants exist.
    if (req.method === 'GET' && /^\/api\/console\/agents\/[^/]+\/spec-grant-requests$/.test(req.url)) {
      res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
      res.end(JSON.stringify({ requests: [] })); return;
    }
    if (req.url === '/api/console/hosted/options') { res.writeHead(200, { 'content-type': 'application/json' }); res.end(JSON.stringify({ nodes: [], combos: [], accounts: [], plans: [], quota: { remaining: {} } })); return; } // G3 托管区的挂载请求;本件不测它,只让桩认得路由
    if (req.url === '/api/console/enroll/applications') { res.writeHead(200, { 'content-type': 'application/json' }); res.end(JSON.stringify({ applications: [] })); return; } // G3 新增面板的挂载请求;本件不测它,只让桩认得这条路由
    if (req.url === '/api/console/agent-groups') {
      if (req.method === 'POST') {
        if (failGroupAction === 'network') { req.socket.destroy(); return; }
        if (failGroupAction === 'json400') { res.writeHead(400, {'Content-Type':'application/json'}); res.end(JSON.stringify({error:'group_name_invalid',message:'桩：服务端给的中文原因'})); return; }
        if (failGroupAction === '500') { res.writeHead(500, {'Content-Type':'text/plain'}); res.end('fixture create denied'); return; }
        let raw=''; req.on('data',(c)=>{raw+=c}); req.on('end',()=>{const body=JSON.parse(raw);groupWrites.push({url:req.url,body});if(auditGroupMode&&auditGroup&&body.short_name===auditGroup.short_name){res.writeHead(409,{'Content-Type':'text/plain; charset=utf-8'});res.end(`团队名称不可用；建议名称：${body.short_name}1、${body.short_name}2`);return;}if(auditGroupMode){auditGroup={...groupPayload(false).groups[0],short_name:body.short_name,members:[groupPayload(false).groups[0].members[0]]};}res.writeHead(201,{'Content-Type':'application/json'});res.end(JSON.stringify(auditGroupMode?auditGroup:groupPayload(false).groups[0]));}); return;
      }
      groupGETs += 1;
      if (failGroupLoad === '500') { res.writeHead(500, {'Content-Type':'text/plain'}); res.end('fixture groups unavailable'); return; }
      if (failGroupLoad === 'shape') { res.writeHead(200, {'Content-Type':'application/json'}); res.end('{"owned_agents":"wrong","groups":[]}'); return; }
      if (failGroupLoad === 'timeout') { setTimeout(() => { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify(groupPayload(false))); }, 200); return; }
      if (failGroupLoad === 'empty') { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify({principal:'owner-a',owned_agents:[],groups:[]})); return; }
      if (auditGroupMode) { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify({principal:'owner-a',owned_agents:groupPayload(false).owned_agents,groups:auditGroup?[auditGroup]:[]})); return; }
      if (emptyGroupInventory) { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify({principal:'owner-a',owned_agents:[],groups:[]})); return; }
      const send = (closed) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(groupPayload(closed))); };
      if (slowGroupLoad) { slowGroupLoad=false; setTimeout(() => send(false), 120); return; }
      if (raceGroupResponses) {
        raceGroupRequest += 1;
        if (raceGroupRequest === 1) { setTimeout(() => send(false), 150); return; }
        send(true); return;
      }
      send(false);
      return;
    }
    if (req.url === '/api/console/agent-groups/team-001AAAAAAAAAAAAAAAAAAAAAAAA/members' && req.method === 'POST') {
      let raw = '';
      req.on('data', (chunk) => { raw += chunk; });
      req.on('end', () => {
        const body = JSON.parse(raw); groupJoinWrites.push({ url: req.url, body });
        if (auditGroupMode && auditGroup && auditGroup.members.some((member) => member.agent_id === body.agent_id)) {
          res.writeHead(409, {'Content-Type':'text/plain; charset=utf-8'}); res.end('该 Agent 已在该编组中'); return;
        }
        if (auditGroupMode && auditGroup && auditGroup.additions_closed) {
          res.writeHead(409, {'Content-Type':'text/plain; charset=utf-8'}); res.end('该编组已关闭添加'); return;
        }
        if (auditGroupMode && auditGroup && !auditGroup.members.some((member) => member.agent_id === body.agent_id)) {
          auditGroup.members.push({agent_id:'agent-b',key_id:'fixture-key-b',session_name:'owner-beta-2642'});
        }
        res.writeHead(204);
        res.end();
      });
      return;
    }
    if (req.url === '/api/console/agent-groups/missing-1AAAAAAAAAAAAAAAAAAAAAAA/members' && req.method === 'POST' && auditGroupMode) {
      res.writeHead(404, {'Content-Type':'text/plain; charset=utf-8'}); res.end('组码不存在'); return;
    }
    if (req.url === '/api/console/agent-groups/team-001AAAAAAAAAAAAAAAAAAAAAAAA/members/agent-b' && req.method === 'DELETE' && auditGroupMode) {
      auditGroup.members = auditGroup.members.filter((member) => member.agent_id !== 'agent-b'); res.writeHead(204); res.end(); return;
    }
    if (req.url === '/api/console/agent-groups/team-001AAAAAAAAAAAAAAAAAAAAAAAA/additions' && req.method === 'POST' && auditGroupMode) {
      let raw=''; req.on('data',(chunk)=>{raw+=chunk}); req.on('end',()=>{auditGroup.additions_closed=JSON.parse(raw).closed;res.writeHead(204);res.end();}); return;
    }
    if (req.url === '/api/console/agent-groups/team-001AAAAAAAAAAAAAAAAAAAAAAAA' && req.method === 'DELETE' && auditGroupMode) {
      auditGroup = null; res.writeHead(204); res.end(); return;
    }
    if (req.url === '/console/specs?embedded=1') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end('<!doctype html><title>DingWei Specs</title><h1>我的 spec</h1><button id=specsProbe onclick="this.textContent=\'clicked\'">probe</button>'); return; }
    if (req.url && req.url.startsWith('/view/')) {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
      res.end('<!doctype html><title>fixture agent view</title>');
      return;
    }
    if (req.url === '/favicon.ico') { res.writeHead(204); res.end(); return; }
    res.writeHead(404);
    res.end('not found');
  });

  const port = await listen(server);
  const launchOptions = { headless: true };
  if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
  const browser = await chromium.launch(launchOptions);
  let passed = 0;
  const failures = [];
    const pageErrors = [];
    const consoleErrors = [];
  async function test(name, fn) {
    if (CASE_FILTER && caseFilterReached) return;
    const selectedCase = !CASE_FILTER || name.includes(CASE_FILTER);
    if (CASE_FILTER && !selectedCase) return;
    if (selectedCase) caseFilterReached = true;
    // Restore HTTP fixture state even when an assertion throws before local cleanup.
    // Counters remain monotonic; cookies and the shared page intentionally persist.
    const saved = { raceGroupResponses, raceGroupRequest, failGroupLoad, failGroupAction,
      slowGroupLoad, emptyGroupInventory, auditGroupMode, auditGroup: structuredClone(auditGroup),
      failPlatformLoad, failAgentsLoad, includeOfflineOwned };
    const savedAgents = structuredClone(agents), savedQueue = agentsQueue.slice();
    const savedWrites = groupWrites.slice(), savedJoins = groupJoinWrites.slice();
    const contextsBefore = new Set(browser.contexts());
    const pagesBefore = new Set(browser.contexts().flatMap((context) => context.pages()));
    try {
      await fn();
      if (selectedCase) caseFilterReached = true;
      passed += 1;
      console.log(`  PASS ${name}`);
    } catch (error) {
      failures.push(`${name}: ${error.message}`);
      console.log(`  FAIL ${name}: ${error.message}`);
    } finally {
      try {
        for (const context of browser.contexts()) {
          if (!contextsBefore.has(context)) await context.close();
          else for (const page of context.pages()) if (!pagesBefore.has(page)) await page.close();
        }
      } finally {
        ({ raceGroupResponses, raceGroupRequest, failGroupLoad, failGroupAction,
          slowGroupLoad, emptyGroupInventory, auditGroupMode, auditGroup,
          failPlatformLoad, failAgentsLoad, includeOfflineOwned } = saved);
        agents.splice(0, agents.length, ...savedAgents);
        agentsQueue.splice(0, agentsQueue.length, ...savedQueue);
        groupWrites.splice(0, groupWrites.length, ...savedWrites);
        groupJoinWrites.splice(0, groupJoinWrites.length, ...savedJoins);
      }
    }
  }

  // Console refreshes the Agent snapshot every 15 seconds. Interactive-flow
  // checks must cross a real timer tick so platform tabs cannot accidentally
  // inherit the offline-Agent pointer lock after a fast test has already ended.
  async function waitPastAgentRefreshCycle(page) {
    await page.waitForTimeout(16000);
  }


  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
    page.on('pageerror', (error) => pageErrors.push(error.message));
    page.on('console', (message) => { if (message.type() === 'error') consoleErrors.push(message.text()); });
    page.setDefaultTimeout(1500);
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: `http://127.0.0.1:${port}` });
    await page.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });
    if (CASE_FILTER) {
      await page.context().addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
      await page.reload({ waitUntil: 'domcontentloaded' });
      await page.locator('#platform-items a[data-item-id="agent-groups"]').click();
      await page.locator('#retryGroups').waitFor({ state: 'visible', timeout: CONDITION_WAIT_MS });
    }

    await test('loads the real console document', async () => {
      assert.strictEqual(await page.title(), 'DingWei Agent Console');
      await page.locator('nav #a').waitFor({ state: 'attached' });
    });

    await test('keeps platform workspaces interactive after a full Agent refresh interval', async () => {
      const timerPage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      timerPage.setDefaultTimeout(1500);
      await timerPage.context().addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
      await timerPage.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });
      await timerPage.locator('#platform-items a[data-item-id="agent-groups"]').click();
      await timerPage.locator('.group-workspace').waitFor({state:'visible'});
      await timerPage.locator('#platform-items a[data-item-id="specs"]').click();
      const specsPane = timerPage.locator('#panes iframe[title="Specs workspace"]');
      await specsPane.waitFor({state:'visible'});
      await timerPage.locator('#tabs .tab[title="Agent 管理"]').click();

      const agentGETsBeforeTimer = agentGETs;
      await waitPastAgentRefreshCycle(timerPage);
      assert.ok(agentGETs > agentGETsBeforeTimer, 'the test must observe at least one real 15-second Agent refresh');

      const groupPane = timerPage.locator('#panes .group-workspace');
      assert.notStrictEqual(await groupPane.evaluate((node) => node.style.pointerEvents), 'none', 'Agent timer must not disable the group platform pane');
      assert.notStrictEqual(await specsPane.evaluate((node) => node.style.pointerEvents), 'none', 'Agent timer must not disable the Specs platform pane');
      await timerPage.locator('#groupAddOpen').click();
      const code = timerPage.locator('#groupShortName');
      await code.scrollIntoViewIfNeeded();
      const box = await code.boundingBox();
      assert.ok(box, 'group name input must have a visible box after timer tick');
      assert.strictEqual(await timerPage.evaluate(({x,y}) => document.elementFromPoint(x,y)?.id, {x:box.x+box.width/2,y:box.y+box.height/2}), 'groupShortName');
      await code.click();
      await timerPage.keyboard.type('team1');
      assert.strictEqual(await code.inputValue(), 'team1');
      await timerPage.locator('#tabs .tab[title="Specs"]').click();
      const specsProbe = timerPage.frameLocator('iframe[title="Specs workspace"]').locator('#specsProbe');
      await specsProbe.click();
      assert.strictEqual(await specsProbe.textContent(), 'clicked');
      await timerPage.close();
    });

    await test('resets the created name while preserving the selected short-name Agent', async () => {
      const createPage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      try {
        createPage.setDefaultTimeout(1500);
        await createPage.context().addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
        await createPage.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });
        await createPage.locator('#platform-items a[data-item-id="agent-groups"]').click();
        await createPage.locator('#groupAddOpen').click();
        const leader = createPage.locator('#groupLeader');
        await createPage.waitForFunction(() => document.getElementById('groupLeader').disabled === false);
        assert.deepStrictEqual(await leader.locator('option').allTextContents(), ['alpha', 'beta']);
        await leader.selectOption('agent-b');
        assert.strictEqual(await leader.inputValue(), 'agent-b');
        const name = createPage.locator('#groupShortName');
        await name.click();
        await createPage.keyboard.type('myteam');
        const created = createPage.waitForResponse((response) => response.url().endsWith('/api/console/agent-groups') && response.request().method() === 'POST' && response.status() === 201);
        await createPage.locator('#createGroup').click();
        await created;
        await createPage.locator('#groupAddPanel').waitFor({state:'hidden'});
        assert.strictEqual(await name.inputValue(), '', 'successful create must clear the submitted name');
        assert.strictEqual(await leader.inputValue(), 'agent-b', 'refresh after create must preserve the selected Agent');
      } finally {
        await createPage.close();
        groupWrites.length = 0;
      }
    });

    await test('offers owned active offline Agents as group membership candidates', async () => {
      includeOfflineOwned = true;
      const offlinePage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      try {
        offlinePage.setDefaultTimeout(1500);
        await offlinePage.context().addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
        await offlinePage.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });
        await offlinePage.locator('#platform-items a[data-item-id="agent-groups"]').click();
        await offlinePage.waitForFunction(() => document.getElementById('groupLeader').disabled === false);
        assert.deepStrictEqual(await offlinePage.locator('#groupLeader option').allTextContents(), ['alpha', 'beta', 'gamma（offline）']);
        assert.deepStrictEqual(await offlinePage.locator('#groupJoinAgent option').allTextContents(), ['beta', 'gamma（offline）']);
      } finally {
        await offlinePage.close();
        includeOfflineOwned = false;
        groupJoinWrites.length = 0;
      }
    });

    await test('distinguishes platform and Agent load failures from true empty states', async () => {
      const stalePage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      // 负载时序窗修法（S-BROWSER-FOLLOWUP，照零组格前例）：本格各等待依赖页面 fetch
      // 完成，产品自身 fetch 超时 8000ms（console_auth.go:792）；1500ms 上界比产品
      // 超时还短，负载下会在产品合法等待时先红。改用 CONDITION_WAIT_MS(30000)≫8000＋余量；断言不改。
      stalePage.setDefaultTimeout(CONDITION_WAIT_MS);
      await stalePage.context().addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
      await stalePage.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });
      await stalePage.locator('#platform-items a[data-item-id="agent-groups"]').waitFor({ state: 'visible' });
      await stalePage.locator('#a button[title="owner-alpha-2642"]').waitFor({ state: 'visible' });
      failPlatformLoad = '500';
      failAgentsLoad = '500';
      await stalePage.evaluate(() => Promise.all([refreshPlatform(), refreshAgents()]));
      assert.strictEqual(await stalePage.locator('#platform-items a').count(), 0, 'failed refresh must clear stale platform entries');
      assert.strictEqual(await stalePage.locator('#retryPlatform').isVisible(), true);
      assert.strictEqual(await stalePage.locator('#retryAgents').isVisible(), true);
      // R-V2-0105-P1-LIST-UPLOAD：已有成功快照 ⇒ 失败不清空，列表顶部标过期，在线点本身带过期样式，原失败提示与重试保留
      const aliveBtn = stalePage.locator('#a button[title="owner-alpha-2642"]');
      assert.strictEqual(await aliveBtn.count(), 1, 'failed refresh must keep the last good Agent list');
      assert.match(await stalePage.locator('#agents-stale').textContent(), /^列表可能已过期（上次更新 \d\d:\d\d:\d\d）$/);
      assert.strictEqual(await stalePage.locator('#agents-stale').isVisible(), true);
      assert.strictEqual(await aliveBtn.evaluate((b) => b.classList.contains('agent-stale')), true, 'online dot itself must look stale');
      assert.strictEqual(await stalePage.locator('#agents-status').textContent(), 'Agent 列表加载失败: HTTP 500: fixture agents unavailable');
      await stalePage.evaluate(() => renderAgentNav());
      assert.strictEqual(await aliveBtn.evaluate((b) => b.classList.contains('agent-stale')), true, 'stale style must survive a nav rebuild');
      failPlatformLoad = '';
      failAgentsLoad = '';
      agents.push(agentRecord('owner-newcomer-2642', true));   // 恢复后的成功带新内容：列表须更新，不只是去标
      await stalePage.evaluate(() => refreshAgents());
      agents.pop();
      assert.strictEqual(await stalePage.locator('#agents-stale').isHidden(), true, 'a success must clear the stale mark');
      assert.ok((await stalePage.evaluate(() => agentListSnapshot.map((a) => a.full_name))).includes('owner-newcomer-2642'), 'the list must update on the recovering success');
      assert.strictEqual(await aliveBtn.evaluate((b) => b.classList.contains('agent-stale')), false);
      assert.strictEqual(await stalePage.locator('#agents-status').textContent(), '');
      // 交错：先发的慢请求晚回，标记与列表以最后一次成功为准
      const settle = () => stalePage.evaluate(() => { const first = refreshAgents(); return new Promise((r) => setTimeout(r, 80)).then(() => Promise.all([first, refreshAgents()])); });
      agentsQueue.push((res) => setTimeout(() => { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify({ agents: [agentRecord('owner-old-2642', true)] })); }, 400));
      await settle();
      assert.deepStrictEqual(await stalePage.evaluate(() => agentListSnapshot.map((a) => a.full_name)), ['owner-alpha-2642', 'owner-offline-2642'], 'an older response landing late must not replace the newer list');
      assert.strictEqual(await aliveBtn.count(), 1);
      agentsQueue.push((res) => setTimeout(() => { res.writeHead(500, {'Content-Type':'text/plain'}); res.end('late failure'); }, 400));
      await settle();
      assert.strictEqual(await stalePage.locator('#agents-stale').isHidden(), true, 'an older failure after a newer success must not mark stale');
      assert.strictEqual(await stalePage.locator('#agents-status').textContent(), '');
      // 401／403 不是过期：无过期标、原错误文本
      failAgentsLoad = '401';
      await stalePage.evaluate(() => refreshAgents());
      assert.strictEqual(await stalePage.locator('#agents-stale').isHidden(), true, '401 must not be shown as a stale list');
      assert.strictEqual(await stalePage.locator('#agents-status').textContent(), 'Agent 列表加载失败: HTTP 401: fixture login required');
      assert.strictEqual(await stalePage.locator('#a button.agent-stale').count(), 0);
      failPlatformLoad = '500';
      failAgentsLoad = '500';
      await stalePage.close();
      const failedPage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      // 同上：1500ms 上界短于产品 fetch 超时 8000ms；CONDITION_WAIT_MS≫8000＋调度余量。
      failedPage.setDefaultTimeout(CONDITION_WAIT_MS);
      const failedPageErrors = [], failedConsoleErrors = [], expectedFailureDiagnostics = [];
      failedPage.on('pageerror', (error) => failedPageErrors.push(error.message));
      failedPage.on('console', (message) => {
        if (message.type() !== 'error') return;
        if (message.text().startsWith('Failed to load resource: the server responded with a status of')) expectedFailureDiagnostics.push(message.text());
        else failedConsoleErrors.push(message.text());
      });
      await failedPage.context().addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
      await failedPage.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });
      await failedPage.locator('#platform-status').filter({ hasText: '工作台加载失败: HTTP 500: fixture platform unavailable' }).waitFor({ state: 'visible' });
      await failedPage.locator('#agents-status').filter({ hasText: 'Agent 列表加载失败: HTTP 500: fixture agents unavailable' }).waitFor({ state: 'visible' });
      assert.strictEqual(await failedPage.locator('#retryPlatform').isVisible(), true);
      assert.strictEqual(await failedPage.locator('#retryAgents').isVisible(), true);
      assert.notStrictEqual(await failedPage.locator('#agents-status').textContent(), '没有自己的 Agent');
      assert.strictEqual(await failedPage.locator('#agents-stale').isHidden(), true, 'first-ever failure has nothing to call stale');
      assert.deepStrictEqual(await failedPage.evaluate(() => agentListSnapshot.map((a) => a.full_name)), [], 'first-ever failure keeps the Agent snapshot empty');

      failPlatformLoad = 'timeout';
      failAgentsLoad = 'timeout';
      await failedPage.evaluate(() => { consoleFetchTimeoutMs = 100; });
      // 100ms abort 旋钮下，「点击→读加载文案」若分两次 CDP 往返，负载下往返本身可
      // 超过 100ms，读到的是已被 abort 改写的超时文案（竞态在测试侧，不在产品）。
      // 同一 evaluate 内点击＋同步读：单线程内无定时器可插入；断言值一字不改。
      const loadingTexts = await failedPage.evaluate(() => {
        document.getElementById('retryPlatform').click();
        document.getElementById('retryAgents').click();
        return { platform: document.getElementById('platform-status').textContent, agents: document.getElementById('agents-status').textContent };
      });
      assert.strictEqual(loadingTexts.platform, '工作台加载中…');
      assert.strictEqual(loadingTexts.agents, 'Agent 列表加载中…');
      await failedPage.locator('#platform-status').filter({ hasText: '工作台加载失败: 请求超时' }).waitFor({ state: 'visible' });
      await failedPage.locator('#agents-status').filter({ hasText: 'Agent 列表加载失败: 请求超时' }).waitFor({ state: 'visible' });
      // 恢复产品默认（console_auth.go:792 consoleFetchTimeoutMs=8000）：100ms 只是逼出
      // 挂起夹具 AbortError 的测试旋钮；不恢复则后续 empty/成功段的健康 fetch 在负载下
      // 与 100ms abort 竞态。超时段断言已在旋钮下完成，恢复不影响其判别力。
      await failedPage.evaluate(() => { consoleFetchTimeoutMs = 8000; });

      failPlatformLoad = 'empty';
      failAgentsLoad = 'empty';
      await failedPage.locator('#retryPlatform').click();
      await failedPage.locator('#retryAgents').click();
      await failedPage.locator('#platform-status').filter({ hasText: '没有可用工作台入口' }).waitFor({ state: 'visible' });
      await failedPage.locator('#agents-status').filter({ hasText: '没有自己的 Agent' }).waitFor({ state: 'visible' });
      assert.notStrictEqual(await failedPage.locator('#agents-status').textContent(), 'Agent 列表加载失败: 请求超时');
      assert.strictEqual(await failedPage.locator('#retryPlatform').isHidden(), true);
      assert.strictEqual(await failedPage.locator('#retryAgents').isHidden(), true);

      failPlatformLoad = '';
      failAgentsLoad = '';
      await failedPage.evaluate(() => Promise.all([refreshPlatform(), refreshAgents()]));
      await failedPage.locator('#platform-items a[data-item-id="agent-groups"]').waitFor({ state: 'visible' });
      await failedPage.locator('#a button[title="owner-alpha-2642"]').waitFor({ state: 'visible' });
      assert.strictEqual(await failedPage.locator('#platform-status').textContent(), '');
      assert.strictEqual(await failedPage.locator('#agents-status').textContent(), '');
      assert.strictEqual(await failedPage.locator('#retryPlatform').isHidden(), true);
      assert.strictEqual(await failedPage.locator('#retryAgents').isHidden(), true);
      assert.strictEqual(expectedFailureDiagnostics.length, 2);
      assert.deepStrictEqual(failedPageErrors, []);
      assert.deepStrictEqual(failedConsoleErrors, []);
      await failedPage.close();
    });
    if (['silent-platform-load','silent-agent-load','preserve-stale-load-data','show-retry-after-success','show-retry-on-empty','clear-agents-on-failure','agents-ignore-request-order','agents-stale-ignore-order','agents-stale-no-dot','agents-stale-first-failure','agents-stale-on-auth'].includes(MUTATION)) {
      console.log(`console browser harness: passed=${passed} failed=${failures.length}`);
      if (failures.length) console.error(failures.join('\n'));
      process.exitCode = failures.length ? 1 : 0;
      return;
    }

    await test('renders online and offline agents from the console API', async () => {
      await page.locator('#agentNavMode').click();
      const online = page.locator('#a button[title="owner-alpha-2642"]');
      const offline = page.locator('#a button[title="owner-offline-2642"]');
      await online.waitFor({ state: 'visible' });
      await offline.waitFor({ state: 'attached' });
      assert.strictEqual(await online.isEnabled(), true);
      assert.strictEqual(await offline.isDisabled(), true);
      assert.strictEqual(await page.locator('#retryAgents').isHidden(), true);
      await page.locator('#agentNavMode').click();
    });

    await test('renders no platform entry without an observable action', async () => {
      const agentsItem = page.locator('#platform-items a[data-item-id="agents"]');
      assert.strictEqual(await agentsItem.count(), 0, 'the inert Agents platform entry must not be rendered');
      assert.strictEqual(await page.locator('#platform-items a:not([data-item-id="specs"]):not([data-item-id="task-board"]):not([data-item-id="agent-groups"])').count(), 0);
      assert.strictEqual(await page.locator('#platform-actions button').count(), 0);
      assert.strictEqual(await page.locator('#retryPlatform').isHidden(), true);
      assert.strictEqual(await page.locator('html').getAttribute('data-platform-role'), 'owner');
    });

    await test('keeps dead action buttons absent after the server role changes', async () => {
      await page.context().addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
      await page.evaluate(() => refreshPlatform());
      assert.strictEqual(await page.locator('#platform-actions button').count(), 0);
      assert.strictEqual(await page.locator('html').getAttribute('data-platform-role'), 'management');
    });

    await test('lets a human start from zero groups without a hidden-control deadlock', async () => {
      emptyGroupInventory = true;
      const emptyPage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      emptyPage.setDefaultTimeout(1500);
      const emptyErrors = [];
      emptyPage.on('pageerror', (error) => emptyErrors.push(error.message));
      emptyPage.on('console', (message) => { if (message.type() === 'error') emptyErrors.push(message.text()); });
      await emptyPage.context().addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
      await emptyPage.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });
      const entry = emptyPage.locator('#platform-items a[data-item-id="agent-groups"]');
      await entry.waitFor({ state: 'visible' });
      await entry.click();
      await emptyPage.locator('#group-status').filter({ hasText: '没有自己的 Agent，无法建组或加入' }).waitFor({ state: 'visible' });
      assert.strictEqual(await emptyPage.locator('#groupDetail').isHidden(), true);
      assert.strictEqual(await emptyPage.locator('#groupLeader').isDisabled(), true);
      assert.strictEqual(await emptyPage.locator('#groupJoinAgent').isDisabled(), true);
      async function humanHit(selector) {
        const target = emptyPage.locator(selector);
        const box = await target.boundingBox();
        assert.ok(box && box.width > 0 && box.height > 0, `${selector} has no visible hit box`);
        return emptyPage.evaluate(({ selector, x, y }) => {
          const wanted = document.querySelector(selector);
          const hit = document.elementFromPoint(x, y);
          return Boolean(wanted && hit && (hit === wanted || wanted.contains(hit)));
        }, { selector, x: box.x + box.width / 2, y: box.y + box.height / 2 });
      }
      assert.strictEqual(await humanHit('#retryGroups'), true);
      const getsBeforeRetry = groupGETs;
      const refreshed = emptyPage.waitForResponse((response) => response.url().endsWith('/api/console/agent-groups') && response.status() === 200);
      await emptyPage.locator('#retryGroups').click();
      await refreshed;
      assert.strictEqual(groupGETs, getsBeforeRetry + 1, 'visible retry must issue one groups GET');
      assert.strictEqual(await emptyPage.locator('#group-status').textContent(), '没有自己的 Agent，无法建组或加入');
      assert.deepStrictEqual(emptyErrors, []);
      await emptyPage.close();
      emptyGroupInventory = false;
    });
    if (MUTATION === 'legacy-group-deadlock') {
      console.log(`console browser harness: passed=${passed} failed=${failures.length}`);
      if (failures.length) console.error(failures.join('\n'));
      process.exitCode = failures.length ? 1 : 0;
      return;
    }

    await test('runs the zero-group human workflow without hidden actions or long tasks', async () => {
      auditGroupMode = true;
      auditGroup = null;
      const auditContext = await browser.newContext({ viewport: { width: 1280, height: 800 } });
      await auditContext.addInitScript(() => {
        globalThis.__dwLongTasks = [];
        new PerformanceObserver((list) => {
          for (const entry of list.getEntries()) if (entry.duration > 200) globalThis.__dwLongTasks.push(entry.duration);
        }).observe({ type: 'longtask', buffered: true });
      });
      await auditContext.addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
      const auditPage = await auditContext.newPage();
      // 同零组格修法：1500ms 上界短于产品 fetch 超时 8000ms；负载下各 waitFor/click
      // 会在产品合法等待时先红。CONDITION_WAIT_MS≫8000＋调度余量；断言不改。
      auditPage.setDefaultTimeout(CONDITION_WAIT_MS);
      const auditErrors = [];
      const auditExpectedHTTPDiagnostics = [];
      auditPage.on('pageerror', (error) => auditErrors.push(error.message));
      auditPage.on('console', (message) => {
        if (message.type() !== 'error') return;
        if (message.text().startsWith('Failed to load resource: the server responded with a status of')) auditExpectedHTTPDiagnostics.push(message.text());
        else auditErrors.push(message.text());
      });
      await auditPage.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });
      async function humanHit(selector) {
        const target = auditPage.locator(selector);
        await target.scrollIntoViewIfNeeded();
        const box = await target.boundingBox();
        assert.ok(box && box.width > 0 && box.height > 0, `${selector} has no visible hit box`);
        return auditPage.evaluate(({ selector, x, y }) => {
          const wanted = document.querySelector(selector), hit = document.elementFromPoint(x, y);
          return Boolean(wanted && hit && (hit === wanted || wanted.contains(hit)));
        }, { selector, x: box.x + box.width / 2, y: box.y + box.height / 2 });
      }
      const entry = auditPage.locator('#platform-items a[data-item-id="agent-groups"]');
      await entry.waitFor({ state: 'visible' });
      await entry.click();
      await auditPage.waitForFunction(() => document.getElementById('groupLeader').disabled === false);
      if (await auditPage.locator('#groupAddPanel').isHidden()) await auditPage.locator('#groupAddOpen').click();
      await auditPage.locator('#groupAddPanel').waitFor({ state: 'visible' });

      await auditPage.locator('#groupShortName').click();
      await auditPage.keyboard.type('dingwei');
      await auditPage.locator('#groupLeader').selectOption('agent-a');
      assert.strictEqual(await humanHit('#createGroup'), true);
      await auditPage.locator('#createGroup').click();
      await auditPage.locator('#groupListItems').getByText('dingwei').waitFor({ state: 'visible' });

      // Failure paths are product outcomes and must remain visible without JS errors.
      await auditPage.locator('#groupAddOpen').click();
      await auditPage.locator('#groupShortName').click();
      await auditPage.keyboard.press(process.platform === 'darwin' ? 'Meta+A' : 'Control+A');
      await auditPage.keyboard.type('dingwei');
      assert.strictEqual(await humanHit('#createGroup'), true);
      await auditPage.locator('#createGroup').click();
      await auditPage.locator('#groupNameFeedback').filter({ hasText: '团队名称不可用；建议名称：dingwei1、dingwei2' }).waitFor({ state: 'visible' });
      await auditPage.locator('#groupAddOpen').click();

      await auditPage.locator('#groupListItems tr').first().click();
      assert.strictEqual(await humanHit('#groupAdditionsToggle'), true);
      await auditPage.locator('#groupAdditionsToggle').click();
      await auditPage.locator('#groupAdditionsToggle').filter({ hasText: '开放添加' }).waitFor({ state: 'visible' });
      await auditPage.locator('#groupJoinAgent').selectOption('agent-b');
      await auditPage.locator('#joinGroup').click();
      await auditPage.locator('#groupJoinFeedback').filter({ hasText: '该编组已关闭添加' }).waitFor({ state: 'visible' });
      await auditPage.locator('#groupListItems tr').first().click();
      await auditPage.locator('#groupAdditionsToggle').click();
      await auditPage.locator('#groupAdditionsToggle').filter({ hasText: '关闭添加' }).waitFor({ state: 'visible' });

      await auditPage.locator('#groupJoinAgent').selectOption('agent-b');
      await auditPage.locator('#joinGroup').click();
      await auditPage.locator('#groupListItems tr').first().click();
      const member = auditPage.locator('#groupMembers .group-member-row').filter({ hasText: 'beta' });
      await member.waitFor({ state: 'visible' });
      await member.hover();
      const removeMember = member.locator('.group-member-remove');
      await removeMember.waitFor({ state: 'visible' });
      assert.strictEqual(await removeMember.evaluate((button) => button.closest('.group-member-row') === button.parentElement), true);
      auditPage.once('dialog', (dialog) => dialog.accept());
      await removeMember.click();
      await member.waitFor({ state: 'detached' });

      assert.strictEqual(await humanHit('#groupAdditionsToggle'), true);
      await auditPage.locator('#groupAdditionsToggle').click();
      await auditPage.locator('#groupAdditionsToggle').filter({ hasText: '开放添加' }).waitFor({ state: 'visible' });
      await auditPage.locator('#groupAdditionsToggle').click();
      await auditPage.locator('#groupAdditionsToggle').filter({ hasText: '关闭添加' }).waitFor({ state: 'visible' });

      const deleteGroup = auditPage.locator('#groupListItems button').filter({ hasText: '删除' });
      assert.strictEqual(await deleteGroup.count(), 1);
      auditPage.once('dialog', (dialog) => dialog.accept());
      await deleteGroup.click();
      await auditPage.locator('#groupEmptyCreate').waitFor({ state: 'visible' });
      assert.deepStrictEqual(await auditPage.evaluate(() => globalThis.__dwLongTasks), []);
      assert.strictEqual(auditExpectedHTTPDiagnostics.length, 2, 'two deliberate HTTP failures (duplicate name, additions closed) must stay narrowly classified');
      assert.deepStrictEqual(auditErrors, []);
      await auditContext.close();
      auditGroupMode = false;
      auditGroup = null;
      groupWrites.length = 0;
      groupJoinWrites.length = 0;
    });

    await test('opens Agent groups inside console with the owner-only online Agent selector', async () => {
      const entry = page.locator('#platform-items a[data-item-id="agent-groups"]');
      await entry.waitFor({ state: 'visible' });
      assert.strictEqual(await entry.getAttribute('href'), '#agent-groups');
      assert.strictEqual(await entry.getAttribute('target'), null);
      slowGroupLoad = true;
      await entry.click();
      await page.evaluate(() => refreshGroups('owner-selector'));
      await page.locator('#tabs .tab[title="Agent 管理"]').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('.group-workspace').count(), 1);
      assert.strictEqual(await page.locator('.group-workspace:visible').count(), 1);
      for (const id of ['groupLeader', 'groupListItems', 'groupDetail', 'retryGroups']) {
        assert.strictEqual(await page.locator(`#${id}`).count(), 1, `${id} must have one live DOM node`);
      }
      await page.waitForFunction(() => document.getElementById('groupLeader').disabled === false);
      await page.locator('#groupListItems').getByText('team-001').waitFor({ state: 'visible' });
      assert.deepStrictEqual(await page.locator('#groupLeader option').allTextContents(), ['alpha', 'beta']);
      await page.locator('#groupListItems tr').first().click();
      assert.deepStrictEqual(await page.locator('#groupJoinAgent option').allTextContents(), ['beta']);
      assert.strictEqual(await page.locator('.group-workspace input[name="key_id"]').count(), 0);
    });

    await test('shows a visible successful retry result', async () => {
      await page.locator('#retryGroups').click();
      await page.locator('#groupRetryFeedback').getByText('已刷新 · 1 个编组').waitFor({ state: 'visible' });
    });

    await test('ignores an older group snapshot that arrives after the latest response', async () => {
      raceGroupResponses = true;
      raceGroupRequest = 0;
      await Promise.all([
        page.evaluate(() => refreshGroups('race-first')),
        page.evaluate(() => refreshGroups('race-latest')),
      ]);
      await page.waitForTimeout(180);
      assert.strictEqual(await page.locator('#groupAdditionsToggle').textContent(), '开放添加');
      raceGroupResponses = false;
    });

    await test('keeps rejected and accepted create results visible beside the name control', async () => {
      groupWrites.length = 0;
      await page.locator('#groupAddOpen').click();
      await page.locator('#groupShortName').fill('');
      await page.locator('#createGroup').click();
      assert.strictEqual(await page.locator('#groupNameFeedback').textContent(), '请输入团队名称');
      assert.strictEqual(groupWrites.length, 0);
      await page.locator('#groupShortName').click();
      await page.locator('#groupShortName').pressSequentially('团队2');
      await page.locator('#createGroup').click();
      await page.locator('#groupAddPanel').waitFor({ state: 'hidden' });
      assert.deepStrictEqual(groupWrites[0].body, { agent_id: 'agent-a', short_name: '团队2' });
      assert.strictEqual('key_id' in groupWrites[0].body, false);
      assert.strictEqual('session_name' in groupWrites[0].body, false);
    });

    await test('shows group load and network action failures beside their controls', async () => {
      const conditionDeadline = Date.now() + CONDITION_WAIT_MS;
      const expectText = async (selector, text, criterion) => {
        const locator = page.locator(selector);
        await page.waitForFunction(({ selector, text }) => { const node = document.querySelector(selector); return node && node.offsetParent !== null && node.textContent.includes(text); }, { selector, text }, { timeout: Math.max(1, conditionDeadline - Date.now()) }).catch(() => {});
        console.log('EXPECTTEXT-DIAG ' + selector + ' actual=' + JSON.stringify(await locator.textContent()));
        assert.ok(String(await locator.textContent() || '').includes(text), criterion);
      };
      failGroupLoad = '500';
      await page.locator('#retryGroups').click({ timeout: CONDITION_WAIT_MS });
      await expectText('#groupRetryFeedback', '编组加载失败: HTTP 500', '编组加载失败: HTTP 500');
      assert.strictEqual(await page.locator('#groupLeader').isDisabled(), true);
      assert.strictEqual(await page.locator('#groupLeader option').textContent(), '加载失败');
      failGroupLoad = 'shape'; await page.locator('#retryGroups').click({ timeout: CONDITION_WAIT_MS }); await expectText('#groupRetryFeedback', '响应格式无效', '响应格式无效');
      failGroupLoad = 'timeout'; await page.evaluate(() => { groupFetchTimeoutMs=20; }); await page.locator('#retryGroups').click({ timeout: CONDITION_WAIT_MS }); await expectText('#groupRetryFeedback', '请求超时', '请求超时');
      // 恢复产品默认（console_auth.go:792 groupFetchTimeoutMs=8000）：20ms 只是逼出
      // 挂起夹具 AbortError 的测试旋钮；不恢复则后续 empty/恢复段的健康 fetch 在负载下
      // 与 20ms abort 竞态（前单 conditionDeadline ~29.6s 超时红的根因）。断言不改。
      await page.evaluate(() => { groupFetchTimeoutMs = 8000; });
      failGroupLoad = 'empty'; await page.locator('#retryGroups').click({ timeout: CONDITION_WAIT_MS }); await expectText('#group-status', '没有自己的 Agent', '没有自己的 Agent');
      assert.strictEqual(await page.locator('#groupLeader option').textContent(), '没有自己的 Agent');
      failGroupLoad = ''; await page.locator('#retryGroups').click({ timeout: CONDITION_WAIT_MS }); await page.waitForFunction(() => document.getElementById('groupLeader').disabled === false, null, { timeout: Math.max(1, conditionDeadline - Date.now()) });
      assert.deepStrictEqual(await page.locator('#groupLeader option').allTextContents(), ['alpha', 'beta']);
      failGroupLoad = '';
      failGroupAction = '500';
      if (await page.locator('#groupAddPanel').isHidden()) await page.locator('#groupAddOpen').click({ timeout: CONDITION_WAIT_MS });
      await page.locator('#groupShortName').fill('团队3', { timeout: CONDITION_WAIT_MS });
      await page.locator('#createGroup').click({ timeout: CONDITION_WAIT_MS });
      await expectText('#groupNameFeedback', 'fixture create denied', 'fixture create denied');
      failGroupAction = 'network';
      await page.locator('#createGroup').click({ timeout: CONDITION_WAIT_MS });
      await expectText('#groupNameFeedback', '请求失败:', '请求失败:');
      failGroupAction = '';
    });

    // R-V2-0102-GROUP-FIXES B。断言面＝添加编组面板（发不发请求、发出的 short_name、显示的文字）；
    // 实现承诺＝normalizeGroupName＋NEW_GROUP_RE 预检＋groupAction 读 JSON message、纯文本照旧（上一格已钉纯文本）。
    await test('group name: malformed names are explained locally in the shared sentence; invisible characters are stripped; a JSON error shows its message', async () => {
      const RULE = '编组名只能用 1～24 个字母、数字或下划线（可用中文、日文、韩文；不含空格、连字符、点号及其他符号）';
      assert.strictEqual(await page.evaluate(() => GROUP_NAME_RULE_TEXT), RULE);
      if (await page.locator('#groupAddPanel').isHidden()) await page.locator('#groupAddOpen').click();
      groupWrites.length = 0;
      for (const bad of ['te st', 'test-1', 'a.b', '___', '二十五个字二十五个字二十五个字二十五个字二十五个字']) {   // 25 字直接赋值（粘贴等场景）；只有下划线不合法
        await page.evaluate((v) => { document.getElementById('groupShortName').value = v; }, bad);
        await page.locator('#createGroup').click();
        assert.strictEqual(await page.locator('#groupNameFeedback').textContent(), RULE, `local precheck for ${bad}`);
      }
      assert.strictEqual(groupWrites.length, 0, 'malformed names are not sent');
      await page.evaluate(() => { document.getElementById('groupShortName').value = '\u200btest\u2060 '; });
      await page.locator('#createGroup').click();
      await page.waitForFunction(() => document.getElementById('groupAddPanel').hidden);
      assert.strictEqual(groupWrites.at(-1).body.short_name, 'test', 'invisible characters stripped before sending');
      await page.locator('#groupAddOpen').click();
      failGroupAction = 'json400';
      await page.locator('#groupShortName').fill('okname');
      await page.locator('#createGroup').click();
      await page.locator('#groupNameFeedback').filter({hasText:'桩：服务端给的中文原因'}).waitFor({state:'visible'});
      assert.strictEqual(await page.locator('#groupNameFeedback').textContent(), '桩：服务端给的中文原因');
      failGroupAction = '';
    });

    await test('opens a real console tab and agent iframe', async () => {
      await page.locator('#a button[title="owner-alpha-2642"]').click();
      const tab = page.locator('#tabs .tab[title="owner-alpha-2642"]');
      const pane = page.locator('#panes iframe[title="Agent view: owner-alpha-2642"]');
      await tab.waitFor({ state: 'visible' });
      assert.strictEqual(await tab.evaluate((node) => node.classList.contains('on')), true);
      assert.strictEqual(new URL(await pane.getAttribute('src'), page.url()).pathname, '/view/owner-alpha-2642');
    });

    await test('explains why an open Agent tab becomes non-interactive when offline', async () => {
      agents[0].online = false;
      await page.evaluate(() => refreshAgents());
      const tab = page.locator('#tabs .tab[title^="owner-alpha-2642"]');
      await tab.waitFor({ state: 'visible' });
      assert.strictEqual(await tab.locator('.tab-state').textContent(), '离线，暂不可交互');
      assert.strictEqual(await tab.getAttribute('aria-disabled'), 'true');
      assert.match(await tab.getAttribute('title'), /Agent 离线，暂不可交互/);
      assert.strictEqual(await page.locator('#panes iframe[title="Agent view: owner-alpha-2642"]').evaluate((node) => node.style.pointerEvents), 'none');
      const reason = page.locator('#panes .agent-offline-reason');
      assert.strictEqual(await reason.isVisible(), true);
      assert.strictEqual(await reason.textContent(), '该 Agent 已离线，暂不可交互');
      assert.notStrictEqual(await page.locator('#panes .group-workspace').evaluate((node) => node.style.pointerEvents), 'none', 'Agent refresh must not disable the platform group workspace');
      agents[0].online = true;
      await page.evaluate(() => refreshAgents());
      assert.strictEqual(await page.locator('#tabs .tab[title="owner-alpha-2642"] .tab-state').isHidden(), true);
      assert.strictEqual(await reason.isHidden(), true);
    });

    await test('opens Specs inside console without destroying live Agent panes', async () => {
      const paneCount = await page.locator('#panes iframe').count();
      const paneSrc = await page.locator('#panes iframe').first().getAttribute('src');
      await page.locator('#platform-items a[data-item-id="specs"]').click();
      const specsPane = page.locator('#panes iframe[title="Specs workspace"]');
      await specsPane.waitFor({state:'visible'});
      assert.strictEqual(new URL(await specsPane.getAttribute('src'), page.url()).pathname, '/console/specs');
      assert.strictEqual(await page.locator('#panes iframe').count(), paneCount+1);
      assert.strictEqual(await page.locator('#panes iframe').first().getAttribute('src'), paneSrc);
    });

    // 业主 2026-09-24 批「版本号显示 ---- 可以」。
    // 🔴 这条断的是【显示值等于服务端喂进来的那一版】,不是「页面上有版本号字样」——
    //    后者在写死实现下照样绿(阴性对照:--mutate=hardcoded-version 必须红)。
    await test('shows the server-rendered build version and never swallows a click', async () => {
      const badge = page.locator('#app-version');
      await badge.waitFor({ state: 'attached' });
      assert.strictEqual(
        (await badge.textContent()).trim(),
        `${HARNESS_VERSION} · ${HARNESS_COMMIT.slice(0, 7)}`,
        '角标显示的必须是服务端喂进来的那一版');
      assert.strictEqual(await badge.getAttribute('data-version'), HARNESS_VERSION);
      // 🔴 「不遮挡内容」不靠位置侥幸:角标正中那一点的命中元素必须【不是】角标本身。
      const hitID = await page.evaluate(() => {
        const el = document.getElementById('app-version');
        const r = el.getBoundingClientRect();
        const top = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
        return top ? top.id : null;
      });
      assert.notStrictEqual(hitID, 'app-version', '角标吞掉了它所在位置的点击 ⇒ 它在遮挡内容');
      assert.strictEqual(await badge.isVisible(), true, '角标必须是业主看得见的');
    });
  } finally {
    await browser.close();
    await new Promise((resolve) => server.close(resolve));
  }

  console.log(`console browser harness: passed=${passed} failed=${failures.length}`);
  if (failures.length) {
    console.error(failures.join('\n'));
    process.exit(1);
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(2);
});
