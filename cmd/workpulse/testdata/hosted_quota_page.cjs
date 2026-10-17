// Local browser fixture: every request is intercepted; no real Hub or Agent.
const fs = require('fs');
const path = require('path');
const assert = require('assert/strict');
const { chromium } = require('playwright');
const source = fs.readFileSync(path.join(__dirname, '../hosted_quota_routes.go'), 'utf8');
const html = source.match(/const hostedQuotaPage = `([\s\S]*?)`/)[1];
const hold = 'b'.repeat(32);
const base = {can_release: true, nodes: [{node_id:'nd_fixture', quota_mode:'soft', fresh:true, hold_release_version:1,
  thresholds:{warn_percent:80}, seats:{ag_fixture:{hold:'disk_limit',hold_id:hold,stop_state:'stopped'}}}]};
let checks = 0;
(async () => {
  const browser = await chromium.launch({channel: process.env.PW_CHANNEL || 'chrome', args:['--no-sandbox']});
  try {
    for (const mode of ['owner', 'readonly', 'stale', 'network-failure', 'xss']) {
      const page = await browser.newPage();
      const data = structuredClone(base);
      let posts=0, body;
      if(mode==='readonly')data.can_release=false;
      if(mode==='stale')data.nodes[0].fresh=false;
      if(mode==='xss')data.nodes[0].node_id='<img src=x onerror="window.injected=true">';
      await page.route('**/*', async route => {
        const req=route.request(),url=new URL(req.url());
        if(url.pathname==='/console/hosted-quota')return route.fulfill({contentType:'text/html',body:html});
        if(url.pathname==='/api/console/hosted/quota-monitor')return route.fulfill({contentType:'application/json',body:JSON.stringify(data)});
        if(req.method()==='POST'){
          posts++;body=req.postDataJSON();
          if(mode==='network-failure')return route.abort();
          return route.fulfill({contentType:'application/json',body:JSON.stringify({state:'released_stopped',notice:'hold 已解除，席位保持停止'})});
        }
        return route.abort();
      });
      await page.goto('http://fixture.invalid/console/hosted-quota');
      await page.waitForFunction(()=>document.querySelector('#nodes section'));
      const buttons=page.getByRole('button',{name:'解除 hold，保持停止',exact:true});
      if(mode==='readonly'||mode==='stale'){
        assert.equal(await buttons.count(),0);checks++;
      }else if(mode==='xss'){
        assert.equal(await page.locator('#nodes img').count(),0);
        assert.equal(await page.evaluate(()=>window.injected),undefined);checks+=2;
      }else{
        page.on('dialog',d=>d.accept());
        await buttons.click();
        await page.waitForFunction(()=>document.querySelector('#status').textContent.length>0);
        assert.deepEqual(body,{hold_id:hold,confirm:true});checks++;
        assert.equal(posts,1);checks++;
        assert.equal(await buttons.isDisabled(),true);checks++;
        if(mode==='network-failure'){
          await page.waitForTimeout(300);
          assert.equal(posts,1);checks++;
          assert.match(await page.locator('#status').textContent(),/不自动重发/);checks++;
        }else{assert.match(await page.locator('#status').textContent(),/保持停止/);checks++;}
      }
      await page.close();
    }
    console.log(`HOSTED_QUOTA_PAGE_OK checks=${checks}`);
  }finally{await browser.close();}
})().catch(err=>{console.error(err);process.exitCode=1;});
