// S-R49-R5-BINDING-PAGE-IMPL: production page bytes, local existing-API fixture.
const assertModule = require('assert');
let assertionCount = 0;
const assert = new Proxy(assertModule, {
  apply(target, self, args) { assertionCount++; return Reflect.apply(target, self, args); },
  get(target, key, receiver) { const value = Reflect.get(target, key, receiver); return typeof value === 'function' ? (...args) => { assertionCount++; return value.apply(target, args); } : value; },
});
const fs = require('fs'), path = require('path'), http = require('http');
const {chromium} = require('playwright');
const root = path.join(__dirname, '..'), mutation = process.argv.find(v => v.startsWith('--mutate='))?.slice(9) || '';
let script = fs.readFileSync(path.join(root, 'internal/m8/console_openspec_binding.js'), 'utf8');
let consoleSource = fs.readFileSync(path.join(root, 'internal/m8/console_auth.go'), 'utf8');
function mutate(text, before, after) { assert(text.includes(before), 'BIND_MUTATION_ANCHOR'); const changed = text.replace(before, after); assert.notStrictEqual(changed, text, 'BIND_MUTATION_EFFECTIVE'); return changed; }
if (mutation === 'no-confirm') script = mutate(script, "$('confirmation').hidden = false;", "$('confirmation').hidden = false; setBusy(false); await commit();");
if (mutation === 'hide-old') script = mutate(script, "$('confirmOld').textContent = mappingText(current);", "$('confirmOld').textContent = current === null ? '无映射' : ''; ");
if (mutation === 'leak-entry') consoleSource = mutate(consoleSource, 'if(agentListAdmin){var bindingLink=', 'if(true){var bindingLink=');
assert(['', 'no-confirm', 'hide-old', 'leak-entry'].includes(mutation), 'BIND_KNOWN_MUTATION');
const start = 'const consolePageHTML = `', end = '`\n\nfunc (h *Hub) HandleConsoleAgents';
assert(consoleSource.includes(start) && consoleSource.includes(end), 'BIND_REAL_CONSOLE_SOURCE');
const consoleHTML = consoleSource.slice(consoleSource.indexOf(start)+start.length, consoleSource.indexOf(end, consoleSource.indexOf(start)));
const pageHTML = fs.readFileSync(path.join(root, 'internal/m8/console_openspec_binding.html'), 'utf8').replace('/*BINDING_SCRIPT*/', () => script);
let admin = false, binding = null, errorOverride = '', queryCount = 0;
const writes = [], requests = [], remote = [];
let base;
const server = http.createServer(async (req, res) => {
  const u = new URL(req.url, base || 'http://localhost');
  const reply = (value, code=200) => { res.writeHead(code, {'Content-Type':'application/json'}); res.end(JSON.stringify(value)); };
  if (u.pathname === '/console') { res.setHeader('Content-Type','text/html'); res.end(consoleHTML); return; }
  if (u.pathname === '/console/openspec-bindings') { if (!admin) { reply({error:'platform_admin_required'},403); return; } res.setHeader('Content-Type','text/html');res.end(pageHTML); return; }
  if (u.pathname === '/api/console/platform') { reply({role:'management',agent_list_admin:admin,actions:['read'],items:[{id:'platform-admin',title:'平台管理',href:'#platform-admin'}]}); return; }
  if (u.pathname === '/api/console/agents') { reply({agents:[]});return; }
  if (u.pathname === '/api/console/agent-groups') { reply({groups:[],owned_agents:[]});return; }
  if (u.pathname === '/api/console/platform/users') { reply({users:[]});return; }
  if (u.pathname !== '/api/console/openspec-bindings') { reply({});return; }
  requests.push({method:req.method,path:u.pathname,query:u.search});
  if (!admin) { reply({error:'platform_admin_required'},403);return; }
  if (errorOverride) { reply({error:errorOverride},400);return; }
  if (req.method==='GET') {
    queryCount++;
    assert.deepStrictEqual([...u.searchParams.keys()].sort(), ['instance','principal','provider'], 'BIND_SINGLE_QUERY_FIELDS');
    assert.strictEqual(u.searchParams.get('provider'),'github','BIND_FIXED_PROVIDER');assert.strictEqual(u.searchParams.get('instance'),'github.com','BIND_FIXED_INSTANCE');
    reply(binding && binding.principal===u.searchParams.get('principal') ? binding : null);return;
  }
  let raw='';for await (const part of req) raw+=part;
  const body=JSON.parse(raw);
  const row={method:req.method,body,accepted:false};writes.push(row);
  assert.strictEqual(u.search,'','BIND_MUTATION_NO_QUERY');
  assert.strictEqual(req.headers['content-type'],'application/json','BIND_JSON_HEADER');
  assert.strictEqual(req.headers.origin,base,'BIND_BROWSER_ORIGIN');
  assert.deepStrictEqual(Object.keys(body).sort(),['account_id','instance','login','principal','provider'],'BIND_ONLY_FIVE_FIELDS');
  const valid=body.principal && Buffer.byteLength(body.principal)<=200 && body.provider==='github' && body.instance==='github.com' && /^[1-9][0-9]{0,19}$/.test(body.account_id) && /^[A-Za-z0-9][A-Za-z0-9-]{0,38}$/.test(body.login);
  if(!valid){reply({error:'仓库坐标、路径或文件类型无效'},400);return;}
  assert(['PUT','DELETE'].includes(req.method),'BIND_EXISTING_METHODS');
  row.accepted=true;binding=req.method==='PUT'?body:null;res.writeHead(204);res.end();
});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));base='http://127.0.0.1:'+server.address().port;
  const launch={headless:true};if(process.env.PW_CHANNEL)launch.channel=process.env.PW_CHANNEL;
  const browser=await chromium.launch(launch);
  try{
    const page=await browser.newPage();const pageErrors=[];page.on('pageerror',e=>pageErrors.push(e.message));
    await page.route('**/*',route=>{if(!route.request().url().startsWith(base+'/')){remote.push(route.request().url());return route.abort();}return route.continue();});
    // A management role without platform_admin must not see this entry.
    await page.goto(base+'/console');await page.click('[data-item-id="platform-admin"]');
    assert.strictEqual(await page.locator('#adminMenu').count(),1,'BIND_MANAGEMENT_WORKSPACE_PRESENT');
    assert.strictEqual(await page.locator('#openSpecBindingEntry').count(),0,'BIND_ADMIN_ENTRY_ONLY');
    const denied=await page.goto(base+'/console/openspec-bindings');assert.strictEqual(denied.status(),403,'BIND_NONADMIN_DIRECT_PAGE');
    admin=true;await page.goto(base+'/console');await page.click('[data-item-id="platform-admin"]');
    assert.strictEqual(await page.locator('#openSpecBindingEntry').getAttribute('href'),'/console/openspec-bindings','BIND_ADMIN_ENTRY_TARGET');
    await page.click('#openSpecBindingEntry');await page.waitForSelector('#bindingForm');
    assert.strictEqual(await page.locator('input').count(),3,'BIND_THREE_INPUTS');
    assert.deepStrictEqual(await page.locator('input').evaluateAll(es=>es.map(e=>e.name).sort()),['account_id','login','principal'],'BIND_NO_CREDENTIAL_INPUT');
    const ready=()=>page.waitForFunction(()=>!document.getElementById('save').disabled);
    const fill=async(principal,login,id)=>{await page.fill('#principal',principal);await page.fill('#login',login);await page.fill('#accountID',id);};
    await fill('owner-a','alice','123');await page.click('#save');await ready();
    assert.strictEqual(writes.length,0,'BIND_CONFIRM_BEFORE_PUT');
    assert(await page.locator('#confirmation').isVisible(),'BIND_CONFIRMATION_VISIBLE');
    assert((await page.locator('#confirmTarget').innerText()).includes('owner-a'),'BIND_TARGET_VISIBLE');
    assert.strictEqual(await page.locator('#confirmOld').innerText(),'无映射','BIND_CREATE_EMPTY_OLD');
    assert((await page.locator('#confirmNew').innerText()).includes('alice'),'BIND_NEW_VALUE_VISIBLE');
    await page.click('#cancel');assert.strictEqual(writes.length,0,'BIND_CANCEL_NO_WRITE');
    await page.click('#save');await ready();await page.click('#confirm');await ready();
    assert.strictEqual(writes.length,1,'BIND_CREATE_ONE_PUT');assert.strictEqual(writes[0].method,'PUT','BIND_CREATE_METHOD');assert(writes[0].accepted,'BIND_CREATE_ACCEPTED');
    assert.deepStrictEqual(binding,{principal:'owner-a',provider:'github',instance:'github.com',login:'alice',account_id:'123'},'BIND_CREATE_BODY');
    assert((await page.locator('#current').innerText()).includes('123'),'BIND_READBACK_AFTER_CREATE');
    assert(queryCount>=3,'BIND_CREATION_READBACK_QUERIES');
    await page.reload();await fill('owner-a','','');await page.click('#query');await ready();
    assert((await page.locator('#current').innerText()).includes('alice'),'BIND_QUERY_AFTER_RELOAD');
    assert((await page.locator('#current').innerText()).includes('123'),'BIND_QUERY_ID');
    await page.fill('#login','bob');await page.fill('#accountID','456');await page.click('#save');await ready();
    assert.strictEqual(writes.length,1,'BIND_OVERWRITE_NOT_IMPLICIT');
    const old=await page.locator('#confirmOld').innerText();assert(old.includes('alice')&&old.includes('123'),'BIND_OLD_VISIBLE');
    assert((await page.locator('#confirmNew').innerText()).includes('bob'),'BIND_NEW_OVERWRITE_VISIBLE');
    await page.click('#confirm');await ready();assert.strictEqual(writes.length,2,'BIND_UPSERT_ONE_WRITE');assert.strictEqual(binding.login,'bob','BIND_UPSERT_REPLACES');assert.strictEqual(binding.account_id,'456','BIND_UPSERT_ID');
    await page.click('#save');await ready();await page.fill('#principal','owner-b');
    assert(await page.locator('#confirmation').isHidden(),'BIND_EDIT_INVALIDATES_CONFIRMATION');assert.strictEqual(writes.length,2,'BIND_EDIT_CANNOT_COMMIT_STALE_TARGET');
    const invalid=[['owner-a','bob','01'],['owner-a','bad/name','456'],['','bob','456'],['owner-a','bob','0']];
    for(const values of invalid){await fill(...values);await page.click('#save');await ready();await page.click('#confirm');await ready();assert.strictEqual(await page.locator('#notice').innerText(),'仓库坐标、路径或文件类型无效','BIND_API_INVALID_ERROR_EXACT');assert.strictEqual(binding.account_id,'456','BIND_INVALID_PRESERVES_OLD');}
    await fill('owner-a','ignored','999');await page.click('#remove');await ready();
    const beforeDelete=writes.length;assert((await page.locator('#confirmOld').innerText()).includes('bob'),'BIND_DELETE_OLD_VISIBLE');assert.strictEqual(binding.account_id,'456','BIND_DELETE_NOT_IMPLICIT');
    await page.click('#cancel');assert.strictEqual(writes.length,beforeDelete,'BIND_DELETE_CANCEL_NO_WRITE');
    await page.click('#remove');await ready();await page.click('#confirm');await ready();
    assert.strictEqual(writes.length,beforeDelete+1,'BIND_DELETE_ONE_REQUEST');assert.strictEqual(writes.at(-1).method,'DELETE','BIND_DELETE_METHOD');assert.strictEqual(writes.at(-1).body.account_id,'456','BIND_DELETE_USES_QUERIED_VALUE');assert.strictEqual(binding,null,'BIND_DELETE_REMOVES');assert.strictEqual(await page.locator('#current').innerText(),'无映射','BIND_DELETE_READBACK');
    await page.click('#remove');await ready();assert.strictEqual(writes.length,beforeDelete+1,'BIND_MISSING_NOT_CLAIMED_DELETED');assert(await page.locator('#confirmation').isHidden(),'BIND_MISSING_NO_CONFIRMATION');
    errorOverride='<img src=x onerror=window.pwned=1>';await page.click('#query');await ready();assert.strictEqual(await page.locator('#notice').innerText(),errorOverride,'BIND_ERROR_TEXT_EXACT');assert.strictEqual(await page.locator('#notice img').count(),0,'BIND_ERROR_NOT_HTML');assert.strictEqual(await page.evaluate(()=>!!window.pwned),false,'BIND_ERROR_INERT');errorOverride='';
    admin=false;await page.click('#query');await ready();assert.strictEqual(await page.locator('#notice').innerText(),'platform_admin_required','BIND_FORBIDDEN_ERROR_EXACT');
    assert.strictEqual(remote.length,0,'BIND_NO_EXTERNAL_REQUEST');assert.deepStrictEqual(pageErrors,[],'BIND_NO_PAGE_ERRORS');assert(requests.every(r=>r.path==='/api/console/openspec-bindings'),'BIND_EXISTING_API_ONLY');
    const count=assertionCount, floor=125;assertModule(count>=floor,`BIND_ASSERTION_FLOOR: ${count}<${floor}`);
    console.log(`BIND_BROWSER_OK assertions=${count} floor=${floor} mutation=${mutation||'none'} channel=${process.env.PW_CHANNEL||'headless-shell'}`);
  }finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(error=>{console.error(error.stack);server.close();process.exitCode=1;});
