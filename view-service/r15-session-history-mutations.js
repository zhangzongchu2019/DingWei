// Confirm the history order and scroll-position assertions each detect its own regression.
const assert = require('assert');
const { spawnSync } = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');

const root = __dirname;
const source = fs.readFileSync(path.join(root, 'renderer-canary-sse.html'), 'utf8');
const cases = [
  {
    name: 'drop_history_node_move',
    from: 'if (insertionPoint) for (const node of added) timeline.insertBefore(node, insertionPoint);',
    to: '/* mutant: omitted history node move */',
    expected: 'timeline order invalid:',
  },
  {
    name: 'drop_scroll_anchor_correction',
    from: 'if (anchor && Number.isFinite(anchorTop)) timeline.scrollTop += anchor.getBoundingClientRect().top - anchorTop;',
    to: '/* mutant: omitted scrollTop anchor correction */',
    expected: 'scroll anchor shifted',
  },
];

const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'r15-history-mutants-'));
try {
  for (const test of cases) {
    assert.strictEqual(source.split(test.from).length - 1, 1, `${test.name}: source anchor count`);
    const override = path.join(temp, `${test.name}.html`);
    fs.writeFileSync(override, source.replace(test.from, test.to));
    const result = spawnSync(process.execPath, [path.join(root, 'r15-session-history-browser-regression.js')], {
      cwd: path.dirname(root), encoding: 'utf8', timeout: 30000,
      env: { ...process.env, RENDERER_OVERRIDE: override },
    });
    const output = `${result.stdout || ''}${result.stderr || ''}`;
    assert.notStrictEqual(result.status, 0, `${test.name}: mutant unexpectedly passed\n${output}`);
    assert.ok(output.includes(test.expected), `${test.name}: wrong assertion failed\n${output}`);
    console.log(`PASS mutant=${test.name} rc=${result.status} red_at="${test.expected}"`);
  }
} finally {
  fs.rmSync(temp, { recursive: true, force: true });
}
