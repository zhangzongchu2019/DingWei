// 🔴 第三方字节的**唯一 pin 源**(防第二事实源:三个探针都 require 这一份,不许各自写死)。
//
// 边界(组长 ts-No.77 裁定 (a)):装置**准予进仓**,但 🔴 **第三方字节不入仓** ——
//   `vendor/` 在 `.gitignore` 里;字节由 `prepare-vendor.js` 从 **npm / 内网镜像**取回并逐字校验这些 pin。
// 硬条件⑤:🔴 FA 字节**不得抄**本次交付时从 `maxcdn.bootstrapcdn.com` 取回的那份 —— 走 npm。
//   🔵 事实记录(交付件 §三):npm `font-awesome@4.7.0` 的 `css/font-awesome.min.css` 与
//      bootstrapcdn `latest` 当时发的那份**逐字同哈希**(`799aeb25…`),`woff2` 亦同(`2adefcbc…`)
//      ⇒ 换供应路径**不改字节**;但**供应路径本身**才是要治的东西(可审计、可 pin、可离线镜像)。
'use strict';

const fs = require('fs');
const path = require('path');
const crypto = require('crypto');

const HERE = __dirname;

// npm `easymde@2.21.0` tarball 内 `package/dist/` 两份(= 交付件里量过的那两份,逐字一致)
const PINS_MAIN = {
  'easymde.min.js': '2c06bddfd0c89176db08ccf9d42e2beaa9b4f1a4ff8fb2ec0bf1bed25ce08e05',
  'easymde.min.css': '6eee36340432776d682e7372ec4a7eb29be4fdb3ffada32c0bfa5e31a5ef34a2'
};

// npm `font-awesome@4.7.0`(许可证 registry 字段:`(OFL-1.1 AND MIT)`)
//   `fonts/fontawesome-webfont.woff2` 用上游原字节;
//   `fa.min.css` 是**派生件**:上游 css 把 6 处 `../fonts/` 改写为 `/vendor/fonts/`(prepare-vendor.js 里断言 6 处),
//   ⇒ pin 的是**派生后**的哈希;上游原哈希 `799aeb25…` 由 prepare-vendor.js 先校验再改写。
const PINS_FA = {
  'fa.min.css': '413eb1dc4d9e53a480fb226219f093854d21ab3214c7da778e62759719ef95ee',
  'fonts/fontawesome-webfont.woff2': '2adefcbc041e7d18fcf2d417879dc5a09997aa64d675b7a3c4b6ce33da13f3fe'
};

// 上游原字节哈希(prepare-vendor.js 在改写**之前**校验这两个)
const UPSTREAM = {
  'easymde.tgz': '72ef8983e4e53f3648cae5a0e5bfdbd6a25e013b2aa84d264e7b90e8e377679d',
  'font-awesome.tgz': null,   // 🔵 见 prepare-vendor.js:tarball 哈希随镜像/时间可能变 ⇒ 以**包内文件**哈希为准
  'font-awesome.min.css(上游原字节)': '799aeb25cc0373fdee0e1b1db7ad6c2f6a0e058dfadaa3379689f583213190bd'
};

const sha256 = (f) => crypto.createHash('sha256').update(fs.readFileSync(f)).digest('hex');

// 🔴 缺件或哈希不符 ⇒ **exit(3) 中止**,不产出任何结论。
//    理由与 S0 同形:**手段可能失效,所以先证手段再产出结论**。
function requireVendor(pins, what) {
  const missing = [], bad = [];
  for (const name of Object.keys(pins)) {
    const f = path.join(HERE, 'vendor', name);
    if (!fs.existsSync(f)) { missing.push(name); continue; }
    const h = sha256(f);
    if (h !== pins[name]) bad.push(name + ' 实测 ' + h + ' ≠ pin ' + pins[name]);
  }
  if (missing.length || bad.length) {
    console.error('🔴 被测字节不可用(' + what + ')⇒ **中止,不产出任何结论**(rc=3)。');
    if (missing.length) console.error('   缺:' + missing.join(', ') + '  ⇒ 先跑  node prepare-vendor.js');
    if (bad.length) {
      console.error('   哈希不符:');
      bad.forEach((b) => console.error('     ' + b));
      console.error('   🔴 哈希不符 = 量的**不是**交付件里那份字节 ⇒ 结论不可比,别跑');
    }
    process.exit(3);
  }
  return Object.keys(pins).map((n) => n + ' sha256=' + sha256(path.join(HERE, 'vendor', n)).slice(0, 16) + '…');
}

module.exports = { HERE, PINS_MAIN, PINS_FA, UPSTREAM, sha256, requireVendor };
