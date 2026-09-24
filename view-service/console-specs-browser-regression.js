// Browser-level north-star gate for the real console specs management page.
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const GO_SOURCE = path.join(__dirname, '..', 'internal', 'm8', 'console_specs_page.go');
const ASSET_DIR = path.join(__dirname, '..', 'internal', 'm8', 'static', 'easymde');
const MD_CORE = path.join(__dirname, 'md-render-core.js');
const RENDERER = path.join(__dirname, 'renderer-canary-sse.html');
const START = 'const consoleSpecsPageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleSpecsPage';
const MUTATION = process.argv.find((arg) => arg.startsWith('--mutate='))?.slice('--mutate='.length) || '';

function productionHTML() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(START);
  assert.notStrictEqual(start, -1, 'consoleSpecsPageHTML start marker missing');
  const contentStart = start + START.length;
  const end = source.indexOf(END, contentStart);
  assert.notStrictEqual(end, -1, 'consoleSpecsPageHTML end marker missing');
  let html = source.slice(contentStart, end).replace(
    '<script id=dw-md-core></script>', () => `<script>${fs.readFileSync(MD_CORE, 'utf8')}</script>`);
  // 🔴 变异必须【真的改到字节】:本仓已查出三个既有变异因锚点失配成了空操作、变异下仍全绿。
  //    统一走 mutateHTML():替换前后必须不同,否则当场报错。
  const mutateHTML = (needle, replacement) => {
    const before = html;
    html = html.split(needle).join(replacement);
    assert.notStrictEqual(html, before, `变异 ${MUTATION} 未改动任何字节 —— 锚点已失配,这条证明不存在`);
  };
  if (MUTATION === 'no-list-error-text') {
    mutateHTML('catch(err){specListFailure(err);}', 'catch(err){}');
  }
  if (MUTATION === 'no-editor-focus') {
    assert.ok(html.includes('specEditors.edit.codemirror.focus();'), 'no-editor-focus mutation anchor missing');
    html = html.replace('specEditors.edit.codemirror.focus();', '');
  }
  if (MUTATION === 'no-editor-refresh') {
    assert.ok(html.includes("if(id==='editor')specEditors.edit.codemirror.refresh();"), 'no-editor-refresh mutation anchor missing');
    html = html.replace("if(id==='editor')specEditors.edit.codemirror.refresh();", '');
  }
  if (MUTATION === 'no-render') html = html.replace('renderSpec(data.revision.content_markdown);', '');
  if (MUTATION === 'no-wire') html = html.replace('wireSpecRender(root);', '');
  if (MUTATION === 'no-copy') html = html.replace("root.querySelectorAll('.copy-code')", "root.querySelectorAll('.copy-code-disabled')");
  if (MUTATION === 'no-server-error') html = html.replace("return prefix+' '+r.status+(detail?':'+detail:'');", "return prefix;");
  if (MUTATION === 'role-split') html = html.replace("var canEdit=(isOwner||!!(sharedSpecID&&sharedEditToken))&&!spec.editing_closed;", "var canEdit=platformRole!=='management';");
  if (MUTATION === 'no-action-error') html = html.replace("status.textContent=await responseFailure('操作失败',r);", "status.textContent='';");
  if (MUTATION === 'no-view') html = html.replace("view.className='view-spec';", "view.className='view-spec-disabled';");
  if (MUTATION === 'readonly-can-write') html = html.replace("document.getElementById('save-spec').hidden=!!readOnly;", "document.getElementById('save-spec').hidden=false;");
  if (MUTATION === 'same-screen-create') html = html.replace('<section id=new-screen hidden>', '<section id=new-screen>');
  if (MUTATION === 'hide-dependents') html = html.replace("+' · 被依赖 '+((task.depended_by||[]).join(', ')||'无')", "");
  if (MUTATION === 'hide-list-on-detail') html = html.replace("showScreen('detail-screen');return;", "showScreen('detail-screen');document.getElementById('list-screen').hidden=true;return;");
  if (MUTATION === 'hide-list-on-detail') assert.ok(html.includes("document.getElementById('list-screen').hidden=true"), 'hide-list mutation anchor missing');
  if (MUTATION === 'no-task-dod') html = html.replace("row.querySelector('.task-dod').value=taskDoD(task);", "row.querySelector('.task-dod').value='';");
  if (MUTATION === 'no-parallel-marker') html = html.replace("(task.parallel?'可并行':'顺序执行')", "'顺序执行'");
  if (MUTATION === 'no-hours-hint') html = html.replace("node.textContent=Number(input.value)>8?'建议再拆':'';", "node.textContent='';");
  if (MUTATION === 'no-empty-create') html = html.replace("list.append(empty);", "");
  if (MUTATION === 'show-detail-before-click') html = html.replace("document.getElementById('detail-screen').hidden=true;", "document.getElementById('detail-screen').hidden=false;");
  if (MUTATION === 'keep-detail-on-page') html = html.replace('async function refreshSpecs(){clearSpecSelection();', 'async function refreshSpecs(){');
  if (MUTATION === 'no-task-entry') html = html.replace("document.getElementById('show-task-split').hidden=!data.spec.can_split;", "document.getElementById('show-task-split').hidden=true;");
  if (MUTATION === 'eager-task-form') html = html.replace("document.getElementById('task-split').hidden=true;document.getElementById('show-task-split').hidden=!data.spec.can_split;", "document.getElementById('task-split').hidden=false;document.getElementById('show-task-split').hidden=!data.spec.can_split;");
  const silentPresplit = ['silent-presplit-no-reply','silent-presplit-timeout','silent-presplit-invalid','silent-presplit-restart'];
  if (silentPresplit.includes(MUTATION)) html = html.replaceAll("status.textContent='预拆失败，可手工添加';", "status.textContent='';");
  if (MUTATION && !['no-list-error-text', 'no-editor-focus', 'no-editor-refresh', 'no-render', 'no-wire', 'no-copy', 'no-server-error', 'role-split', 'no-action-error', 'no-view', 'readonly-can-write', 'same-screen-create', 'hide-dependents', 'hide-list-on-detail', 'no-task-dod', 'no-parallel-marker', 'no-hours-hint', 'no-empty-create', 'show-detail-before-click', 'keep-detail-on-page', 'no-task-entry', 'eager-task-form', ...silentPresplit].includes(MUTATION)) throw new Error(`unknown mutation: ${MUTATION}`);
  return html;
}

function listen(server) {
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => resolve(server.address().port));
  });
}

async function requestBody(req) {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  return chunks.length ? JSON.parse(Buffer.concat(chunks).toString('utf8')) : {};
}

async function main() {
  const html = productionHTML();
  const specs = [];
  const revisions = new Map();
  const taskPlans = [];
  const taskCreates = [];
  const taskDeps = new Map([['task-a', []], ['task-b', []]]);
  const taskDoDs = new Map([['task-a', 'A done'], ['task-b', '']]);
  let failListMode = '';
  let presplitMode = 'success'; let presplitRequest = null; let presplitGenerated = false;
  const server = http.createServer(async (req, res) => {
    if (String(req.url).startsWith('/fixture/presplit-mode') && req.method === 'POST') {
      presplitMode = new URL(req.url, 'http://fixture').searchParams.get('mode') || 'success'; presplitRequest = null; presplitGenerated = false;
      res.writeHead(204); res.end(); return;
    }
    if (req.url === '/fixture/populate-pages' && req.method === 'POST') {
      for (let i = 0; i < 20; i++) { const item={id:`page-spec-${i}`,owner_short_name:'甲',current_editor_short_name:'甲',title:`Page ${i}`,state:'editing',task_state:'splitting',current_revision:1,can_edit:true,can_split:true}; specs.push(item); revisions.set(item.id,{spec_id:item.id,revision:1,content_markdown:'# page',acceptance_json:'[]',input_anchors_json:'[]'}); }
      res.writeHead(204); res.end(); return;
    }
    if (String(req.url).startsWith('/console/specs')) {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(html); return;
    }
    if (req.url === '/view/parity-session') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(fs.readFileSync(RENDERER)); return;
    }
    if (req.url === '/view/parity-session/status') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ session: 'parity-session', auth_mode: 'legacy', readonly: true, state: 'idle' })); return;
    }
    if (req.url === '/f/spec/good/26/0921/0123456789abcdef0123456789abcdef.png') {
      res.writeHead(200, { 'Content-Type': 'image/png' });
      res.end(Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64')); return;
    }
    if (req.url === '/f/spec/missing/26/0921/ffffffffffffffffffffffffffffffff.png') {
      res.writeHead(404, { 'Content-Type': 'text/plain' }); res.end('gone'); return;
    }
    if (req.url === '/console/spec-assets/easymde.min.js') {
      res.writeHead(200, { 'Content-Type': 'text/javascript' }); res.end(fs.readFileSync(path.join(ASSET_DIR, 'easymde.min.js'))); return;
    }
    if (req.url === '/console/spec-assets/easymde.min.css') {
      res.writeHead(200, { 'Content-Type': 'text/css' }); res.end(fs.readFileSync(path.join(ASSET_DIR, 'easymde.min.css'))); return;
    }
    if (String(req.url).startsWith('/fixture/fail-list') && req.method === 'POST') {
      failListMode = new URL(req.url, 'http://fixture').searchParams.get('mode') || '';
      res.writeHead(204); res.end(); return;
    }
    if (String(req.url).startsWith('/api/console/specs?') && req.method === 'GET') {
      if (failListMode === '500') { res.writeHead(500, { 'Content-Type': 'text/plain; charset=utf-8' }); res.end('fixture list unavailable'); return; }
      if (failListMode === 'network') { req.socket.destroy(); return; }
      const page = Number(new URL(req.url, 'http://fixture').searchParams.get('page') || 1); const start=(page-1)*20;
      const own=specs;
      res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ role: 'management', principal: 'owner-a', specs:own.slice(start,start+20), page, page_size:20, total:own.length, total_pages:Math.max(1,Math.ceil(own.length/20)) })); return;
    }
    if (req.url === '/api/console/specs' && req.method === 'POST') {
      const body = await requestBody(req);
      if (body.title === 'server-reject') {
        res.writeHead(400, { 'Content-Type': 'text/plain; charset=utf-8' }); res.end('server create detail'); return;
      }
      const item = { id: `spec-${specs.length + 1}`, owner_short_name:'甲',current_editor_short_name:'甲',title: body.title, state: 'editing',task_state:'splitting',current_revision: 1,can_edit:true,can_split:true };
      specs.push(item); revisions.set(item.id, { ...body, spec_id: item.id, revision: 1,
        acceptance_json: JSON.stringify(body.acceptance), input_anchors_json: JSON.stringify(body.input_anchors) });
      res.writeHead(201, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(item)); return;
    }
    const match = String(req.url || '').match(/^\/api\/console\/specs\/([^/]+)$/);
    if (match && req.method === 'GET') {
      const item = specs.find((candidate) => candidate.id === match[1]);
      if (!item) { res.writeHead(404); res.end(); return; }
      res.writeHead(200, { 'Content-Type': 'application/json' });
      const aLevel=taskDeps.get('task-a').includes('task-b')?1:0,bLevel=taskDeps.get('task-b').includes('task-a')?1:0;const parallel=aLevel===bLevel;
      const taskA={proposal_id:'task-a',spec_id:'spec-1',revision:2,title:'Implement A',content:'task content',status:'planned',estimated_hours:5,depends_on_json:JSON.stringify(taskDeps.get('task-a')),acceptance_json:taskDoDs.get('task-a')?JSON.stringify([{id:'DOD',text:taskDoDs.get('task-a')}]) : '[]',depended_by:['task-c'].concat(taskDeps.get('task-b').includes('task-a')?['task-b']:[]),dependency_level:aLevel,parallel};
      const taskB={proposal_id:'task-b',spec_id:'spec-1',revision:2,title:'Implement B',content:'dependency',status:'planned',estimated_hours:2,depends_on_json:JSON.stringify(taskDeps.get('task-b')),acceptance_json:taskDoDs.get('task-b')?JSON.stringify([{id:'DOD',text:taskDoDs.get('task-b')}]) : '',depended_by:taskDeps.get('task-a').includes('task-b')?['task-a']:[],dependency_level:bLevel,parallel};
      const ordered=[taskA,taskB].sort((a,b)=>a.dependency_level-b.dependency_level||a.proposal_id.localeCompare(b.proposal_id));
      const generated={proposal_id:'task-presplit',spec_id:item.id,revision:1,title:'Agent task',content:'generated',status:'planned',estimated_hours:2,depends_on_json:'[]',acceptance_json:'[]',depended_by:[],dependency_level:0,parallel:false};
      res.end(JSON.stringify({ spec: item, revision: revisions.get(item.id), tasks:item.id==='spec-1'?ordered:(presplitGenerated&&item.id==='spec-2'?[generated]:[]), dependency_candidates:[taskA,taskB] })); return;
    }
    if(req.url==='/api/console/spec-editors'){res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify({editors:[{short_name:'甲'},{short_name:'乙'},{short_name:'丙'}]}));return;}
    const presplitMatch=String(req.url||'').match(/^\/api\/console\/specs\/([^/]+)\/presplit$/);
    if(presplitMatch&&req.method==='GET'){const agents=presplitMode==='no-agents'?[]:[{id:'agent-a',session_name:'owner-agent-2642'}];res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify({agents,request:presplitRequest}));return;}
    if(presplitMatch&&req.method==='POST'){await requestBody(req);if(presplitMode==='no-reply'){presplitRequest={request_id:'r-no-reply',status:'failed',failure_reason:'no reply'};res.writeHead(502);res.end('预拆失败，可手工添加');return;}const failure={timeout:'timeout',invalid:'invalid task format',restart:'restart'}[presplitMode];presplitRequest={request_id:'r-'+presplitMode,status:failure?'failed':'completed',failure_reason:failure||''};if(!failure)presplitGenerated=true;res.writeHead(202,{'Content-Type':'application/json'});res.end(JSON.stringify({request_id:presplitRequest.request_id,status:'pending'}));return;}
    const planMatch=String(req.url||'').match(/^\/api\/console\/task-proposals\/([^/]+)\/plan$/);
    if(planMatch&&req.method==='PATCH'){const body=await requestBody(req);const id=planMatch[1];taskPlans.push({id,body});if(body.depends_on.includes(id)){res.writeHead(409);res.end('task cannot depend on itself');return;}const next=new Map(taskDeps);next.set(id,body.depends_on);if((next.get('task-a')||[]).includes('task-b')&&(next.get('task-b')||[]).includes('task-a')){res.writeHead(409);res.end('task dependency cycle');return;}taskDeps.set(id,body.depends_on);taskDoDs.set(id,body.definition_of_done||'');res.writeHead(204);res.end();return;}
    const taskDeleteMatch=String(req.url||'').match(/^\/api\/console\/task-proposals\/([^/]+)$/);
    if(taskDeleteMatch&&req.method==='DELETE'){if(taskDeleteMatch[1]==='task-presplit')presplitGenerated=false;res.writeHead(204);res.end();return;}
    const splitMatch=String(req.url||'').match(/^\/api\/console\/specs\/([^/]+)\/tasks$/);
    if(splitMatch&&req.method==='POST'){taskCreates.push({specID:splitMatch[1],body:await requestBody(req)});res.writeHead(201,{'Content-Type':'application/json'});res.end(JSON.stringify({proposal_id:'task-new'}));return;}
    if (match && req.method === 'PATCH') {
      const body = await requestBody(req); const item = specs.find((candidate) => candidate.id === match[1]);
      if (body.content_markdown === 'server-save-reject') {
        res.writeHead(409, { 'Content-Type': 'text/plain; charset=utf-8' }); res.end('server save detail'); return;
      }
      item.current_revision += 1;
      revisions.set(item.id, { ...body, spec_id: item.id, revision: item.current_revision,
        acceptance_json: JSON.stringify(body.acceptance), input_anchors_json: JSON.stringify(body.input_anchors) });
      res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ revision: item.current_revision })); return;
    }
    const action = String(req.url || '').match(/^\/api\/console\/specs\/([^/]+)\/(submit|archive|claim|reviews|reopen-editing)$/);
    if (action && req.method === 'POST') {
      if (action[2] === 'archive') {
        res.writeHead(409, { 'Content-Type': 'text/plain; charset=utf-8' }); res.end('already archived'); return;
      }
      if (action[2] === 'reopen-editing') {
        const item = specs.find((candidate) => candidate.id === action[1]);
        if (!item || item.principal_id !== 'owner-a') { res.writeHead(404); res.end('not found'); return; }
        item.editing_closed = false;
      }
      res.writeHead(204); res.end(); return;
    }
    res.writeHead(404); res.end('not found');
  });

  const port = await listen(server);
  const launchOptions = { headless: true };
  if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
  const browser = await chromium.launch(launchOptions);
  try {
    const context = await browser.newContext();
    await context.addInitScript(() => { Object.defineProperty(navigator, 'clipboard', { value: { writeText: async (text) => { globalThis.__copiedCode = text; } }, configurable: true }); });
    const page = await context.newPage(); page.setDefaultTimeout(2500);
    const pageErrors = [], consoleErrors = [];
    page.on('pageerror', (error) => pageErrors.push(error.message));
    // 🔵 连 URL 一起记:否则放行某个状态码就只能在全站范围放行,
    //    而本套件里 500 是 S-5 那一格【故意注入】的,只该在列表端点上被放行。
    page.on('console', (message) => { if (message.type() === 'error') consoleErrors.push({ text: message.text(), url: message.location().url || '' }); });
    await page.goto(`http://127.0.0.1:${port}/console/specs`, { waitUntil: 'domcontentloaded' });
    assert.strictEqual(await page.locator('#list-screen').isVisible(), true, 'entry must show my spec list first');
    assert.strictEqual(await page.locator('#new-screen').isHidden(), true, 'new form must not share the first screen');
    assert.strictEqual(await page.locator('#detail-screen').isHidden(), true, 'detail must be empty before a spec is selected');
    assert.strictEqual(await page.locator('#task-area').isHidden(), true, 'task area must be empty before a spec is selected');
    await page.waitForFunction(()=>document.getElementById('page-status').textContent!=='');assert.strictEqual(await page.locator('#empty-create-spec').count(),1,'empty list must expose exactly one create-spec link');await page.click('#empty-create-spec');
    assert.strictEqual(await page.locator('#new-screen').isVisible(), true, 'empty list create link must open a separate create screen');
    const validNew = { title: 'Valid', markdown: '# Valid', acceptance: 'accepted', anchor: 'msg:valid' };
    async function setNew(values) {
      await page.fill('#new-title', values.title);
      await page.evaluate((value) => window.specEditors.new.value(value), values.markdown);
      await page.fill('#new-acceptance', values.acceptance);
      await page.fill('#new-anchor', values.anchor);
    }
    for (const [field, selector, label] of [
      ['title', '#new-title', 'Title'], ['markdown', '#new-content', 'Markdown'],
      ['acceptance', '#new-acceptance', 'Acceptance'], ['anchor', '#new-anchor', 'Owner message anchor'],
    ]) {
      await setNew({ ...validNew, [field]: '' });
      await page.click('#create-spec');
      await page.locator('#create-status').filter({ hasText: label }).waitFor({ state: 'visible' });
      assert.strictEqual(specs.length, 0, `missing ${field} must be blocked before POST`);
    }
    await setNew({ ...validNew, title: 'server-reject' });
    await page.click('#create-spec');
    await page.locator('#create-status').filter({ hasText: '创建失败 400:server create detail' }).waitFor({ state: 'visible' });
    assert.strictEqual(specs.length, 0, 'server-rejected create must not create a spec');
    assert.strictEqual(await page.locator('label .required').count(), 8, 'both create and edit forms must mark all four required fields');
    for (const title of ['Spec A', 'Spec B']) {
      await page.click('#show-new-spec');
      await page.fill('#new-title', title);
      await page.evaluate((value) => window.specEditors.new.value(value), `# ${title}`);
      await page.fill('#new-acceptance', `${title} accepted`);
      await page.fill('#new-anchor', `msg:${title.slice(-1)}`);
      await page.click('#create-spec');
      await page.locator(`#spec-list [data-title="${title}"]`).waitFor({ state: 'visible' });
    }
    assert.strictEqual(await page.locator('#spec-list [data-spec-id]').count(), 2, 'true page must show both specs at once');
    for (const title of ['Spec A', 'Spec B']) {
      const row = page.locator(`#spec-list [data-title="${title}"]`);
      assert.strictEqual(await row.locator('.edit-spec').count(), 1, 'management owner must see Edit for own spec');
    }
    async function openPresplitMode(mode) {
      await page.request.post(`http://127.0.0.1:${port}/fixture/presplit-mode?mode=${mode}`);
      await page.click('#show-spec-list');
      await page.locator('#spec-list [data-title="Spec B"] .view-spec').click();
      await page.locator('#agent-presplit').waitFor({state:'visible'});
    }
    await openPresplitMode('no-agents');
    assert.strictEqual(await page.locator('#presplit-status').textContent(),'没有自己的在线 Agent');
    assert.strictEqual(await page.locator('#start-presplit').isDisabled(),true);
    const targetSilentMode = MUTATION.startsWith('silent-presplit-') ? MUTATION.slice('silent-presplit-'.length) : '';
    for (const mode of targetSilentMode ? [targetSilentMode] : ['no-reply','timeout','invalid','restart']) {
      await openPresplitMode(mode);
      assert.deepStrictEqual(await page.locator('#presplit-agent option').allTextContents(),['请选择 Agent','owner-agent-2642']);
      assert.strictEqual(await page.locator('#start-presplit').isDisabled(),true,'Agent must be explicitly selected');
      await page.selectOption('#presplit-agent','agent-a');
      await page.click('#start-presplit');
      await page.waitForTimeout(100);
      assert.strictEqual(await page.locator('#presplit-status').textContent(),'预拆失败，可手工添加',`${mode}: failure must be visible instead of leaving only an apparently successful empty task table`);
      assert.strictEqual((await page.locator('#task-list').textContent()).trim(),'尚未拆出任务',`${mode} must remain visibly failed instead of looking like a successful empty task table`);
    }
    await openPresplitMode('success');
    await page.selectOption('#presplit-agent','agent-a');
    await page.click('#start-presplit');
    await page.locator('#presplit-status').filter({hasText:'预拆完成'}).waitFor({state:'visible'});
    await page.locator('#task-list [data-task-id="task-presplit"]').waitFor({state:'visible'});
    await page.locator('#task-list [data-task-id="task-presplit"] .delete-task').click();
    await page.locator('#task-list [data-task-id="task-presplit"]').waitFor({state:'detached'});
    await page.locator('#spec-list [data-title="Spec A"] .view-spec').click();await page.locator('#detail-screen').waitFor({state:'visible'});assert.notStrictEqual((await page.locator('#spec-rendered').textContent()).trim(),'');
    await page.click('#show-new-spec');
    async function keyboardReplace(selector,value){await page.locator(selector).click();await page.keyboard.press(process.platform==='darwin'?'Meta+A':'Control+A');await page.keyboard.type(value);}
    await keyboardReplace('#new-title','Created after detail');await page.locator('#new-screen .CodeMirror').click();await page.keyboard.press(process.platform==='darwin'?'Meta+A':'Control+A');await page.keyboard.type('# Created after detail');await keyboardReplace('#new-acceptance','created accepted');await keyboardReplace('#new-anchor','msg:created');await page.click('#create-spec');
    await page.locator('#spec-list [data-title="Created after detail"]').waitFor({state:'visible'});assert.strictEqual(await page.locator('#detail-screen').isHidden(),true,'creation list refresh must hide prior detail');assert.strictEqual((await page.locator('#spec-rendered').textContent()).trim(),'','creation list refresh must clear prior detail DOM');assert.strictEqual((await page.locator('#task-list').textContent()).trim(),'','creation list refresh must clear prior task DOM');
    await page.request.post(`http://127.0.0.1:${port}/fixture/populate-pages`);await page.click('#show-spec-list');await page.waitForFunction(()=>!document.getElementById('next-page').disabled);
    await page.locator('#spec-list [data-title="Spec A"] .view-spec').click();await page.locator('#detail-screen').waitFor({state:'visible'});await page.click('#next-page');
    assert.strictEqual(await page.locator('#detail-screen').isHidden(),true,'pagination must clear selected spec detail');assert.strictEqual(await page.locator('#task-area').isHidden(),true,'pagination must clear selected task area');assert.strictEqual(await page.locator('#task-list').textContent(),'','pagination must clear rendered tasks');
    await page.waitForFunction(()=>document.getElementById('page-status').textContent==='2 / 2');await page.click('#prev-page');await page.locator('#spec-list [data-title="Spec A"] .view-spec').waitFor({state:'visible'});await page.locator('#spec-list [data-title="Spec A"] .view-spec').click();
    await page.locator('#detail-screen').waitFor({state:'visible'});
    assert.strictEqual(await page.locator('#list-screen').isVisible(), true, 'opening detail must keep the spec list visible above it');
    assert.strictEqual(await page.locator('#detail-screen').evaluate((node) => node.previousElementSibling && node.previousElementSibling.id), 'list-screen', 'detail must be the list following sibling');
    const task=page.locator('#task-list .task-row[data-task-id="task-a"]');await task.waitFor({state:'visible'});
    assert.strictEqual(await task.getAttribute('id'),'task-row-task-a');
    assert.strictEqual(await task.locator('.task-dod').getAttribute('id'),'task-row-task-a-dod');
    assert.deepStrictEqual(await page.locator('#task-list .task-row').evaluateAll((rows)=>rows.map((r)=>r.dataset.taskId)),['task-a','task-b']);
    assert.strictEqual(await page.locator('#task-list .task-row[data-task-id="task-a"] .meta').textContent().then((x)=>x.includes('可并行')),true);
    assert.strictEqual(await page.locator('#task-list .task-row[data-task-id="task-b"] .task-dod').inputValue(),'');
    assert.strictEqual(await task.locator('strong').textContent(),'Implement A');
    assert.strictEqual(await task.locator('.task-content').textContent(),'task content');
    assert.match(await task.locator('.meta').textContent(),/状态 planned/);
    assert.match(await task.locator('.meta').textContent(),/被依赖 task-c/);
    assert.strictEqual(await task.locator('.task-hours').inputValue(),'5');
    assert.strictEqual(await task.locator('.task-deps option:checked').count(),0);
    assert.strictEqual(await page.locator('#show-task-split').isVisible(),true,'selected splittable spec must show a task split entry');assert.strictEqual(await page.locator('#task-split').isHidden(),true,'task split form must stay hidden before clicking entry');await page.click('#show-task-split');assert.strictEqual(await page.locator('#task-split').isVisible(),true,'clicking task split entry must reveal the form');
    await task.locator('.task-dod').fill('A complete');await task.locator('.task-hours').fill('16');
    assert.strictEqual(await task.locator('.task-hours-hint').textContent(),'建议再拆');
    await task.locator('.task-deps').selectOption(['task-a']);await task.locator('.save-task-plan').click();
    await task.locator('.task-status').filter({hasText:'task cannot depend on itself'}).waitFor({state:'visible'});
    await task.locator('.task-deps').selectOption(['task-b']);await task.locator('.save-task-plan').click();
    await page.waitForTimeout(100);
    assert.deepStrictEqual(taskPlans.slice(0,2),[
      {id:'task-a',body:{content:'task content',status:'planned',estimated_hours:16,depends_on:['task-a'],definition_of_done:'A complete'}},
      {id:'task-a',body:{content:'task content',status:'planned',estimated_hours:16,depends_on:['task-b'],definition_of_done:'A complete'}},
    ]);
    assert.deepStrictEqual(await page.locator('#task-list .task-row').evaluateAll((rows)=>rows.map((r)=>r.dataset.taskId)),['task-b','task-a']);
    assert.strictEqual(await page.locator('#task-list .task-row[data-task-id="task-a"] .task-dod').inputValue(),'A complete');
    assert.strictEqual(await page.locator('#task-list .task-row[data-task-id="task-a"] .meta').textContent().then((x)=>x.includes('顺序执行')),true);
    const taskB=page.locator('#task-list .task-row[data-task-id="task-b"]');await taskB.locator('.task-deps').selectOption(['task-a']);await taskB.locator('.save-task-plan').click();
    await taskB.locator('.task-status').filter({hasText:'task dependency cycle'}).waitFor({state:'visible'});
    await page.click('#show-task-split');await page.fill('#task-title','Implement new');await page.fill('#task-content','new content');await page.fill('#task-dod','New done');await page.fill('#task-hours','16');
    assert.strictEqual(await page.locator('#task-hours-hint').textContent(),'建议再拆');await page.selectOption('#task-deps',['task-b']);await page.click('#create-task');
    await page.waitForFunction(() => document.getElementById('task-create-status').textContent === '已拆解');
    assert.deepStrictEqual(taskCreates[0],{specID:'spec-1',body:{title:'Implement new',content:'new content',definition_of_done:'New done',estimated_hours:16,depends_on:['task-b']}});
    for (const title of ['Spec A', 'Spec B']) {
      await page.click('#show-spec-list');
      await page.locator(`#spec-list [data-title="${title}"] .edit-spec`).click();
      await page.waitForFunction((want) => document.getElementById('edit-title').value === want, title);
      assert.strictEqual(await page.inputValue('#edit-title'), title, `edit form must open ${title}`);
      await page.locator('#edit-rendered h1').filter({ hasText: title }).waitFor({ state: 'visible' });
      const changed = title === 'Spec A'
        ? `# ${title} changed\n\n\`\`\`python\nprint("copy-me")\n\`\`\`\n\n![good](http://127.0.0.1:${port}/f/spec/good/26/0921/0123456789abcdef0123456789abcdef.png)\n\n![missing](http://127.0.0.1:${port}/f/spec/missing/26/0921/ffffffffffffffffffffffffffffffff.png)`
        : `# ${title} changed`;
      await page.evaluate((value) => window.specEditors.edit.value(value), changed);
      await page.click('#save-spec');
      await page.locator('#edit-status').filter({ hasText: 'saved revision 2' }).waitFor({ state: 'visible' });
      if (title === 'Spec A') {
        await page.locator('#edit-rendered h1').filter({ hasText: 'Spec A changed' }).waitFor({ state: 'visible' });
        await page.locator('#edit-rendered img.md-img[alt="good"]').waitFor({ state: 'visible' });
        await page.locator('#edit-rendered .md-image-fallback').waitFor({ state: 'visible' });
        assert.strictEqual(await page.locator('#edit-rendered img.md-img').count(), 1, 'working image must remain inline');
        assert.strictEqual(await page.locator('#edit-rendered .md-image-fallback').getAttribute('href'), `http://127.0.0.1:${port}/f/spec/missing/26/0921/ffffffffffffffffffffffffffffffff.png`, 'failed image must retain a direct download exit');
        assert.strictEqual(await page.locator('#edit-rendered img.md-img').evaluate((el) => getComputedStyle(el).maxHeight), '420px', 'shared renderer CSS contract must be present');
        await page.locator('#edit-rendered .copy-code').click();
        assert.strictEqual(await page.evaluate(() => globalThis.__copiedCode), 'print("copy-me")', 'Copy button must be wired to the rendered code');
        const timeline = await context.newPage();
        await timeline.goto(`http://127.0.0.1:${port}/view/parity-session`, { waitUntil: 'domcontentloaded' });
        const timelineRendered = await timeline.evaluate((markdown) => renderMarkdown(markdown), changed);
        const specCoreRendered = await page.evaluate((markdown) => DwMdCore.renderMarkdown(markdown), changed);
        assert.strictEqual(specCoreRendered, timelineRendered, 'spec and timeline must render identical bytes at the real spec origin');
        assert.strictEqual(await page.locator('#edit-rendered > .md').count(), 1, 'the spec page must mount the shared renderer output into its visible DOM');
        assert.strictEqual(await page.evaluate((origin) => DwMdCore.isOwnFileUrl(origin + '/f/spec/good/26/0921/0123456789abcdef0123456789abcdef.png'), `http://127.0.0.1:${port}`), true, 'shared own-file boundary must use the real spec page origin');
        await timeline.close();
      }
    }
    await page.evaluate(() => openSpec('spec-2'));
    await page.evaluate(() => window.specEditors.edit.value('server-save-reject'));
    await page.click('#save-spec');
    await page.waitForTimeout(100);
    assert.strictEqual(await page.locator('#edit-status').textContent(), '保存失败 409:server save detail', 'save must expose status and server response body');
    assert.strictEqual(specs.find((item) => item.title === 'Spec B').current_revision, 2, 'server-rejected save must not create a revision');
    assert.deepStrictEqual(pageErrors, [], `failed specAction must be handled on-page: ${JSON.stringify(pageErrors)}`);
    await page.reload({ waitUntil: 'domcontentloaded' });
    assert.strictEqual(await page.locator('#spec-list [data-spec-id]').count(), 20, 'reload must retain the first owned-spec page');
    assert.strictEqual(await page.locator('#spec-list [data-title="Spec A"]').count(),1);assert.strictEqual(await page.locator('#spec-list [data-title="Spec B"]').count(),1);
    await page.click('#show-new-spec');
    await page.evaluate(() => window.specEditors.new.value('editor contract'));
    await page.locator('.CodeMirror-line').first().waitFor({state:'attached'});
    const editorContract = await page.evaluate(() => ({
      spell: window.specEditors.new.options.spellChecker,
      fa: window.specEditors.new.options.autoDownloadFontAwesome,
      autosave: window.specEditors.new.options.autosave.enabled,
      previewButtons: document.querySelectorAll('.editor-toolbar .preview,.editor-toolbar .side-by-side').length,
      autosaveKeys: Object.keys(localStorage).filter((key) => /easymde|smde/i.test(key)),
      lineTag: document.querySelector('.CodeMirror-line').tagName,
      lineWhiteSpace: getComputedStyle(document.querySelector('.CodeMirror-line')).whiteSpace,
    }));
    assert.deepStrictEqual(editorContract, { spell: false, fa: false, autosave: false, previewButtons: 0, autosaveKeys: [], lineTag: 'PRE', lineWhiteSpace: 'pre-wrap' });
    const button = page.locator('.editor-toolbar .dw-bold').first();
    const normal = await button.evaluate((el) => getComputedStyle(el).backgroundColor);
    await button.hover();
    const hover = await button.evaluate((el) => getComputedStyle(el).backgroundColor);
    assert.notStrictEqual(hover, normal, 'toolbar hover background must be explicit and visible');
    await button.evaluate((el) => el.classList.add('active'));
    const active = await button.evaluate((el) => getComputedStyle(el).backgroundColor);
    assert.notStrictEqual(active, normal, 'toolbar active background must be explicit and visible');
    const wrapMetrics = await page.evaluate(() => {
      window.specEditors.new.value('x'.repeat(800));
      const scroll = document.querySelector('.CodeMirror-scroll');
      const line = document.querySelector('.CodeMirror-line');
      return { overflow: scroll.scrollWidth - scroll.clientWidth,
        lineOverhang: Math.max(0, Math.round(line.getBoundingClientRect().right - scroll.getBoundingClientRect().right)) };
    });
    assert.strictEqual(wrapMetrics.overflow, 0, `editor line overflowed: ${JSON.stringify(wrapMetrics)}`);
    assert.strictEqual(wrapMetrics.lineOverhang, 0, `editor line overhung viewport: ${JSON.stringify(wrapMetrics)}`);
    // ㉟ 打开【修改】后,人必须立刻能干活:焦点在编辑器里、编辑器在眼前、内容真的画出来了 ——————
    // 🔴 业主症状「滚不动 / 按键无响应」的定性(件 SPECS-EDITOR-REPRO-20260923):
    //    甲 焦点默认在 BODY ⇒ 不点就打字,键一个都不进编辑器 ——「不是响应慢,是根本没收到」
    //    乙 渲染预览排在编辑器上方,长文时编辑器在第 20 屏开外 ⇒ 点完 Edit 第一屏看不见编辑框
    //    丙 内容在 #editor 仍 hidden 时灌入、无人 refresh ⇒ CodeMirror 沿用上一份的尺寸、只补 1 行
    //       🔴 丙最直指根因;三档都必须红,**1KB 也红** —— 尺寸是放大器,不是根因
    // 🔴 必须走【新建 → openSpec(id,false)】这条真实路径:在 new 屏上直接 setValue 时编辑器是可见的,
    //    丙 不会复现。本轮实测栽过一次:测试自己调了 refresh,把丙盖成假绿。
    const ownerReal = fs.readFileSync(path.join(__dirname, 'fixtures-specs', 'owner-spec-20260923.txt'), 'utf8');
    const ownerProfile = (() => {
      const lines = ownerReal.split('\n');
      const lens = lines.map((line) => line.length).sort((a, b) => a - b);
      return { bytes: Buffer.byteLength(ownerReal), lines: lines.length,
        blank: lines.filter((line) => !line.trim()).length,
        median: lens[Math.floor(lens.length / 2)], max: lens[lens.length - 1],
        cjk: (ownerReal.match(/[一-鿿]/g) || []).length,
        markdown: (ownerReal.match(/^#|```|^\s*\|/gm) || []).length };
    })();
    assert.deepStrictEqual(ownerProfile, { bytes: 15423, lines: 199, blank: 63, median: 40, max: 466, cjk: 0, markdown: 0 },
      `业主真实素材画像不符,素材可能被换过: ${JSON.stringify(ownerProfile)}`);
    const filler = '## 小节\n\n正文,中英混排 mixed content for realism.\n\n- 要点一\n- 要点二\n\n';
    const editorCases = [
      { label: '1KB(阳性对照:也必须红)', markdown: filler.repeat(10) },
      { label: '业主真实正文 15423B', markdown: ownerReal },
      { label: '30KB', markdown: filler.repeat(420) },
    ];
    for (const editorCase of editorCases) {
      let caseSpecID = '';
      const grabCase = (resp) => {
        if (resp.url().endsWith('/api/console/specs') && resp.request().method() === 'POST' && resp.status() === 201) {
          resp.json().then((body) => { caseSpecID = body.id; }).catch(() => {});
        }
      };
      page.on('response', grabCase);
      await page.click('#show-new-spec');
      await setNew({ title: `Editor-${editorCase.label}`, markdown: editorCase.markdown, acceptance: 'usable', anchor: 'msg:editor' });
      await page.click('#create-spec');
      await page.waitForSelector('#list-screen:not([hidden])');
      for (let i = 0; i < 40 && !caseSpecID; i++) await page.waitForTimeout(100);
      page.off('response', grabCase);
      assert.ok(caseSpecID, `未能取到新建 spec 的 id(${editorCase.label})`);
      await page.evaluate((id) => window.openSpec(id, false), caseSpecID);
      await page.waitForSelector('#editor:not([hidden])');
      await page.waitForFunction((n) => window.specEditors.edit.value().length >= n, editorCase.markdown.length);
      await page.waitForTimeout(300);
      const usable = await page.evaluate(() => {
        const cm = window.specEditors.edit.codemirror;
        const wrap = cm.getWrapperElement();
        const rect = wrap.getBoundingClientRect();
        const cx = Math.round(rect.left + rect.width / 2);
        const cy = Math.round(rect.top + Math.min(rect.height, innerHeight) / 2);
        const hit = document.elementFromPoint(cx, cy);
        const active = document.activeElement;
        return {
          focusInEditor: !!(active && active.closest && active.closest('.CodeMirror')),
          activeTag: active ? active.tagName + (active.id ? '#' + active.id : '') : null,
          hitInEditor: !!(hit && hit.closest && hit.closest('.CodeMirror')),
          hitTag: hit ? hit.tagName + (hit.id ? '#' + hit.id : '') : null,
          centerY: cy, viewportH: innerHeight, wrapTop: Math.round(rect.top),
          renderedLines: wrap.querySelectorAll('.CodeMirror-line').length, lineCount: cm.lineCount(),
        };
      });
      assert.ok(usable.focusInEditor,
        `${editorCase.label}:打开修改后焦点在 ${usable.activeTag},不在编辑器里 ⇒ 不点就打字,键一个都不进去`);
      assert.ok(usable.hitInEditor,
        `${editorCase.label}:编辑器中心点命中的是 ${usable.hitTag}(中心 y=${usable.centerY},视口高 ${usable.viewportH},编辑器顶 ${usable.wrapTop})⇒ 点完 Edit 第一屏看不见编辑框`);
      assert.strictEqual(usable.renderedLines, usable.lineCount,
        `${editorCase.label}:编辑器只渲染了 ${usable.renderedLines} 行,而这份 spec 有 ${usable.lineCount} 行 ⇒ 尺寸没重算(沿用了上一份)`);
      // 丁 🔴 纪律十:「点得到」之外必须断言「点了/键了**有效果**」。
      //    甲只证明 activeElement 在编辑器里,证不了键真的进得去 ——
      //    本线今天栽过同型的一次:元素存在、可见、中心点命中自身,而 onclick 是 null(点得到、点了没用)。
      //    ⇒ 这里**不做任何点击**,直接敲键:业主的真实动作就是"打开就打字"。
      const beforeType = await page.evaluate(() => window.specEditors.edit.value().length);
      await page.keyboard.type('ZZ');
      await page.waitForTimeout(200);
      const afterType = await page.evaluate(() => ({
        len: window.specEditors.edit.value().length,
        head: window.specEditors.edit.value().slice(0, 4) }));
      assert.ok(afterType.len > beforeType,
        `${editorCase.label}:打开修改后【不点击】直接键入,编辑器内容没有变化(${beforeType} → ${afterType.len}) ⇒ 键根本没进编辑器`);
      console.log(`editor usable ${editorCase.label}: 焦点=${usable.activeTag} 命中=${usable.hitTag} 渲染行=${usable.renderedLines}/${usable.lineCount} 直接键入=${beforeType}→${afterType.len}`);
    }
    // ㊱ S-5:「我的 spec」刷新失败**不得静默** ——————————————————————————————
    // 🔴 业主实测:新建 spec 后回列表**没有变化**;关闭 tab 再打开(重新挂载走另一条路径)才出现。
    //    产品侧成因(读码):`show-spec-list` 的 onclick 是
    //      showScreen('list-screen');refreshSpecs();      ← 既没 await 也没 catch
    //    而 refreshSpecs 内部 `if(!r.ok)throw new Error('list failed')`
    //    ⇒ 请求一旦不 ok:**未捕获的 promise 异常**,列表保持旧内容,**一个字都不显示**。
    // 🔴 本格按组长要求覆盖**两种**:① 失败必须有可见文案 ② 成功必须真的换了内容
    //    (只验①会被"永远不刷新"骗过;只验②会被"失败时静默保留旧内容"骗过 —— 两条缺一不可。)
    const listFail = async (mode) => {
      const res = await page.request.post(`http://127.0.0.1:${port}/fixture/fail-list?mode=${mode}`);
      assert.strictEqual(res.status(), 204, 'fixture 开关必须生效');
    };
    // ① 失败:必须出现可见文案,且不得抛未捕获异常
    const errorsBefore = pageErrors.length;
    await listFail('500');
    await page.click('#show-spec-list');
    await page.waitForTimeout(800);
    const listFailState = await page.evaluate(() => {
      const listText = (document.getElementById('spec-list') || {}).innerText || '';
      const screenText = (document.getElementById('list-screen') || {}).innerText || '';
      return { listText: listText.slice(0, 200), visibleError: /失败|错误|不可用|重试/.test(screenText),
        retry: !!document.querySelector('#retry-specs'),
        retryVisible: !!(document.querySelector('#retry-specs') && document.querySelector('#retry-specs').offsetParent) };
    });
    assert.ok(listFailState.visibleError,
      `列表刷新失败时页面上没有任何可见文案(列表区文本: ${JSON.stringify(listFailState.listText)}) ⇒ 业主看到的就是"点了没反应"`);
    assert.ok(listFailState.retryVisible,
      '列表刷新失败时必须给一个可见的重试入口,否则业主只能关 tab 重开');
    assert.deepStrictEqual(pageErrors.slice(errorsBefore), [],
      `列表刷新失败时抛了未捕获异常: ${JSON.stringify(pageErrors.slice(errorsBefore))}`);
    // ② 成功:必须真的换了内容(防"永远不刷新"也能过①)
    // 🔵 判据取「失败态被真实替换掉」而不是「某个标题出现」——
    //    列表是分页的(每页 20),前面的用例已灌进 20+ 条,新建的那份未必落在第一页;
    //    用标题去断言会因为分页而假红,那是**装置的噪声**,不是产品行为。
    await listFail('');
    await page.click('#retry-specs');
    await page.waitForFunction(() => !document.getElementById('spec-list-error'), null, { timeout: 5000 })
      .catch(() => { throw new assert.AssertionError({ message: '点「重试」之后错误行仍在 ⇒ 重试入口没有真的重刷' }); });
    const listOkState = await page.evaluate(() => ({
      pageStatus: (document.getElementById('page-status') || {}).textContent || '',
      rows: document.querySelectorAll('.spec-row:not(.task-row)').length,
      errorGone: !document.getElementById('spec-list-error'),
    }));
    assert.ok(listOkState.errorGone, '重试成功后错误行必须消失');
    assert.notStrictEqual(listOkState.pageStatus, '', '重试成功后分页状态必须被重新写上(证明真的取了新数据)');
    assert.ok(listOkState.rows > 0, '重试成功后列表必须重新渲染出行');
    console.log(`S-5: 失败有文案=${listFailState.visibleError} 重试可见=${listFailState.retryVisible} 重试后错误行消失=${listOkState.errorGone} 分页状态=${JSON.stringify(listOkState.pageStatus)} 行数=${listOkState.rows}`);
    assert.deepStrictEqual(pageErrors, [], `spec page emitted top-level errors: ${JSON.stringify(pageErrors)}`);
    const unexpectedConsole = consoleErrors.filter((entry) => {
      if (/^Failed to load resource: the server responded with a status of (400|404|409|502)/.test(entry.text)) return false;
      // S-5 故意把列表端点打成 500;只在该端点上放行,别处的 500 仍然算缺陷
      if (/status of 500/.test(entry.text) && /\/api\/console\/specs\?/.test(entry.url)) return false;
      return true;
    });
    assert.deepStrictEqual(unexpectedConsole, [], `spec page emitted unexpected console errors: ${JSON.stringify(unexpectedConsole)}`);
    console.log(`editor wrap metrics overflow=${wrapMetrics.overflow} lineOverhang=${wrapMetrics.lineOverhang}`);
    console.log('console specs browser: passed=34 failed=0');
  } finally {
    await browser.close(); await new Promise((resolve) => server.close(resolve));
  }
}

main().catch((error) => { console.error(error); process.exit(1); });
