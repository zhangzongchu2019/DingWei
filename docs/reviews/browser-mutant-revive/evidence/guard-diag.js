const fs = require('fs');
const path = require('path');
const assert = require('assert');
const ROOT = '/tmp/wt-mutant-revive';
const src = fs.readFileSync(path.join(ROOT, 'view-service/console-browser-regression.js'), 'utf8');
const start = src.indexOf('function consoleHTMLFromProductionSource()');
const end = src.indexOf('\nfunction listen(');
assert.ok(start > 0 && end > start, 'function markers');
const fnText = src.slice(start, end).replace('function consoleHTMLFromProductionSource()', 'function()');
const GO_SOURCE = path.join(ROOT, 'internal/m8/console_auth.go');
const START = 'const consolePageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleAgents';
const HARNESS_VERSION = '7.7.7.7-harness';
const HARNESS_COMMIT = 'c0ffee1234567890';
function build(MUTATION, text) {
  const fn = eval('(' + (text || fnText) + ')');
  return fn();
}
const anchor = "groupFeedback(feedback||'group-status','请求失败: '+String(e&&e.message||e));return false;";
const base = build('');
assert.ok(base.includes(anchor), 'anchor present in unmutated html');
const mutated = build('unhandled-group-action');
assert.ok(!mutated.includes(anchor), 'anchor consumed');
assert.ok(mutated.includes('.catch(function(e){throw e;});'), 'catch rethrows');
assert.strictEqual(mutated.split('throw e;').length - 1, 1, 'exactly one replacement');
const silent = build('silent-group-action-feedback');
assert.ok(silent.includes(anchor), 'silent knife leaves catch intact');
assert.ok(!silent.includes("groupFeedback(feedback||'group-status',msg||('HTTP '+r.status));return false;"), 'silent knife consumed http anchor');
const drifted = fnText.replace(anchor, "status.textContent='编组操作失败: '+String(err&&err.message||err);return false;");
let guardError = null;
try { build('unhandled-group-action', drifted); } catch (err) { guardError = err.message; }
assert.ok(guardError && guardError.startsWith('mutation anchor missing:'), 'guard throws on missing anchor: ' + guardError);
console.log('GUARD-DIAG OK');
console.log('catch_after_mutation=' + mutated.slice(mutated.indexOf('.catch(function(e){'), mutated.indexOf('.catch(function(e){') + 40));
console.log('guard_error=' + guardError.slice(0, 160));
