// 🔴 取第三方字节到 `vendor/`(**不入仓**,见 `.gitignore`)并**逐字校验 pin**。
//
// 为什么要有这个文件(组长 ts-No.77 裁定 (a) + 引入硬条件⑤):
//   · 装置**准予进仓**,但 🔴 **第三方字节不入仓** ⇒ 装置必须能在别人机器上**自己把字节取回来**,
//     否则它进了仓也跑不起来 —— 而"跑不起来"最坏的收场是**被人当成"外部请求 0"**。
//   · 🔴 FA **不得抄**本次交付时从 `maxcdn.bootstrapcdn.com` 取回的那份 ⇒ 走 **npm / 内网镜像**。
//     🔵 事实记录:npm `font-awesome@4.7.0` 的 css/woff2 与当时 bootstrapcdn 发的那份**逐字同哈希**
//        (交付件 §三),所以换的是**供应路径**(可审计、可 pin、可镜像),不是字节。
//
// 用法   node prepare-vendor.js                 # 默认 registry.npmjs.org
//        DW_NPM_MIRROR=https://npm.内网/  node prepare-vendor.js
// 幂等   `vendor/` 已在场且哈希全部命中 ⇒ **不联网**,直接 rc=0(所以断网机器上跑第二次也能过)
// rc     0 = 字节就位且哈希命中 · 3 = 取不到/哈希不符(🔴 **不产出半成品**,并说明缺哪一份)
'use strict';

const fs = require('fs');
const path = require('path');
const zlib = require('zlib');
const https = require('https');
const { HERE, PINS_MAIN, PINS_FA, UPSTREAM, sha256, requireVendor } = require('./vendor-pins');

const MIRROR = (process.env.DW_NPM_MIRROR || 'https://registry.npmjs.org').replace(/\/+$/, '');
const VENDOR = path.join(HERE, 'vendor');

const PKGS = [
  { name: 'easymde', version: '2.21.0', want: ['package/dist/easymde.min.js', 'package/dist/easymde.min.css'] },
  { name: 'font-awesome', version: '4.7.0', want: ['package/css/font-awesome.min.css', 'package/fonts/fontawesome-webfont.woff2'] }
];

function get(url, redirects) {
  const left = redirects === undefined ? 5 : redirects;
  return new Promise((resolve, reject) => {
    https.get(url, (res) => {
      if ([301, 302, 303, 307, 308].includes(res.statusCode) && res.headers.location && left > 0) {
        res.resume();
        return resolve(get(new URL(res.headers.location, url).toString(), left - 1));
      }
      if (res.statusCode !== 200) { res.resume(); return reject(new Error('HTTP ' + res.statusCode + ' ' + url)); }
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => resolve(Buffer.concat(chunks)));
      res.on('error', reject);
    }).on('error', reject);
  });
}

// 极简 tar 读取(512 字节头)—— 只取我们要的那几个成员,不落整包,也不依赖系统 `tar`
function tarEntries(buf) {
  const out = {};
  let off = 0;
  while (off + 512 <= buf.length) {
    const hdr = buf.slice(off, off + 512);
    if (hdr.every((b) => b === 0)) break;
    const name = hdr.slice(0, 100).toString('utf8').replace(/\0.*$/, '');
    const sizeOct = hdr.slice(124, 136).toString('utf8').replace(/\0.*$/, '').trim();
    const size = sizeOct ? parseInt(sizeOct, 8) : 0;
    const type = String.fromCharCode(hdr[156]);
    off += 512;
    if (name && type !== '5' && type !== 'L') out[name] = buf.slice(off, off + size);
    off += Math.ceil(size / 512) * 512;
  }
  return out;
}

function write(rel, data) {
  const f = path.join(VENDOR, rel);
  fs.mkdirSync(path.dirname(f), { recursive: true });
  fs.writeFileSync(f, data);
  return f;
}

(async () => {
  console.log('prepare-vendor · 镜像 ' + MIRROR);
  fs.mkdirSync(VENDOR, { recursive: true });

  // 幂等:全部命中就不联网
  const all = Object.assign({}, PINS_MAIN, PINS_FA);
  const alreadyOk = Object.keys(all).every((n) => {
    const f = path.join(VENDOR, n);
    return fs.existsSync(f) && sha256(f) === all[n];
  });
  if (alreadyOk) {
    console.log('🔵 vendor/ 已在场且哈希全部命中 ⇒ **不联网**,直接可用。');
    requireVendor(all, 'prepare-vendor 幂等检查');
    console.log('rc=0'); process.exit(0);
  }

  const blobs = {};
  for (const p of PKGS) {
    const url = MIRROR + '/' + p.name + '/-/' + p.name + '-' + p.version + '.tgz';
    process.stdout.write('· 取 ' + p.name + '@' + p.version + ' … ');
    let tgz;
    try { tgz = await get(url); } catch (e) {
      console.log('🔴 失败:' + e.message);
      console.log('   ⇒ 换内网镜像:DW_NPM_MIRROR=https://你的镜像/ node prepare-vendor.js');
      console.log('rc=3'); process.exit(3);
    }
    const tgzSha = require('crypto').createHash('sha256').update(tgz).digest('hex');
    console.log(tgz.length + ' B,sha256=' + tgzSha.slice(0, 16) + '…');
    if (p.name === 'easymde' && UPSTREAM['easymde.tgz'] && tgzSha !== UPSTREAM['easymde.tgz']) {
      console.log('🔴 tarball 哈希与记录不符:实测 ' + tgzSha + ' ≠ 记录 ' + UPSTREAM['easymde.tgz']);
      console.log('   🔵 不一定有害(镜像重打包会改 tarball 哈希),但**包内文件哈希**必须命中 —— 继续按包内文件判。');
    }
    const entries = tarEntries(zlib.gunzipSync(tgz));
    for (const w of p.want) {
      if (!entries[w]) { console.log('🔴 包内缺成员 ' + w + ' ⇒ 这份 tarball 形状不对,不产出半成品'); console.log('rc=3'); process.exit(3); }
      blobs[w] = entries[w];
    }
  }

  // ① EasyMDE 两份:原字节落盘,哈希必须命中 PINS_MAIN
  write('easymde.min.js', blobs['package/dist/easymde.min.js']);
  write('easymde.min.css', blobs['package/dist/easymde.min.css']);

  // ② Font Awesome:woff2 原字节;css 是**派生件**(改写字体相对路径为夹具的绝对路径)
  const upCss = blobs['package/css/font-awesome.min.css'];
  const upCssSha = require('crypto').createHash('sha256').update(upCss).digest('hex');
  const upPin = UPSTREAM['font-awesome.min.css(上游原字节)'];
  if (upCssSha !== upPin) {
    console.log('🔴 上游 FA css 哈希不符:实测 ' + upCssSha + ' ≠ pin ' + upPin + ' ⇒ 不落盘(不产出半成品)');
    console.log('rc=3'); process.exit(3);
  }
  write('fonts/fontawesome-webfont.woff2', blobs['package/fonts/fontawesome-webfont.woff2']);
  const cssText = upCss.toString('utf8');
  const n = cssText.split('../fonts/').length - 1;
  if (n !== 6) {
    console.log('🔴 改写前断言失败:`../fonts/` 出现 ' + n + ' 次(记录为 6)⇒ 上游 css 形状变了,别静默改写');
    console.log('rc=3'); process.exit(3);
  }
  write('fa.min.css', Buffer.from(cssText.split('../fonts/').join('/vendor/fonts/'), 'utf8'));

  // ③ 落盘后**再验一次**(写盘也可能出错;而且这是"手段先自证"的同一形状)
  requireVendor(all, 'prepare-vendor 落盘后校验');
  console.log('🔵 字节就位:');
  Object.keys(all).forEach((k) => console.log('   ' + k + '  sha256=' + sha256(path.join(VENDOR, k))));
  console.log('🔵 FA css 是**派生件**:上游原字节 ' + upCssSha.slice(0, 16) + '… → 改写 6 处 `../fonts/` 为 `/vendor/fonts/` → '
    + sha256(path.join(VENDOR, 'fa.min.css')).slice(0, 16) + '…');
  console.log('rc=0'); process.exit(0);
})().catch((e) => { console.error('prepare-vendor error:', e); console.log('rc=3'); process.exit(3); });
