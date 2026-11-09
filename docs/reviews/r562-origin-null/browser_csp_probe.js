const { chromium } = require('playwright');
(async () => {
  const b = await chromium.launch();
  for (const q of ['', '?add=1']) {
    const p = await b.newPage();
    const msgs=[]; p.on('console', m => msgs.push(m.text()));
    await p.goto('http://127.0.0.1:18941/' + q);
    await p.click('#b'); await p.waitForTimeout(1500);
    console.log(JSON.stringify({case: q||'self-only', url: p.url(), csp: msgs.filter(t=>/form-action|Refused/.test(t)).slice(0,1)}));
    await p.close();
  }
  await b.close();
})();
