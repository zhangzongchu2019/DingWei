// Real Chromium forms against the isolated product Hub and real temporary PG.
const {chromium} = require('playwright');
const {createHash} = require('crypto');
const fs = require('fs');
const http = require('http');
const assert = require('assert/strict');
(async () => {
 const [readyPath, mode] = process.argv.slice(2);
 const {origin, resource} = JSON.parse(fs.readFileSync(readyPath, 'utf8'));
 const callbackServer = http.createServer((req,res)=>{res.writeHead(200,{'Content-Type':'text/html'});res.end('<h1>授权完成</h1>');});
 await new Promise(resolve=>callbackServer.listen(0,'127.0.0.1',resolve));
 const browser = await chromium.launch({headless:true,args:['--no-sandbox']});
 try {
  const context = await browser.newContext({ignoreHTTPSErrors:true});
  const page = await context.newPage();
  page.on('console', m=>{if(m.type()==='error') console.error('browser console:',m.text());});
  page.on('requestfailed', r=>console.error('browser request failed:',new URL(r.url()).pathname,r.failure()?.errorText));
  const rejected = [], posts = [];
  page.on('response', async r => {if (r.status()===403) rejected.push(r.url());});
  page.on('request', r => {if(r.method()==='POST' && r.url().startsWith(origin)) posts.push({path:new URL(r.url()).pathname, origin:r.headers().origin});});
  const redirect = 'http://127.0.0.1:'+callbackServer.address().port+'/callback';
  const verifier = 'abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG';
  // Read the public server metadata to use its exact supported scope spelling.
  const metadata = await (await context.request.get(origin+'/.well-known/oauth-authorization-server')).json();
  const scope = metadata.scopes_supported.join(' ');
  const query = new URLSearchParams({response_type:'code',client_id:origin+'/mcp/clients/codex.json',redirect_uri:redirect,resource,scope,state:'synthetic-browser-state',code_challenge_method:'S256',code_challenge:createHash('sha256').update(verifier).digest('base64url')});
  const connect = await page.goto(origin+'/oauth/authorize?'+query);
  await page.waitForURL(origin+'/feishu/connect?**');
  assert.equal(await page.locator('input:not([type=hidden])').count(),0,'ZC_BROWSER_CONNECT_HAS_INPUT');
  const connectPolicy = connect.headers()['referrer-policy'];
  assert.equal(connectPolicy,'same-origin','ZC_BROWSER_CONNECT_REFERRER');
  const connectCSP = connect.headers()['content-security-policy'];
  assert.ok(!connectCSP.includes(new URL(redirect).origin),'ZC_BROWSER_CONNECT_CALLBACK_SOURCE');
  await page.locator('button[type=submit]').click();
  await page.locator('#synthetic-feishu-authorize').waitFor();
  const feishuOrigin = new URL(page.url()).origin;
  assert.ok(connectCSP.includes("form-action 'self' "+feishuOrigin+';'),'ZC_BROWSER_CONNECT_AUTHORIZE_SOURCE');
  await page.locator('#synthetic-feishu-authorize').click();
  if(mode==='multi') {
   await page.locator('input[type=radio][name=target_agent]').first().waitFor();
   assert.equal(await page.locator('input[type=radio][name=target_agent]').count(),2,'ZC_BROWSER_TARGET_OPTIONS');
   await page.locator('input[type=radio][value="other.ho"]').check();
   await page.locator('button[type=submit]').click();
  }
  await page.waitForURL(redirect+'?**');
  const result = new URL(page.url());
  assert.equal(result.searchParams.get('state'),'synthetic-browser-state');
  assert.ok(result.searchParams.get('code'),'ZC_BROWSER_CODE_MISSING');
  assert.deepEqual(rejected,[],'ZC_BROWSER_ORIGIN_REJECTED');
  assert.equal(posts.length,mode==='multi'?2:1);
  for (const post of posts) assert.equal(post.origin,origin,'ZC_BROWSER_POST_ORIGIN');
  const tokenResponse = await context.request.post(origin+'/oauth/token',{form:{grant_type:'authorization_code',code:result.searchParams.get('code'),client_id:origin+'/mcp/clients/codex.json',redirect_uri:redirect,resource,code_verifier:verifier}});
  assert.equal(tokenResponse.status(),200,'ZC_BROWSER_TOKEN_STATUS');
  const token = await tokenResponse.json();
  assert.ok(token.access_token,'ZC_BROWSER_TOKEN_MISSING');
  assert.equal(token.token_type.toLowerCase(),'bearer');
  assert.equal(token.scope.split(' ').length,4);
  console.log(JSON.stringify({mode,issued:true,connectEditableInputs:0,originRejected:rejected.length,posts,realHub:true,realTemporaryPG:true,fakeFeishu:true}));
  await context.close();
 } finally {await browser.close();await new Promise(resolve=>callbackServer.close(resolve));}
})().catch(e=>{console.error(e);process.exit(1);});
