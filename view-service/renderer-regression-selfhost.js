const fs = require('fs');
const http = require('http');
const path = require('path');

class EnvironmentUnavailable extends Error {}

function chromiumForRegression() {
  try {
    return require('playwright').chromium;
  } catch (err) {
    throw new EnvironmentUnavailable(`Playwright unavailable: ${err.message}`);
  }
}

async function launchChromium() {
  const chromium = chromiumForRegression();
  try {
    const options = { headless: true };
    if (process.env.PW_CHANNEL) options.channel = process.env.PW_CHANNEL;
    return await chromium.launch(options);
  } catch (err) {
    if (/executable doesn't exist|browser.*not found|failed to launch/i.test(err.message || '')) {
      throw new EnvironmentUnavailable(`Chromium unavailable: ${err.message}`);
    }
    throw err;
  }
}

async function startRendererServer() {
  const rendererPath = path.join(__dirname, 'renderer-canary-sse.html');
  let renderer;
  try {
    renderer = fs.readFileSync(rendererPath);
  } catch (err) {
    throw new EnvironmentUnavailable(`renderer fixture unavailable: ${err.message}`);
  }
  const server = http.createServer((req, res) => {
    const pathname = new URL(req.url, 'http://renderer.test').pathname;
    if (pathname !== '/renderer-canary-sse.html') {
      res.writeHead(404, { 'Content-Type': 'text/plain' });
      res.end('not found');
      return;
    }
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
    res.end(renderer);
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  }).catch(err => { throw new EnvironmentUnavailable(`fixture server unavailable: ${err.message}`); });
  const port = server.address().port;
  return {
    url: `http://127.0.0.1:${port}/renderer-canary-sse.html`,
    close: () => new Promise(resolve => server.close(resolve)),
  };
}

function finishRegression(err) {
  if (err instanceof EnvironmentUnavailable) {
    console.error(`ENVIRONMENT_UNAVAILABLE: ${err.message}`);
    process.exitCode = 3;
    return;
  }
  console.error(err && (err.stack || err.message) || err);
  process.exitCode = 2;
}

module.exports = { finishRegression, launchChromium, startRendererServer };
