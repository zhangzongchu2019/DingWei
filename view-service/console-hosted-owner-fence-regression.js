// Full production page, synthetic renderer access decision. Real session/API
// authorization is separately exercised through actual wireNewPlatform Go tests.
// No package installation or external traffic. MUTATION=buttons removes only
// the lifecycle UI guard and must fail while the same denied HTML is rendered.
const assert = require('assert');
const http = require('http');
const path = require('path');
const {execFileSync} = require('child_process');
const {chromium} = require('playwright');
if (process.env.MUTATION && process.env.MUTATION !== 'buttons') throw new Error('unknown mutation');
let html = execFileSync('go', ['run', './view-service/console-hosted-page-fixture'], {
  cwd: path.join(__dirname, '..'), encoding: 'utf8',
  env: {...process.env, DW_CONSOLE_HOSTED_FIXTURE:'1', DW_CONSOLE_HOSTED_ACCESS_FIXTURE:'0'},
});
assert(html.includes("var FEATURE_HOSTED='false'==='true';"));
if (process.env.MUTATION === 'buttons') {
  const needle = 'function hostedOpButtons(a,hs){\n  if(!FEATURE_HOSTED)return [];';
  assert.strictEqual(html.split(needle).length, 2);
  html = html.replace(needle, 'function hostedOpButtons(a,hs){');
}
const reads = [];
const server = http.createServer((req,res) => {
  const p = new URL(req.url,'http://fixture').pathname;
  if (p === '/console') {res.writeHead(200,{'content-type':'text/html'}); return res.end(html);}
  reads.push(p);
  const values = {
    '/api/console/platform': {role:'owner',actions:[],items:[{id:'agent-groups',kind:'agent-groups',title:'Agent 编组',href:'#agent-groups',status:'ready'}]},
    '/api/console/agent-groups': {groups:[]},
    // Deliberately return a stale hosted projection: UI must not offer actions
    // even if an earlier cache or other fixture supplies one.
    '/api/console/agents': {agents:[{agent_id:'ag-fixture',name:'fixture',full_name:'fixture.own',origin:'hosted',online:false,state:'active',hosted:{state:'running',upgradable:true,node_name:'fixture'}}]},
    '/api/console/enroll/applications': {applications:[]},
  };
  res.writeHead(values[p] ? 200 : 404, {'content-type':'application/json'});
  res.end(JSON.stringify(values[p] || {error:'not_found'}));
});
(async () => {
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  let browser;
  try {
    browser=await chromium.launch(process.env.PW_CHANNEL ? {headless:true,channel:process.env.PW_CHANNEL} : {headless:true});
    const page=await browser.newPage();
    const pageErrors=[];page.on('pageerror',e=>pageErrors.push(e.message));
    await page.goto(`http://127.0.0.1:${server.address().port}/console`);
    await page.locator('#platform-items a[data-item-id="agent-groups"]').click();
    await page.locator('#rosterItems .roster-row').waitFor();
    assert.strictEqual(await page.locator('#hostedCreate').count(),0);
    assert.strictEqual(await page.locator('#llmAccounts').count(),0);
    assert.strictEqual(await page.locator('#rosterItems .hosted-op').count(),0);
    assert.strictEqual(await page.evaluate(()=>hostedOpButtons({}, {state:'running'}).length),0);
    assert.strictEqual(await page.evaluate(()=>ADMIN_CAPABILITIES.some(c=>c.id==='quota')),false);
    assert.deepStrictEqual(reads.filter(p=>p.startsWith('/api/console/hosted/')||p==='/api/console/llm-accounts'||p==='/api/console/admin/quotas'),[]);
    assert.deepStrictEqual(pageErrors,[]);
    console.log('HOSTED_OWNER_FENCE_BROWSER_OK checks=7 assembled=true allowed=false');
  } finally {
    if(browser)await browser.close();await new Promise(resolve=>server.close(resolve));
  }
})().catch(e=>{console.error(e);process.exitCode=1;});
