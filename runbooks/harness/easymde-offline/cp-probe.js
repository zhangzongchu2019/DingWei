// 🔵 一次性小探针:把 F2 里那个 ::before content 的**码位**量出来
//    (交付件原先写 "\uf032" 是我按 fa-bold 推断的 —— 推断不能当实测报)
'use strict';
const http = require('http'), fs = require('fs'), path = require('path');
const { chromium } = require('playwright');
// 🔴 前置:字节哈希必须与 pin 一致(唯一源 vendor-pins.js);HERE 用 __dirname ⇒ 装置搬到哪儿都能跑
const { requireVendor, PINS_MAIN, PINS_FA } = require('./vendor-pins');
const HERE = __dirname;
const PROXY = 'http://127.0.0.1:9';
const server = http.createServer((req, res) => {
  const u = req.url.split('?')[0];
  if (u === '/' || u === '/fa') { res.writeHead(200, {'Content-Type':'text/html; charset=utf-8','Cache-Control':'no-store'}); return res.end(fs.readFileSync(path.join(HERE,'fa-page.html'))); }
  if (u.startsWith('/vendor/')) {
    const f = path.join(HERE,'vendor',u.slice(8));
    if (!f.startsWith(path.join(HERE,'vendor')) || !fs.existsSync(f)) { res.writeHead(404); return res.end('nf'); }
    const ct = f.endsWith('.css')?'text/css':f.endsWith('.js')?'text/javascript':f.endsWith('.woff2')?'font/woff2':'application/octet-stream';
    res.writeHead(200,{'Content-Type':ct,'Cache-Control':'no-store'}); return res.end(fs.readFileSync(f));
  }
  res.writeHead(404); res.end('nf');
});
requireVendor(Object.assign({}, PINS_MAIN, PINS_FA), '码位探针');
server.listen(0,'127.0.0.1', async () => {
  const port = server.address().port;
  const browser = await chromium.launch({headless:true,args:['--no-sandbox',`--proxy-server=${PROXY}`]});
  const ctx = await browser.newContext({viewport:{width:1280,height:900},serviceWorkers:'block'});
  const page = await ctx.newPage();
  const ext = [];
  ctx.on('request', r => { if (!r.url().startsWith(`http://127.0.0.1:${port}`)) ext.push(r.url()); });
  await page.goto(`http://127.0.0.1:${port}/fa?cfg=localfa`, {waitUntil:'load',timeout:20000});
  await page.waitForFunction('window.__dwReady === true', null, {timeout:20000});
  await page.evaluate('document.fonts.ready');
  await page.waitForTimeout(1500);
  const out = await page.evaluate(`(function(){
    var names = ['bold','italic','heading','quote','unordered-list','link','image','preview','side-by-side','fullscreen'];
    var res = [];
    names.forEach(function(n){
      var b = document.querySelector('.editor-toolbar button.' + n);
      if (!b) { res.push({btn:n, found:false}); return; }
      var i = b.querySelector('i') || b;
      var c = getComputedStyle(i,'::before').content;
      var codes = [];
      if (c && c !== 'none') { var s = c.replace(/^"|"$/g,''); for (var k=0;k<s.length;k++) codes.push('U+'+s.charCodeAt(k).toString(16).toUpperCase().padStart(4,'0')); }
      var r = i.getBoundingClientRect();
      res.push({btn:n, cls:i.className, content:c==='none'?'none':codes.join(' '), family:getComputedStyle(i,'::before').fontFamily, w:Math.round(r.width), h:Math.round(r.height)});
    });
    return res;
  })()`);
  console.log(JSON.stringify(out, null, 1));
  console.log('external requests =', ext.length, JSON.stringify(ext));
  await browser.close(); await new Promise(r=>server.close(r));
});
