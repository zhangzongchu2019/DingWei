const { chromium } = require('playwright');
(async () => {
  const b = await chromium.launch();
  for (const pol of ['no-referrer','same-origin','strict-origin-when-cross-origin']) {
    const p = await b.newPage();
    await p.goto('http://127.0.0.1:18931/?pol=' + pol);
    await Promise.all([p.waitForNavigation(), p.click('#b')]);
    await p.close();
  }
  await b.close();
})();
