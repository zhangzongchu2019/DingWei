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
  if (MUTATION === 'list-row-buttons-back') {
    mutateHTML("title.onclick=function(){openSpec(spec.id,true)};",
      "title.onclick=function(){openSpec(spec.id,true)};var legacy=document.createElement('button');legacy.className='view-spec';legacy.textContent='View';legacy.onclick=function(){openSpec(spec.id,true)};row.append(legacy);");
  }
  if (MUTATION === 'readonly-hides-content') {
    mutateHTML("specEditors.edit.codemirror.setOption('readOnly',!canEdit);",
      "specEditors.edit.codemirror.setOption('readOnly',canEdit?false:'nocursor');if(!canEdit)specEditors.edit.value('');");
  }
  if (MUTATION === 'second-edit-entry-back') {
    mutateHTML("showScreen('detail-screen');if(canEdit){",
      "showScreen('detail-screen');if(canEdit){var back=document.createElement('button');back.id='edit-spec-entry';back.textContent='编辑';document.getElementById('detail-screen').append(back);");
  }
  if (MUTATION === 'task-area-outside-detail') {
    mutateHTML('<section id=task-area>', '<section id=task-area data-moved=1 hidden>');
  }
  if (MUTATION === 'no-copy-link-feedback') {
    mutateHTML("statusEl.textContent=ok?'link address copied':'copy failed';", 'statusEl.textContent="";');
  }
  if (MUTATION === 'owner-table-misaligned') mutateHTML('.spec-table th,.spec-table td{padding:8px;', '.spec-table th{padding:8px;}.spec-table td{padding:8px 8px 8px 32px;');
  if (MUTATION === 'owner-empty-extra') mutateHTML('emptyCell.append(empty);', "emptyCell.append(empty,document.createTextNode('暂无 spec'));" );
  if (MUTATION === 'owner-transfer-leak') mutateHTML('if(canEdit){var transfer=', 'if(true){var transfer=');
  if (MUTATION === 'owner-hardcoded-names') mutateHTML("var data=await r.json();select.replaceChildren();", "var data=await r.json();data.editors=[{short_name:'lbl'},{short_name:'th'}];select.replaceChildren();");
  if (MUTATION === 'owner-copy-sticky') mutateHTML("statusEl._copyTimer=setTimeout(function(){statusEl.hidden=true;statusEl.textContent='';},5000);", 'statusEl._copyTimer=0;');
  if (MUTATION === 'owner-transfer-disconnected') mutateHTML("specAction(transferSpec.id,'workflow/transfer'", "specAction(transferSpec.id,'workflow/not-transfer'");
  if (MUTATION === 'owner-delete-action') mutateHTML("var copyLink=document.createElement('button');", "var forbidden=document.createElement('button');forbidden.className='delete-spec';forbidden.textContent='删除';action.append(forbidden);var copyLink=document.createElement('button');");
  if (MUTATION === 'owner-expand-nonexclusive') mutateHTML("document.getElementById('show-new-spec').onclick=function(){showScreen('new-screen');};", "document.getElementById('show-new-spec').onclick=function(){document.getElementById('new-screen').hidden=false;};");
  if (MUTATION === 'owner-empty-restore-all') mutateHTML("document.getElementById('show-spec-list').onclick=function(){showScreen('list-screen');refreshSpecs();};", "document.getElementById('show-spec-list').onclick=function(){showScreen('list-screen');document.getElementById('detail-screen').hidden=false;document.getElementById('transfer-screen').hidden=false;refreshSpecs();};");
  if (MUTATION === 'owner-editor-short-only') mutateHTML("o.textContent=shortName&&shortName!==displayName?shortName+'（'+displayName+'）':displayName;", 'o.textContent=shortName;');
  if (MUTATION === 'owner-title-nonexclusive') mutateHTML("title.onclick=function(){openSpec(spec.id,true)};", "title.onclick=function(){var opened=openSpec(spec.id,true);if(globalThis.__testSpecExclusive)opened.then(function(){document.getElementById('transfer-screen').hidden=false;});};");
  if (MUTATION === 'owner-transfer-nonexclusive') mutateHTML("showScreen('transfer-screen');};action.append(transfer);", "showScreen('transfer-screen');if(globalThis.__testSpecExclusive)document.getElementById('new-screen').hidden=false;};action.append(transfer);");
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
  if (MUTATION === 'readonly-can-write') html = html.replace("document.getElementById('save-spec').hidden=!!readOnly;", "document.getElementById('save-spec').hidden=false;");
  if (MUTATION === 'same-screen-create') html = html.replace('<section id=new-screen hidden>', '<section id=new-screen>');
  if (MUTATION === 'hide-dependents') html = html.replace("+' · 被依赖 '+((task.depended_by||[]).join(', ')||'无')", "");
  if (MUTATION === 'hide-list-on-detail') mutateHTML("title.onclick=function(){openSpec(spec.id,true)};", "title.onclick=async function(){await openSpec(spec.id,true);document.getElementById('list-screen').hidden=true;};");
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
  if (MUTATION && !['list-row-buttons-back', 'readonly-hides-content', 'second-edit-entry-back', 'task-area-outside-detail', 'no-copy-link-feedback', 'no-list-error-text', 'no-editor-focus', 'no-editor-refresh', 'no-render', 'no-wire', 'no-copy', 'no-server-error', 'role-split', 'no-action-error', 'readonly-can-write', 'same-screen-create', 'hide-dependents', 'hide-list-on-detail', 'no-task-dod', 'no-parallel-marker', 'no-hours-hint', 'no-empty-create', 'show-detail-before-click', 'keep-detail-on-page', 'no-task-entry', 'eager-task-form', 'owner-table-misaligned', 'owner-empty-extra', 'owner-transfer-leak', 'owner-hardcoded-names', 'owner-copy-sticky', 'owner-transfer-disconnected', 'owner-delete-action', 'owner-expand-nonexclusive', 'owner-empty-restore-all', 'owner-editor-short-only', 'owner-title-nonexclusive', 'owner-transfer-nonexclusive', ...silentPresplit].includes(MUTATION)) throw new Error(`unknown mutation: ${MUTATION}`);
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
  const transferWrites = [];
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
    if (req.url === '/fixture/add-readonly-spec' && req.method === 'POST') {
      const item={id:'spec-readonly',owner_short_name:'乙',current_editor_short_name:'乙',title:'ReadOnlySpec',
        state:'final',task_state:'closed',current_revision:1,can_edit:false,can_split:false};
      if (!specs.find((x) => x.id === item.id)) { specs.unshift(item);
        revisions.set(item.id,{spec_id:item.id,revision:1,content_markdown:'# 只读内容\n\n正文',acceptance_json:'[]',input_anchors_json:'[]'}); }
      res.writeHead(204); res.end(); return;
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
    if(req.url==='/api/console/spec-editors'){res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify({editors:[{short_name:'zzc',display_name:'张宗楚',value:'张宗楚'},{short_name:'lbl',display_name:'李炳隆',value:'李炳隆'},{short_name:'th',display_name:'唐虹',value:'唐虹'}]}));return;}
    const workflowTransfer=String(req.url||'').match(/^\/api\/console\/specs\/([^/]+)\/workflow\/transfer$/);
    if(workflowTransfer&&req.method==='POST'){transferWrites.push({specID:workflowTransfer[1],body:await requestBody(req)});res.writeHead(204);res.end();return;}
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
    await page.waitForFunction(()=>document.getElementById('page-status').textContent!=='');
    assert.strictEqual(await page.locator('#empty-create-spec').count(),1,'empty list must expose exactly one create-spec link');
    assert.deepStrictEqual(await page.evaluate(() => ({ rows:document.querySelectorAll('#spec-list>tr').length, cells:document.querySelectorAll('#empty-spec-row>td').length, buttons:document.querySelectorAll('#empty-spec-row button').length, text:document.getElementById('empty-spec-row').innerText.trim(), toolbarAddHidden:document.getElementById('show-new-spec').hidden, pagerHidden:document.getElementById('spec-pager').hidden })),
      {rows:1,cells:1,buttons:1,text:'⊕ 添加 spec',toolbarAddHidden:true,pagerHidden:true}, '空列表时 tbody 必须只有第一行 ⊕,不得叠加空态文案或常驻按钮');
    const emptyScreenState=async()=>page.evaluate(()=>({list:!document.getElementById('list-screen').hidden,newSpec:!document.getElementById('new-screen').hidden,detail:!document.getElementById('detail-screen').hidden,transfer:!document.getElementById('transfer-screen').hidden}));
    const emptyBefore=await emptyScreenState();
    await page.click('#empty-create-spec');
    assert.strictEqual(await page.locator('#new-screen').isVisible(), true, 'empty list create link must open a separate create screen');
    assert.deepStrictEqual(await emptyScreenState(),{list:true,newSpec:true,detail:false,transfer:false},'空库打开新建时列表常驻且不可凭空显示详情/转给');
    await page.click('#show-spec-list');
    await page.waitForSelector('#empty-create-spec');
    assert.deepStrictEqual(await emptyScreenState(),emptyBefore,'空库关闭新建后必须逐项恢复点开之前的不对称可见性');
    await page.click('#empty-create-spec');
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
      await page.click('#show-spec-list');
      await page.waitForSelector('#show-new-spec:visible, #empty-create-spec:visible');
      const addEntry = page.locator('#show-new-spec:visible, #empty-create-spec:visible');
      await addEntry.click();
      await page.fill('#new-title', title);
      await page.evaluate((value) => window.specEditors.new.value(value), `# ${title}`);
      await page.fill('#new-acceptance', `${title} accepted`);
      await page.fill('#new-anchor', `msg:${title.slice(-1)}`);
      await page.click('#create-spec');
      await page.locator(`#spec-list [data-title="${title}"]`).waitFor({ state: 'visible' });
    }
    assert.strictEqual(await page.locator('#spec-list [data-spec-id]').count(), 2, 'true page must show both specs at once');
    const columnEdges = await page.evaluate(() => { const textLeft=(node)=>{const range=document.createRange();range.selectNodeContents(node);return range.getBoundingClientRect().left;};return Array.from(document.querySelectorAll('#spec-table th')).map((th, i) => ({ h:textLeft(th), d:textLeft(document.querySelector(`#spec-list tr td:nth-child(${i+1})`)) })); });
    assert.ok(columnEdges.every((edge) => Math.abs(edge.h-edge.d) <= 0.5), `四列表头与内容未对齐: ${JSON.stringify(columnEdges)}`);
    assert.deepStrictEqual(await page.locator('#spec-table thead th').allTextContents(), ['标题','状态','当前','操作']);
    assert.deepStrictEqual(await page.locator('#spec-list tr td:nth-child(2)').allTextContents(), ['修改','修改'], '状态列必须用业主图里的中文状态');
    assert.strictEqual(await page.locator('.delete-spec,.archive-spec').count(),0,'业主已裁定不提供删除或归档入口');
    assert.strictEqual((html.match(/['"](?:lbl|th)['"]/g)||[]).length,0,'前端不得硬编码业主 lbl/th');
    const firstAction=page.locator('#spec-list [data-title="Spec A"] .spec-action');
    await firstAction.locator('.transfer-spec').click();
    await page.locator('#list-transfer-owner option').last().waitFor({state:'attached'});
    assert.deepStrictEqual(await page.locator('#list-transfer-owner option').allTextContents(), ['zzc（张宗楚）','lbl（李炳隆）','th（唐虹）'], '转给候选必须显示接口给出的短名与真名');
    await page.locator('#list-transfer-owner').selectOption({label:'lbl（李炳隆）'});
    await page.locator('input[name="list-transfer-state"][value="review"]').check();
    await page.locator('#confirm-list-transfer').click();
    await page.waitForTimeout(300);
    assert.deepStrictEqual(transferWrites, [{specID:'spec-1',body:{target_short_name:'李炳隆',state:'review'}}], '转审核必须接到真实 workflow/transfer 请求');
    const screenState=async()=>page.evaluate(()=>({list:!document.getElementById('list-screen').hidden,newSpec:!document.getElementById('new-screen').hidden,detail:!document.getElementById('detail-screen').hidden,transfer:!document.getElementById('transfer-screen').hidden}));
    await page.evaluate(()=>{globalThis.__testSpecExclusive=true;});
    try {
      await page.locator('#spec-list [data-title="Spec A"] .spec-title').click();
      await page.locator('#detail-screen').waitFor({state:'visible'});
      assert.deepStrictEqual(await screenState(),{list:true,newSpec:false,detail:true,transfer:false},'点标题后列表常驻且只展开编辑屏');
      await page.locator('#show-new-spec').click();
      assert.deepStrictEqual(await screenState(),{list:true,newSpec:true,detail:false,transfer:false},'点添加后必须收起编辑屏且只展开新建');
      await page.locator('#spec-list [data-title="Spec A"] .transfer-spec').click();
      await page.locator('#transfer-screen').waitFor({state:'visible'});
      assert.deepStrictEqual(await screenState(),{list:true,newSpec:false,detail:false,transfer:true},'点转后必须收起新建且只展开转给');
      await page.locator('#spec-list [data-title="Spec A"] .spec-title').click();
      await page.locator('#detail-screen').waitFor({state:'visible'});
      assert.deepStrictEqual(await screenState(),{list:true,newSpec:false,detail:true,transfer:false},'从转给返回编辑时前一个展开项必须收起');
    } finally {
      await page.evaluate(()=>{globalThis.__testSpecExclusive=false;showScreen('list-screen');});
    }
    for (const title of ['Spec A', 'Spec B']) {
      const row = page.locator(`#spec-list [data-title="${title}"]`);
      // S-6 改格留痕:原断言是「自己的 spec 必须看到 Edit 按钮」;业主裁定去掉列表行按钮、标题即入口,
      //   判据随之改为「标题可点 ＋ 提示表明它可编辑」。能不能编辑仍然有断言,只是落点从按钮挪到标题。
      assert.strictEqual(await row.locator('.spec-title').count(), 1, 'management owner must see a clickable title for own spec');
      assert.strictEqual(await row.locator('.spec-title').getAttribute('title'), '点击打开(可编辑)', '可编辑的 spec,标题提示必须表明可编辑');
    }
    // ㊴ S-6 ④ 的【独立用例】:从标题进入后,拆解/预拆/工作流必须可达 ——————————————
    // 🔴 为什么单独开一条:上一段里这条判据虽然写了,但变异 task-area-outside-detail
    //    先被更早的 openPresplitMode 接住 ⇒ 「红了」证明不了「被这条判据红的」。
    //    本条**自带干净页面**、只依赖已建好的 Spec A、且**排在 presplit 之前**,
    //    因此该变异会直接红在这里,这条判据才算有自己的阳性对照。
    // 🔵 用新页面而不是改执行顺序:不动其它用例的前置状态(组长裁定)。
    {
      const reachPage = await context.newPage();
      reachPage.setDefaultTimeout(2500);
      await reachPage.goto(`http://127.0.0.1:${port}/console/specs`, { waitUntil: 'domcontentloaded' });
      await reachPage.waitForSelector('#spec-list [data-title="Spec A"] .spec-title');
      await reachPage.locator('#spec-list [data-title="Spec A"] .spec-title').click();
      await reachPage.waitForSelector('#detail-screen:not([hidden])');
      const reach = await reachPage.evaluate(() => {
        const vis = (id) => { const el = document.getElementById(id); return !!(el && el.offsetParent); };
        const inDetail = (id) => { const el = document.getElementById(id); return !!(el && el.closest('#detail-screen')); };
        return {
          taskArea: vis('task-area'), taskAreaInDetail: inDetail('task-area'),
          workflow: vis('spec-workflow'), workflowInDetail: inDetail('spec-workflow'),
          presplitExists: !!document.getElementById('agent-presplit'),
          presplitInDetail: inDetail('agent-presplit'),
          rendered: (document.getElementById('spec-rendered') || {}).childElementCount,
        };
      });
      assert.ok(reach.taskArea && reach.taskAreaInDetail,
        `从标题进来之后「任务拆解」不可达(可见=${reach.taskArea} 在详情屏内=${reach.taskAreaInDetail}) ⇒ 主链路从入口上就断了`);
      assert.ok(reach.workflow && reach.workflowInDetail,
        `从标题进来之后「转修改/转审核/审结」不可达(可见=${reach.workflow} 在详情屏内=${reach.workflowInDetail})`);
      assert.ok(reach.presplitExists && reach.presplitInDetail,
        'Agent 预拆区必须在详情屏内(它是否可用另说,但必须够得着)');
      assert.ok(reach.rendered > 0, '详情屏必须把 spec 内容渲染出来');
      console.log(`S-6④ 独立用例: 拆解=${reach.taskArea} 工作流=${reach.workflow} 预拆在详情内=${reach.presplitInDetail}`);
      await reachPage.close();
    }
    async function openPresplitMode(mode) {
      await page.request.post(`http://127.0.0.1:${port}/fixture/presplit-mode?mode=${mode}`);
      await page.click('#show-spec-list');
      await page.locator('#spec-list [data-title="Spec B"] .spec-title').click();
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
    await page.locator('#spec-list [data-title="Spec A"] .spec-title').click();await page.locator('#detail-screen').waitFor({state:'visible'});assert.notStrictEqual((await page.locator('#spec-rendered').textContent()).trim(),'');
    await page.click('#show-new-spec');
    async function keyboardReplace(selector,value){await page.locator(selector).click();await page.keyboard.press(process.platform==='darwin'?'Meta+A':'Control+A');await page.keyboard.type(value);}
    await keyboardReplace('#new-title','Created after detail');await page.locator('#new-screen .CodeMirror').click();await page.keyboard.press(process.platform==='darwin'?'Meta+A':'Control+A');await page.keyboard.type('# Created after detail');await keyboardReplace('#new-acceptance','created accepted');await keyboardReplace('#new-anchor','msg:created');await page.click('#create-spec');
    await page.locator('#spec-list [data-title="Created after detail"]').waitFor({state:'visible'});assert.strictEqual(await page.locator('#detail-screen').isHidden(),true,'creation list refresh must hide prior detail');assert.strictEqual((await page.locator('#spec-rendered').textContent()).trim(),'','creation list refresh must clear prior detail DOM');assert.strictEqual((await page.locator('#task-list').textContent()).trim(),'','creation list refresh must clear prior task DOM');
    await page.request.post(`http://127.0.0.1:${port}/fixture/populate-pages`);await page.click('#show-spec-list');await page.waitForFunction(()=>!document.getElementById('next-page').disabled);
    await page.locator('#spec-list [data-title="Spec A"] .spec-title').click();await page.locator('#detail-screen').waitFor({state:'visible'});await page.click('#next-page');
    assert.strictEqual(await page.locator('#detail-screen').isHidden(),true,'pagination must clear selected spec detail');assert.strictEqual(await page.locator('#task-area').isHidden(),true,'pagination must clear selected task area');assert.strictEqual(await page.locator('#task-list').textContent(),'','pagination must clear rendered tasks');
    await page.waitForFunction(()=>document.getElementById('page-status').textContent==='2 / 2');await page.click('#prev-page');await page.locator('#spec-list [data-title="Spec A"] .spec-title').waitFor({state:'visible'});await page.locator('#spec-list [data-title="Spec A"] .spec-title').click();
    await page.locator('#detail-screen').waitFor({state:'visible'});
    assert.strictEqual(await page.locator('#list-screen').isVisible(), true, 'opening detail must keep the spec list visible above it');
    assert.strictEqual(await page.evaluate(() => Boolean(document.getElementById('list-screen').compareDocumentPosition(document.getElementById('detail-screen')) & Node.DOCUMENT_POSITION_FOLLOWING)), true, 'detail must render after the resident list');
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
      // S-6 改格留痕(第二次):甲案是【标题→详情→点「编辑」】;乙案(业主改判)是**同一套界面**,
      //   点标题即到文本框,**没有第二个入口**。判据(能不能打开这份 spec 的内容)没变,路径又少一步。
      await page.locator(`#spec-list [data-title="${title}"] .spec-title`).click();
      await page.waitForSelector('#detail-screen:not([hidden])');
      await page.waitForFunction((want) => document.getElementById('edit-title').value === want, title);
      assert.strictEqual(await page.inputValue('#edit-title'), title, `edit form must open ${title}`);
      // 乙案:编辑屏与详情屏合并 ⇒ 原 #edit-rendered 这块重复的预览已删,判据落到文本框内容上
      await page.waitForFunction((want) => window.specEditors.edit.value().includes(want), title);
      const changed = title === 'Spec A'
        ? `# ${title} changed\n\n\`\`\`python\nprint("copy-me")\n\`\`\`\n\n![good](http://127.0.0.1:${port}/f/spec/good/26/0921/0123456789abcdef0123456789abcdef.png)\n\n![missing](http://127.0.0.1:${port}/f/spec/missing/26/0921/ffffffffffffffffffffffffffffffff.png)`
        : `# ${title} changed`;
      await page.evaluate((value) => window.specEditors.edit.value(value), changed);
      await page.click('#save-spec');
      await page.locator('#edit-status').filter({ hasText: 'saved revision 2' }).waitFor({ state: 'visible' });
      if (title === 'Spec A') {
        await page.locator('#spec-rendered h1').filter({ hasText: 'Spec A changed' }).waitFor({ state: 'visible' });
        await page.locator('#spec-rendered img.md-img[alt="good"]').waitFor({ state: 'visible' });
        await page.locator('#spec-rendered .md-image-fallback').waitFor({ state: 'visible' });
        assert.strictEqual(await page.locator('#spec-rendered img.md-img').count(), 1, 'working image must remain inline');
        assert.strictEqual(await page.locator('#spec-rendered .md-image-fallback').getAttribute('href'), `http://127.0.0.1:${port}/f/spec/missing/26/0921/ffffffffffffffffffffffffffffffff.png`, 'failed image must retain a direct download exit');
        assert.strictEqual(await page.locator('#spec-rendered img.md-img').evaluate((el) => getComputedStyle(el).maxHeight), '420px', 'shared renderer CSS contract must be present');
        await page.locator('#spec-rendered .copy-code').click();
        assert.strictEqual(await page.evaluate(() => globalThis.__copiedCode), 'print("copy-me")', 'Copy button must be wired to the rendered code');
        const timeline = await context.newPage();
        await timeline.goto(`http://127.0.0.1:${port}/view/parity-session`, { waitUntil: 'domcontentloaded' });
        const timelineRendered = await timeline.evaluate((markdown) => renderMarkdown(markdown), changed);
        const specCoreRendered = await page.evaluate((markdown) => DwMdCore.renderMarkdown(markdown), changed);
        assert.strictEqual(specCoreRendered, timelineRendered, 'spec and timeline must render identical bytes at the real spec origin');
        assert.strictEqual(await page.locator('#spec-rendered > .md').count(), 1, 'the spec page must mount the shared renderer output into its visible DOM');
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
      await page.click('#show-spec-list');
      await page.click('#show-new-spec');
      await setNew({ title: `Editor-${editorCase.label}`, markdown: editorCase.markdown, acceptance: 'usable', anchor: 'msg:editor' });
      await page.click('#create-spec');
      await page.waitForSelector('#list-screen:not([hidden])');
      for (let i = 0; i < 40 && !caseSpecID; i++) await page.waitForTimeout(100);
      page.off('response', grabCase);
      assert.ok(caseSpecID, `未能取到新建 spec 的 id(${editorCase.label})`);
      await page.evaluate((id) => window.openSpec(id, false), caseSpecID);
      await page.waitForSelector('#detail-screen:not([hidden])');   // 乙案:编辑屏已并入详情屏
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
    // ㊳ S-6 乙案:查看与编辑**同一套界面** —————————————————————————————————
    // 🔴 业主 2026-09-24:「查看和编辑应该是同一套界面……无论是编辑还是查看,都是文本框打开展示,
    //    只是只读不让编辑而已」。⇒ 五条判据:
    //    ① 打开任一 spec ⇒ **文本框直接展示内容**,不存在先看后编两步
    //    ② 不可编辑 ⇒ 文本框只读,🔴 **但内容可见、可滚动、可选中复制**(只读 ≠ 不让看)
    //    ③ 可编辑 ⇒ **同一个文本框**可改可存,且**不得出现第二个「编辑」入口**
    //    ④ 拆解/预拆/工作流**同屏可达**(另有独立用例 ㊴ 专门盯,这里只做同屏断言)
    //    ⑤ 列表行仍只有标题可点(S-6 甲已交付,乙案继承)
    await page.click('#show-spec-list');
    await page.waitForSelector('.spec-row .spec-title');
    const rowShape = await page.evaluate(() => ({
      titles: document.querySelectorAll('.spec-row .spec-title').length,
      legacyButtons: document.querySelectorAll('.spec-row .view-spec, .spec-row .edit-spec').length,
    }));
    assert.ok(rowShape.titles > 0, '列表行必须有可点标题');
    assert.strictEqual(rowShape.legacyButtons, 0, `列表行上不得再出现 View／Edit 并列,实测 ${rowShape.legacyButtons} 个`);
    // ①③ 可编辑:一次点击就到文本框,且没有第二个入口
    const editableID = await page.evaluate(() => (document.querySelector('.spec-row') || {}).dataset.specId);
    await page.locator('.spec-row .spec-title').first().click();
    await page.waitForSelector('#detail-screen:not([hidden])');
    const editableShape = await page.evaluate(() => {
      const cm = window.specEditors.edit.codemirror;
      const vis = (id) => { const el = document.getElementById(id); return !!(el && el.offsetParent); };
      return {
        canEdit: window.currentSpec ? window.currentSpec.can_edit : null,
        editorVisible: !!cm.getWrapperElement().offsetParent,
        valueLen: cm.getValue().length,
        readOnly: !!cm.getOption('readOnly'),
        saveHidden: (document.getElementById('save-spec') || {}).hidden,
        secondEntry: document.querySelectorAll('#edit-spec-entry').length,
        editorScreenGone: !document.getElementById('editor'),
        taskArea: vis('task-area'), workflow: vis('spec-workflow'),
      };
    });
    assert.strictEqual(editableShape.canEdit, true, 'S-6 前置:第一行这份 spec 应当可编辑');
    assert.ok(editableShape.editorVisible && editableShape.valueLen > 0,
      `一次点击之后文本框必须直接展示内容(可见=${editableShape.editorVisible} 长度=${editableShape.valueLen})`);
    assert.strictEqual(editableShape.readOnly, false, '可编辑的 spec,文本框不得是只读');
    assert.strictEqual(editableShape.saveHidden, false, '可编辑的 spec 必须能存');
    assert.strictEqual(editableShape.secondEntry, 0, '不得出现第二个「编辑」入口 —— 查看与编辑是同一套界面');
    assert.ok(editableShape.taskArea && editableShape.workflow, '拆解与工作流必须与文本框同屏可达');
    // ② 不可编辑:只读,但内容必须看得见、滚得动、选得中
    await page.request.post(`http://127.0.0.1:${port}/fixture/add-readonly-spec`);
    await page.click('#show-spec-list');
    await page.waitForFunction(() => /ReadOnlySpec/.test(document.getElementById('spec-list').innerText), null, { timeout: 5000 });
    assert.strictEqual(await page.locator('#spec-list [data-title="ReadOnlySpec"] .transfer-spec').count(),0,'「转」不得出现在当前不是我的行');
    assert.strictEqual(await page.locator('#spec-list [data-title="Spec A"] .transfer-spec').count(),1,'「转」必须保留在当前是我的行');
    await page.locator('#spec-list [data-title="ReadOnlySpec"] .spec-title').click();
    await page.waitForSelector('#detail-screen:not([hidden])');
    const readonlyShape = await page.evaluate(() => {
      const cm = window.specEditors.edit.codemirror;
      cm.setSelection({ line: 0, ch: 0 }, { line: 0, ch: 3 });
      return {
        readOnly: !!cm.getOption('readOnly'),
        nocursor: cm.getOption('readOnly') === 'nocursor',
        visible: !!cm.getWrapperElement().offsetParent,
        valueLen: cm.getValue().length,
        rendered: cm.getWrapperElement().querySelectorAll('.CodeMirror-line').length,
        selected: cm.getSelection().length,
        saveHidden: (document.getElementById('save-spec') || {}).hidden,
        secondEntry: document.querySelectorAll('#edit-spec-entry').length,
      };
    });
    assert.strictEqual(readonlyShape.readOnly, true, '不可编辑的 spec,文本框必须只读');
    assert.strictEqual(readonlyShape.nocursor, false, "只读不得用 'nocursor' —— 那会连选中复制都做不了");
    assert.ok(readonlyShape.visible && readonlyShape.valueLen > 0 && readonlyShape.rendered > 0,
      `只读 ≠ 不让看:内容必须可见(可见=${readonlyShape.visible} 长度=${readonlyShape.valueLen} 渲染行=${readonlyShape.rendered})`);
    assert.ok(readonlyShape.selected > 0, '只读态必须能选中复制');
    assert.strictEqual(readonlyShape.saveHidden, true, '不可编辑时不得给出保存入口');
    assert.strictEqual(readonlyShape.secondEntry, 0, '只读态同样不得出现「编辑」入口');
    console.log(`S-6乙: 可编辑(只读=${editableShape.readOnly} 存=${!editableShape.saveHidden} 同屏=${editableShape.taskArea && editableShape.workflow}) 只读(只读=${readonlyShape.readOnly} 可见行=${readonlyShape.rendered} 选中=${readonlyShape.selected})`);
    // ㊲ S-7:「复制只读链接」—— 复制到的**文本内容**必须对,且成功必须有**可见反馈** ——————
    // 🔴 业主 2026-09-24 裁定「当前控写不控读」⇒ 给的是**只读**链接。
    //    它对【已存在】的 spec 同样有效(`GET /console/specs/{id}` 本就公开可读),
    //    不依赖创建时那枚只出现一次的 edit 令牌。
    // 🔴 判据落在**被写入的文本**上,不落在 clipboard 权限上 —— 无头环境里权限判据容易假绿。
    await page.click('#show-spec-list');
    await page.waitForSelector('.spec-row .copy-spec-link');
    await page.evaluate(() => { globalThis.__copiedCode = ''; });
    const firstRowSpecID = await page.evaluate(() => (document.querySelector('.spec-row') || {}).dataset.specId);
    assert.ok(firstRowSpecID, 'S-7 前置:列表行必须带 data-spec-id');
    await page.locator('.spec-row .copy-spec-link').first().click();
    await page.waitForTimeout(400);
    const copyState = await page.evaluate(() => ({
      copied: globalThis.__copiedCode || '',
      rowStatus: (document.querySelector('.spec-row .spec-action-status') || {}).textContent || '',
      origin: location.origin,
    }));
    assert.strictEqual(copyState.copied, `${copyState.origin}/console/specs/${firstRowSpecID}`,
      `复制到剪贴板的文本不是该 spec 的只读链接,实测 ${JSON.stringify(copyState.copied)}`);
    assert.strictEqual(copyState.rowStatus, 'link address copied',
      `复制之后列表行没有可见反馈(实测 ${JSON.stringify(copyState.rowStatus)}) ⇒ 业主又是"点了没反应"`);
    await page.waitForTimeout(5200);
    assert.strictEqual(await page.locator('.spec-row .spec-action-status').first().isHidden(), true,
      '列表行复制冒泡必须在 5 秒后消失');
    // 详情页同样要有一个,且复制的是当前这份 spec
    await page.evaluate(() => { globalThis.__copiedCode = ''; });
    await page.evaluate((id) => window.openSpec(id, true), firstRowSpecID);
    await page.waitForSelector('#copy-spec-link');
    await page.click('#copy-spec-link');
    await page.waitForTimeout(400);
    const detailCopy = await page.evaluate(() => ({
      copied: globalThis.__copiedCode || '',
      status: (document.getElementById('copy-link-status') || {}).textContent || '',
    }));
    assert.strictEqual(detailCopy.copied, `${copyState.origin}/console/specs/${firstRowSpecID}`,
      `详情页复制到的不是当前 spec 的只读链接,实测 ${JSON.stringify(detailCopy.copied)}`);
    assert.strictEqual(detailCopy.status, 'link address copied',
      `详情页复制之后没有可见反馈(实测 ${JSON.stringify(detailCopy.status)})`);
    await page.waitForTimeout(5200);
    assert.strictEqual(await page.locator('#copy-link-status').isHidden(), true, '详情复制冒泡必须在 5 秒后消失');
    console.log(`S-7: 列表复制=${copyState.copied} 反馈=${JSON.stringify(copyState.rowStatus)} 详情反馈=${JSON.stringify(detailCopy.status)}`);
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
