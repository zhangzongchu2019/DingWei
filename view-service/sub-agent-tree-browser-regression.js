// Actual Console source, isolated HTTP dependencies. No online seat or inference.
const assert=require('assert');
const fs=require('fs');
const path=require('path');
const http=require('http');
const {chromium}=require('playwright');
const source=fs.readFileSync(path.join(__dirname,'../internal/m8/console_auth.go'),'utf8');
const start=source.indexOf('const consolePageHTML = `')+'const consolePageHTML = `'.length;
const end=source.indexOf('`\n\nfunc (h *Hub) HandleConsoleAgents',start);
assert(end>start);
let html=source.slice(start,end).replaceAll('{{VERSION}}','test').replaceAll('{{COMMIT}}','test').replaceAll('{{FEATURE_NEWPLAT_NAME_CHECK}}','true').replaceAll('{{FEATURE_TEMP_GROUPS}}','false').replace('const SUB_AGENT_TREE_ENABLED = false;','const SUB_AGENT_TREE_ENABLED = true;');
const mutation=process.argv.includes('--mutate-tree');
if(mutation) html=html.replace("b.textContent=(root.name||root.full_name)+' → '+n.full_name.split('.')[0]+' · '+(n.state==='ended'?'ended':n.online?'在线':'离线')+(n.reason?' · '+n.reason:'');","b.textContent=n.full_name;");
const root={agent_id:'root',full_name:'agent.owner',name:'agent',online:true,state:'active'};
let revoked=false,treeRequests=0;
const trees=()=>({version:1,trees:revoked?[]:[{full_name:root.full_name,nodes:[{node_id:'a',generation:1,full_name:'aa.agent.owner',state:'active',reason:'',online:true},{node_id:'b',generation:1,full_name:'bb.agent.owner',state:'ended',reason:'completed',online:false}]}]});
const server=http.createServer((req,res)=>{
 res.setHeader('Cache-Control','no-store');
 if(req.url.startsWith('/console')){res.setHeader('Content-Type','text/html');res.end(req.url.includes("legacy")?html.replace("const SUB_AGENT_TREE_ENABLED = true;","const SUB_AGENT_TREE_ENABLED = false;"):html);return;}
 res.setHeader('Content-Type','application/json');
 if(req.url==='/api/console/agents'){res.end(JSON.stringify({agents:[root]}));return;}
 if(req.url==='/api/console/agent-trees'){treeRequests++;res.end(JSON.stringify(trees()));return;}
 if(req.url==='/api/console/agent-groups'){res.end(JSON.stringify({owned_agents:[],groups:[]}));return;}
 if(req.url==='/api/console/platform'){res.end(JSON.stringify({items:[]}));return;}
 if(req.url.startsWith('/view/')){res.setHeader('Content-Type','text/html');res.end('<p>isolated view target</p>');return;}
 res.end('{}');
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));let browser;
 try {
  browser=await chromium.launch({headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
  const page=await browser.newPage();await page.goto('http://127.0.0.1:'+server.address().port+'/console');
  await page.waitForSelector('.sub-agent-node');
  const labels=await page.locator('.sub-agent-node').allTextContents();
  assert(labels.some(x=>x.includes('agent → aa')&&x.includes('在线')),'AA010_BROWSER_TREE_ONLINE');
  assert(labels.some(x=>x.includes('agent → bb')&&x.includes('ended')&&x.includes('completed')),'AA010_BROWSER_TREE_ENDED_REASON');
  assert(await page.locator('.sub-agent-node[data-node-id="b"]').isDisabled(),'AA010_BROWSER_ENDED_DISABLED');
  await page.locator('.sub-agent-node[data-node-id="a"]').click();
  assert(await page.locator('iframe[src*="aa.agent.owner"]').count()===1,'AA010_BROWSER_CHILD_CANONICAL_TARGET');
  revoked=true;await page.evaluate(()=>refreshSubAgentTrees());
  assert(await page.locator('.sub-agent-node').count()===0,'AA010_BROWSER_REVOKE_TREE');
  assert(await page.locator('iframe[src*="aa.agent.owner"]').evaluate(el=>el.style.pointerEvents)==='none','AA010_BROWSER_REVOKE_PANE');
  const before=treeRequests;
  await page.goto('http://127.0.0.1:'+server.address().port+'/console?legacy');
  await page.waitForFunction(()=>agentListSnapshot.length===1);
  assert(await page.locator('.sub-agent-node').count()===0&&treeRequests===before,'AA010_NEW_VIEW_OLD_HUB_ROOT_ONLY');
  console.log(JSON.stringify({passed:6,failed:0,source:'actual Console document; mock HTTP only'}));
 } finally {if(browser)await browser.close();await new Promise(r=>server.close(r));}
})().catch(e=>{console.error(e);process.exitCode=1});
