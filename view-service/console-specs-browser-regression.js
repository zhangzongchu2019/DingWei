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
  if (MUTATION && !['no-render', 'no-wire', 'no-copy', 'no-server-error', 'role-split', 'no-action-error', 'no-view', 'readonly-can-write', 'same-screen-create', 'hide-dependents', 'hide-list-on-detail', 'no-task-dod', 'no-parallel-marker', 'no-hours-hint'].includes(MUTATION)) throw new Error(`unknown mutation: ${MUTATION}`);
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
  const server = http.createServer(async (req, res) => {
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
    if (String(req.url).startsWith('/api/console/specs?') && req.method === 'GET') {
      const page = Number(new URL(req.url, 'http://fixture').searchParams.get('page') || 1); const start=(page-1)*20;
      const own=specs.filter((item)=>item.principal_id==='owner-a');
      res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ role: 'management', principal: 'owner-a', specs:own.slice(start,start+20), page, page_size:20, total:own.length, total_pages:Math.max(1,Math.ceil(own.length/20)) })); return;
    }
    if (req.url === '/api/console/specs' && req.method === 'POST') {
      const body = await requestBody(req);
      if (body.title === 'server-reject') {
        res.writeHead(400, { 'Content-Type': 'text/plain; charset=utf-8' }); res.end('server create detail'); return;
      }
      const item = { id: `spec-${specs.length + 1}`, principal_id: 'owner-a', title: body.title, state: 'draft', current_revision: 1 };
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
      res.end(JSON.stringify({ spec: item, revision: revisions.get(item.id), tasks:item.id==='spec-1'?ordered:[], dependency_candidates:[taskA,taskB] })); return;
    }
    const planMatch=String(req.url||'').match(/^\/api\/console\/task-proposals\/([^/]+)\/plan$/);
    if(planMatch&&req.method==='PATCH'){const body=await requestBody(req);const id=planMatch[1];taskPlans.push({id,body});if(body.depends_on.includes(id)){res.writeHead(409);res.end('task cannot depend on itself');return;}const next=new Map(taskDeps);next.set(id,body.depends_on);if((next.get('task-a')||[]).includes('task-b')&&(next.get('task-b')||[]).includes('task-a')){res.writeHead(409);res.end('task dependency cycle');return;}taskDeps.set(id,body.depends_on);taskDoDs.set(id,body.definition_of_done||'');res.writeHead(204);res.end();return;}
    const splitMatch=String(req.url||'').match(/^\/api\/console\/specs\/([^/]+)\/tasks$/);
    if(splitMatch&&req.method==='POST'){taskCreates.push({specID:splitMatch[1],body:await requestBody(req)});res.writeHead(201,{'Content-Type':'application/json'});res.end(JSON.stringify({proposal_id:'task-new'}));return;}
    if (match && req.method === 'PATCH') {
      const body = await requestBody(req); const item = specs.find((candidate) => candidate.id === match[1]);
      if (body.content_markdown === 'server-save-reject') {
        res.writeHead(409, { 'Content-Type': 'text/plain; charset=utf-8' }); res.end('server save detail'); return;
      }
      item.current_revision += 1; item.state = 'needs_improvement';
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
    const pageErrors = [];
    page.on('pageerror', (error) => pageErrors.push(error.message));
    await page.goto(`http://127.0.0.1:${port}/console/specs`, { waitUntil: 'domcontentloaded' });
    assert.strictEqual(await page.locator('#list-screen').isVisible(), true, 'entry must show my spec list first');
    assert.strictEqual(await page.locator('#new-screen').isHidden(), true, 'new form must not share the first screen');
    await page.click('#show-new-spec');
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
      assert.strictEqual(await row.getByRole('button', { name: 'Submit', exact: true }).count(), 1, 'management owner must see Submit for own draft spec');
    }
    await page.locator('#spec-list [data-title="Spec A"] .view-spec').click();
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
    await page.fill('#task-title','Implement new');await page.fill('#task-content','new content');await page.fill('#task-dod','New done');await page.fill('#task-hours','16');
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
        await page.locator('#edit-rendered img.md-img').waitFor({ state: 'visible' });
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
    specs.push({ id: 'spec-foreign', principal_id: 'owner-b', title: 'Foreign', state: 'draft', current_revision: 1 });
    revisions.set('spec-foreign', { spec_id: 'spec-foreign', revision: 1, content_markdown: '# Foreign', acceptance_json: '[{"id":"A1","text":"read"}]', input_anchors_json: '[{"kind":"owner_message","ref":"msg:foreign"}]' });
    specs.push({ id: 'spec-closed', principal_id: 'owner-a', title: 'Closed', state: 'draft', current_revision: 1, editing_closed: true });
    revisions.set('spec-closed', { spec_id: 'spec-closed', revision: 1, content_markdown: '# Closed', acceptance_json: '[{"id":"A1","text":"read"}]', input_anchors_json: '[{"kind":"owner_message","ref":"msg:closed"}]' });
    await page.evaluate(() => refreshSpecs()); await page.click('#show-spec-list');
    assert.strictEqual(await page.locator('#spec-list [data-title="Foreign"]').count(), 0, '我的 spec list must exclude another owner spec');
    await page.evaluate(() => openSpec('spec-foreign',true));
    await page.locator('#detail-screen #spec-rendered h1').filter({hasText:'Foreign'}).waitFor({state:'visible'});
    assert.strictEqual(await page.locator('#detail-screen').isVisible(), true, 'public read must open a separate detail screen');
    assert.strictEqual(await page.locator('#save-spec').isHidden(), true, 'detail screen must not expose Save');
    await page.click('#show-spec-list');
    const closed = page.locator('#spec-list [data-title="Closed"]');
    assert.strictEqual(await closed.locator('.edit-spec').count(), 0, 'editing-closed own spec must not expose Edit');
    await closed.locator('.view-spec').click();
    await page.locator('#detail-screen #spec-rendered h1').filter({hasText:'Closed'}).waitFor({state:'visible'});
    assert.strictEqual(await page.locator('#detail-screen').isVisible(), true, 'editing-closed own spec must remain readable in detail screen');
    await page.click('#show-spec-list');
    await closed.locator('.reopen-spec').click();
    await page.locator('#spec-list [data-title="Closed"] .edit-spec').waitFor({ state: 'visible' });
    assert.strictEqual(specs.find((item) => item.id === 'spec-closed').editing_closed, false, 'owner Reopen editing must change the server-side state');
    const own = page.locator('#spec-list [data-title="Spec A"]');
    await own.getByRole('button', { name: 'Archive', exact: true }).click();
    await own.locator('.spec-action-status').filter({ hasText: '操作失败 409:already archived' }).waitFor({ state: 'visible' });
    assert.deepStrictEqual(pageErrors, [], `failed specAction must be handled on-page: ${JSON.stringify(pageErrors)}`);
    await page.reload({ waitUntil: 'domcontentloaded' });
    assert.strictEqual(await page.locator('#spec-list [data-spec-id]').count(), 3, 'reload must retain all owned specs and exclude foreign specs');
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
    assert.deepStrictEqual(pageErrors, [], `spec page emitted top-level errors: ${JSON.stringify(pageErrors)}`);
    console.log(`editor wrap metrics overflow=${wrapMetrics.overflow} lineOverhang=${wrapMetrics.lineOverhang}`);
    console.log('console specs browser: passed=27 failed=0');
  } finally {
    await browser.close(); await new Promise((resolve) => server.close(resolve));
  }
}

main().catch((error) => { console.error(error); process.exit(1); });
