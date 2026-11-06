// 🔴 view-service/md-render-core.js —— markdown 渲染的【共享件 · 单一事实源】
//
// 为什么有这个文件(组长 ts-No.79 后续裁定,选型 A):
//   `0.2.1.50` 的交付条件写着「spec 展示面必须与 timeline 走**同一条**图片渲染路径,
//   或显式证明它自带等效兜底」。定界件(runbooks/BOUNDARY-SPEC-RENDER-PATH-0.2.1.50-*.md)
//   实测出 spec 展示面**根本不渲染**(正文直接进 textarea)⇒ 要满足那条,两个页面必须跑
//   **同一份字节**。本文件就是那一份。
//
// 消费方:
//   ① timeline 页 `renderer-canary-sse.html` —— 里面那 6 段是**本文件的生成副本**
//      (标记 `// DW-MD-CORE:<段名> BEGIN/END`),🔴 不是第二份清单,是同一份字节的副本。
//   ② spec 展示面(`0.2.1.50`,由 coder56solzzc 接)—— 直接把本文件整份内联进去即可。
//   ③ Node 侧闸 —— `require('./md-render-core.js')` 拿到同一个 API(见文末导出钩子)。
//
// 🔴 改法(顺序不能反):
//      改本文件 → 跑 `python3 view-service/md-render-core-sync.py` → 提交两份
//      漂移由 `view-service/test_md_render_core.py` 挡:不同步 ⇒ 红。
//      🔴 **不要直接改 renderer 里那 6 段** —— 它是生成物,你改的会被下次同步覆盖。
//
// 🔴 三条硬约束(改这个文件前必须知道,每条都有闸或实测依据):
//   1 **不得包 IIFE / 不得改成模块作用域**。renderer 是一个大 `<script>` 的顶层脚本,
//     这些 `function`/`const` 因此是**页面作用域**的:
//       · `renderer-0206-file-render-regression.js` 的 ④i 用 `page.evaluate` **就地改**
//         `FILE_EXT_KIND.svg = 'image'` 来验 deny-set(取不到就静默变空用例,它自己会报错);
//       · `renderer-markdown-image-regression.js` 的 G0b 直接调 `isOwnFileUrl()` 自证边界前提。
//     包进 IIFE ⇒ 两处都取不到 ⇒ 那两格会变成**假绿或假失败**。
//   2 **消毒口径不得改**:先 `escapeHtml` 再 markdown(`renderInline` 第一行就是转义)。
//     `escapeHtml` 里 `"` → `&quot;` 那一句是**承重墙**(ts-No.76 认定:协议白名单挡不住
//     以合法 https:// 开头的属性突破,挡住它的是这一句)⇒ 两件闸各有阳性对照盯着。
//   3 🔴 **`isOwnFileUrl` / `fileExtOf` 依赖浏览器的 `location`;而在非浏览器环境里它们是
//     【静默】降级的 —— 不报错、不告警**(实测 Node v22,`typeof location === 'undefined'`):
//         isOwnFileUrl(本站 /f/ png) → **false**      fileExtOf(同一 URL) → **''**
//         kindFromExt(同一 URL)      → **'download'** isInlineImageUrl(同一 URL) → **false**
//         ⇒ renderMarkdown('![图](本站 /f/ png)') 产出的是**链接**,不是 <img>
//     成因:这两个函数自带 `try { … } catch { return false / '' }` ⇒ `location` 的 ReferenceError
//     **被 catch 吞掉**。🔴 所以症状是"一张图都不出、却全绿",看起来像"产品没做图片支持",
//     而真相是环境里没有 location —— 与本组反复撞的"东西建好了但没接上"同一形状。
//     ⇒ 消费方若在非浏览器环境跑这份核心(SSR / Node 侧测试 / Worker),**必须自备 location 等价物**,
//       否则拿到的就是上面那个静默降级结果。这条由 test_md_render_core.py 的 Node 冒烟腿钉住实测值。
//     🔵 spec 展示面在浏览器里跑 ⇒ 不受影响;其余函数都是纯函数(不碰 location/DOM)。
//     🔴 我原先在这里写的是"调用这两个会抛" —— 那是**读码**结论,**实测证伪**(try/catch 吞掉了)。已更正。
//
// 🔴 给 spec 页那侧的【消费契约】—— 本文件只产出 HTML **字符串**,下面三件它**不负责**,
//    宿主页面必须自备,否则会出现"看起来接上了、其实缺一块":
//      · **CSS**:`.md`、`.md img.md-img`(420px 上限那一套)、`.code-wrap`/`.code-toolbar`/
//        `.tok-kw|.tok-str|.tok-com|.tok-num`、`.table-wrap`
//      · **Copy 按钮接线**:`renderCode` 会产出 `<button class="copy-code" data-code=…>`,
//        不接线就是个**按了没反应的按钮**(renderer 侧由 `wireCopyButtons` 接)
//      · 🔴 **图片取不到时的"下载出路"**:`mdImageHtml` 产出的 `<img data-md-img>` 加载失败后,
//        renderer 侧是靠 `wireMarkdownImages` + `attachFileRenders`(附件卡,带「已过 7 天保留期」
//        与「⬇ 下载图片」)**兜底**的 —— 🔴 **那一层不在本文件里**(它依赖 HEAD 探测/capability/
//        DOM 卡片整套附件机制)。组长已明令「不能只剩 error 撤除」⇒ spec 页必须自带等效兜底,
//        否则交付条件那一条不成立。**这条已报组长,不在本格自行决定。**
//
// 🔴 给 Go 侧的提醒:本文件含**反引号**(模板字面量)⇒ **不能**塞进 Go 的反引号原始字符串字面量
//    (spec 页现在的 `consoleSpecsPageHTML` 正是那种写法)。用 `go:embed` + 拼接,别用原始串。
//
// ── 段的划分(6 段,🔴 **零移动**:每段在产品件里的位置与抽取前逐字相同)──────────────────
//    为什么不合成一段:这些符号在产品件里**本来就不连续**(与附件路径的代码交错),
//    而本格的硬条件是「抽件前后两件闸的结论逐项相同」⇒ 合并就意味着**移动代码**,
//    移动 `const` 会引入 TDZ 风险。所以按现状切成 6 段,一段一个标记对。
//      escape        escapeHtml
//      own-url       isOwnFileUrl(本站 /f/ 边界)
//      ext-kind      FILE_EXT_KIND(🔴 图片行 == nginx types{image/*},由 G0 对账)
//      ext-of        fileExtOf
//      never-inline  FILE_EXT_NEVER_INLINE(svg 等**写死**永不内联)+ kindFromExt
//      render        renderMarkdown/splitFencedBlocks/renderMarkdownText/isTableStart/
//                    splitTableRow/renderTable/renderList/renderInline/isInlineImageUrl/
//                    mdImageHtml/renderCode/highlightCode

// DW-MD-CORE:escape BEGIN
    function escapeHtml(value) {
      return String(value ?? '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }
// DW-MD-CORE:escape END

// DW-MD-CORE:own-url BEGIN
    function canonicalPublicOrigin() {
      try {
        const raw = typeof globalThis !== 'undefined' ? globalThis.DW_CANONICAL_PUBLIC_ORIGIN : '';
        const u = new URL(raw);
        return u.protocol === 'https:' ? u.origin : '';
      } catch { return ''; }
    }

    function isOwnFileUrl(raw) {
      try {
        const u = new URL(raw, location.href);
        return u.pathname.startsWith('/f/') &&
          (u.origin === location.origin || (canonicalPublicOrigin() && u.origin === canonicalPublicOrigin()));
      } catch { return false; }
    }

    function localFileUrl(raw) {
      try {
        const u = new URL(raw, location.href);
        if (!u.pathname.startsWith('/f/') || u.origin !== canonicalPublicOrigin()) return raw;
        u.protocol = location.protocol;
        u.host = location.host;
        return u.href;
      } catch { return raw; }
    }

    function canonicalFileUrl(raw) {
      try {
        const u = new URL(raw, location.href);
        if (u.origin !== location.origin || !u.pathname.startsWith('/f/')) return raw;
        const origin = canonicalPublicOrigin();
        if (!origin) return raw;
        return origin + u.pathname + u.search + u.hash;
      } catch { return raw; }
    }
// DW-MD-CORE:own-url END

// DW-MD-CORE:ext-kind BEGIN
    // 附录 A5 那张白名单的**反向映射**，只在 HEAD 不可用时作降级依据。
    // 🔴 表外的扩展名（含 .bin）一律下载链接，不尝试内联。
    // 🔴 本表只在【降级路径】上用（HEAD 拿不到 Content-Type 时按 URL 扩展名分派）。
    //    B-11（组长 2026-09-19 授权）补两类，成因都是「B 侧本批加宽了能力，本表没跟上」：
    //    ① 音频：B 侧本批新增 audio 落盘与真实 audio MIME（mp3/m4a/flac/aac/opus/oga/wav），
    //       本表若不补，降级路径上音频一律落 download ——「否则不播」那个目的就没达成。
    //    ② 文本：B 侧 filenameTextExts 有 15 个扩展名统一出 text/plain，
    //       而本表此前只有 txt ⇒ md/csv/py/yml/json 在 HEAD 路径内联、在降级路径落 download，
    //       **同一个文件两条路径两种结果**。这里与 B 侧那张表对齐。
    //    🔴 对齐对象写死在这里，便于下次核：internal/fileservice/manager.go 的 filenameTextExts。
    //    ⚠️ 补进本表的扩展名必须与 FILE_EXT_NEVER_INLINE 不相交（④h(a) 盯着这条）。
    const FILE_EXT_KIND = {
      // 🔴 图片行必须 == `deploy/nginx-dingwei.conf` 里 `types{image/*}` 那一组(**单一事实源在 nginx**),
      //    另加一个已登记别名 `jpeg`(image/jpeg 的另一种写法;nginx 的 /f/ 只放行 jpg)。
      //    🔴 **本仓只有这一份图片扩展名清单**:markdown 就地出图(ts-No.79)也走它(经 kindFromExt),
      //    不许在别处再列一份 —— 漂移由 renderer-markdown-image-regression.js 的 G0 逐项对账
      //    (少一个、多一个、连"独有 token 在源码里出现两次"都红)。
      //    ⚠️ 补进本表的扩展名必须与 FILE_EXT_NEVER_INLINE 不相交(④h(a) 与 G0(c) 都盯着这条)。
      png: 'image', jpg: 'image', jpeg: 'image', gif: 'image', webp: 'image',
      avif: 'image', heic: 'image', bmp: 'image', tiff: 'image',
      mp4: 'video', webm: 'video',
      mp3: 'audio', m4a: 'audio', flac: 'audio', aac: 'audio', opus: 'audio', oga: 'audio', wav: 'audio',
      pdf: 'pdf',
      txt: 'text', md: 'text', csv: 'text', json: 'text', log: 'text', py: 'text', js: 'text',
      go: 'text', java: 'text', yaml: 'text', yml: 'text', toml: 'text', ini: 'text', sh: 'text', sql: 'text',
    };
// DW-MD-CORE:ext-kind END

// DW-MD-CORE:ext-of BEGIN
    function fileExtOf(url) {
      try {
        const last = new URL(url, location.href).pathname.split('/').pop() || '';
        const dot = last.lastIndexOf('.');
        return dot > 0 ? last.slice(dot + 1).toLowerCase() : '';
      } catch { return ''; }
    }
// DW-MD-CORE:ext-of END

// DW-MD-CORE:never-inline BEGIN
    // 🔴 附录 E：这几个扩展名**永不内联**，且**不看 FILE_EXT_KIND 是否列了它们**。
    //    为什么要独立一层：今天 svg/html 不在表里，安全是靠「没写」得来的；
    //    将来有人「补全白名单」把它们加进去，降级路径就会内联 —— 而那条路径当时没有任何断言盯着。
    //    （同一形状：守卫加完就假定它会一直有效。组长 2026-09-18 在 ⑭ 闸上遇到的也是这个。）
    const FILE_EXT_NEVER_INLINE = new Set(['svg', 'svgz', 'html', 'htm', 'xhtml', 'xml']);
    function kindFromExt(url) {
      const ext = fileExtOf(url);
      if (FILE_EXT_NEVER_INLINE.has(ext)) return 'download';
      return FILE_EXT_KIND[ext] || 'download';
    }
// DW-MD-CORE:never-inline END

// DW-MD-CORE:render BEGIN
    function renderMarkdown(src) {
      const blocks = splitFencedBlocks(src);
      let html = '';
      for (const block of blocks) {
        if (block.type === 'code') {
          html += renderCode(block.code, block.lang);
        } else {
          html += renderMarkdownText(block.text);
        }
      }
      return `<div class="md">${html}</div>`;
    }

    function splitFencedBlocks(src) {
      const lines = src.split('\n');
      const blocks = [];
      let text = [];
      let inCode = false;
      let lang = '';
      let code = [];
      for (const line of lines) {
        const fence = line.match(/^```([A-Za-z0-9_-]*)\s*$/);
        if (fence && !inCode) {
          if (text.length) blocks.push({ type: 'text', text: text.join('\n') });
          text = [];
          inCode = true;
          lang = fence[1] || '';
          code = [];
        } else if (fence && inCode) {
          blocks.push({ type: 'code', lang, code: code.join('\n') });
          inCode = false;
          lang = '';
          code = [];
        } else if (inCode) {
          code.push(line);
        } else {
          text.push(line);
        }
      }
      if (inCode) {
        text.push('```' + lang);
        text.push(...code);
      }
      if (text.length) blocks.push({ type: 'text', text: text.join('\n') });
      return blocks;
    }

    function renderMarkdownText(src) {
      const lines = src.split('\n');
      let html = '';
      for (let i = 0; i < lines.length;) {
        if (!lines[i].trim()) { i++; continue; }
        if (isTableStart(lines, i)) {
          const table = [];
          while (i < lines.length && lines[i].includes('|') && lines[i].trim()) table.push(lines[i++]);
          html += renderTable(table);
          continue;
        }
        const heading = lines[i].match(/^(#{1,3})\s+(.+)$/);
        if (heading) {
          const level = heading[1].length;
          html += `<h${level}>${renderInline(heading[2])}</h${level}>`;
          i++;
          continue;
        }
        if (/^>\s?/.test(lines[i])) {
          const quote = [];
          while (i < lines.length && /^>\s?/.test(lines[i])) quote.push(lines[i++].replace(/^>\s?/, ''));
          html += `<blockquote>${renderInline(quote.join('\n'))}</blockquote>`;
          continue;
        }
        if (/^---+$/.test(lines[i].trim())) {
          html += '<hr>';
          i++;
          continue;
        }
        if (/^(\s*)([-*+]|\d+\.)\s+/.test(lines[i])) {
          const list = [];
          const ordered = /^\s*\d+\.\s+/.test(lines[i]);
          while (i < lines.length && /^(\s*)([-*+]|\d+\.)\s+/.test(lines[i])) list.push(lines[i++]);
          html += renderList(list, ordered);
          continue;
        }
        const paraStart = i;
        const para = [];
        while (i < lines.length && lines[i].trim() && !isTableStart(lines, i) && !/^(\s*)([-*+]|\d+\.)\s+/.test(lines[i]) && !/^(#{1,3})\s+/.test(lines[i]) && !/^>\s?/.test(lines[i]) && !/^---+$/.test(lines[i].trim())) {
          para.push(lines[i++]);
        }
        // 🔴 无进展保护 —— 长文本「卡死」的直接原因就在这里。
        // 本函数的外层 `for (let i = 0; i < lines.length;)` 没有自增,完全依赖各分支推进 i。
        // 「### 」这种**只有标记、没有标题**的行会两头落空:标题分支的 /^(#{1,3})\s+(.+)$/
        // 要求标题非空所以不匹配;段落分支的守卫里又有 !/^(#{1,3})\s+/ 把它挡在外面 ——
        // 于是一行都不吃、i 原地不动,外层 for 无限循环,html 每轮追加一个空 <p></p>,
        // 约 7600 万轮后撑爆 V8 字符串上限抛 RangeError,页面表现为「卡住几分钟后再也渲染不出来」。
        // 流式下只要某个 chunk 边界正好落在「### 」之后就会踩到(实测 7964 字契约文本的
        // 3680 字前缀恰好如此);非流式下,正文里任何一个空标题行同样会踩到。
        // 兜底:若这一轮没有任何前进,就把该行按普通段落吃掉,保证外层循环必定推进、且不丢内容。
        if (i === paraStart) para.push(lines[i++]);
        html += `<p>${renderInline(para.join('\n'))}</p>`;
      }
      return html;
    }

    function isTableStart(lines, i) {
      return i + 1 < lines.length && /^\s*\|?.+\|.+\|?\s*$/.test(lines[i]) && /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(lines[i + 1]);
    }

    function splitTableRow(row) {
      const cells = [];
      let cell = '';
      let escaped = false;
      const trimmed = row.trim().replace(/^\|/, '').replace(/\|$/, '');
      for (const ch of trimmed) {
        if (escaped) { cell += ch; escaped = false; continue; }
        if (ch === '\\') { escaped = true; continue; }
        if (ch === '|') { cells.push(cell.trim()); cell = ''; continue; }
        cell += ch;
      }
      cells.push(cell.trim());
      return cells;
    }

    function renderTable(rows) {
      const head = splitTableRow(rows[0]);
      const body = rows.slice(2).map(splitTableRow);
      return `<div class="table-wrap"><table><thead><tr>${head.map(c => `<th>${renderInline(c)}</th>`).join('')}</tr></thead><tbody>${body.map(r => `<tr>${r.map(c => `<td>${renderInline(c)}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`;
    }

    function renderList(lines, ordered) {
      const tag = ordered ? 'ol' : 'ul';
      const items = lines.map(line => {
        let text = line.replace(/^(\s*)([-*+]|\d+\.)\s+/, '');
        const task = text.match(/^\[( |x|X)\]\s+(.+)$/);
        if (task) {
          const checked = task[1].toLowerCase() === 'x' ? ' checked' : '';
          text = `<input type="checkbox" disabled${checked}> ${renderInline(task[2])}`;
        } else {
          text = renderInline(text);
        }
        return `<li>${text}</li>`;
      }).join('');
      return `<${tag}>${items}</${tag}>`;
    }

    // 🔴 ts-No.79(业主:「只要是图片的就显示预览的图」「感叹号可以不用了」):
    //    "这条 URL 该不该就地出图"的**唯一**判据。两个前提都复用**已有**的东西,不新造清单、不新造边界:
    //      · isOwnFileUrl —— 附件那条路已在用的信任边界:只认本站 /f/ 直链,
    //        🔴「不对任意外站 URL 做任何请求」(见 FILE_URL_RE 上面那段注释)⇒ 外站图片**仍是链接**。
    //        🔵 这是一条产品可见的边界:要放开得由业主裁,不在本格顺手改(G4 把现状钉住了)。
    //      · kindFromExt —— 先查 FILE_EXT_NEVER_INLINE(svg/svgz/html/htm/xhtml/xml **写死**永不内联;
    //        派工单④:不靠"名单里没写"),再查 FILE_EXT_KIND(== nginx 的 types{image/*},G0 对账)。
    function isInlineImageUrl(url) {
      return isOwnFileUrl(url) && kindFromExt(url) === 'image';
    }

    // 🔴 派工单④:一律 <img>,**绝不内联 SVG 源码** —— 本函数只可能产出 <img>,
    //    没有任何一条路会把文件字节读进来拼成元素(svg 更是上面那层写死的 deny-set 就挡住了)。
    //    alt/src 用的都是 escapeHtml **之后**的串(引号已是 &quot;)⇒ 属性突破不成立;
    //    那道承重墙由 ts-No.76 的 P12 与本件 G3/M-G3 一起钉着。
    function mdImageHtml(alt, url) {
      return `<img class="md-img" data-md-img src="${localFileUrl(url)}" alt="${alt}" loading="lazy" referrerpolicy="no-referrer">`;
    }

    function renderInline(src) {
      let out = escapeHtml(src);
      const codes = [];
      out = out.replace(/`([^`]+)`/g, (_, code) => {
        const token = `@@CODE${codes.length}@@`;
        codes.push(`<code>${code}</code>`);
        return token;
      });
      // 🔴 ts-No.79:`!?` 把感叹号一起吃掉 ⇒「! ＋ 链接」那个怪形态消失(业主:感叹号可以不用了)。
      //    指向图片的就**就地出图**,其余**仍是链接**(派工单③:非图片链接行为不变,只是少了那个 "!")。
      //    🔵 用函数式替换而不是 '$1/$2' 串:捕获值里的 `$` 不再有机会被当替换模式解释。
      out = out.replace(/(!?)\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, (_, bang, text, url) => (
        isInlineImageUrl(url)
          ? mdImageHtml(text, url)
          : `<a href="${localFileUrl(url)}" rel="noreferrer" target="_blank">${text}</a>`));
      out = out.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
      out = out.replace(/(^|[^*])\*([^*\n]+)\*/g, '$1<em>$2</em>');
      out = out.replace(/\n/g, '<br>');
      codes.forEach((html, i) => { out = out.replace(`@@CODE${i}@@`, html); });
      return out;
    }

    function renderCode(code, lang) {
      const safeLang = escapeHtml(lang || 'text');
      const highlighted = highlightCode(code, lang);
      const encoded = encodeURIComponent(code);
      return `<div class="code-wrap"><div class="code-toolbar"><span>${safeLang}</span><button class="copy-code" type="button" data-code="${encoded}">Copy</button></div><pre><code>${highlighted}</code></pre></div>`;
    }

    // 🔴 2026-09-25 修:每一条正则原先都作用在【上一条已经产出的 HTML】上,
    //    于是后一条会把前一条生成的标签当成源码再包一层。实测触发点是关键字名单里的
    //    `class` 命中了 `<span class="tok-str">` 的**属性名**:
    //      print("copy-me")  ⇒  print(<span <span class="tok-kw">class</span>="tok-str">…</span>)
    //      屏上显示成         print(class="tok-str">"copy-me")
    //    ⇒ **凡含字符串字面量的 python 代码块都坏**,spec 页与 timeline 共用本核,两处都坏。
    // 🔵 做法不是把 `class` 从名单里删掉(那只挡住今天这一个词,明天加 `for`/`type` 又会犯);
    //    而是让后续规则【看不见已经包好的部分】:每包完一类就把生成的 span 收进保险箱,
    //    原地留一个不可能被任何规则命中的占位,全部替换结束后再还原。
    function highlightCode(code, lang) {
      // 占位用的两个控制字符若真出现在源码里,先剔掉,免得还原阶段被伪造的占位污染。
      let html = escapeHtml(code).replace(/[\u0001\u0002]/g, '');
      const vault = [];
      // 🔴 占位里【不能出现数字】:数字规则 \b(\d+)\b 会把占位的序号当成代码里的数字包起来,
      //    stash 再把它收走 ⇒ 占位嵌套、还原失败,屏上直接漏出控制字符。(本次实测踩到过。)
      //    故序号按 26 进制编成 A..P,所有规则(关键字/字面量全是小写)都命中不了它。
      const encodeIndex = (n) => n.toString(26).replace(/[0-9a-p]/g, (ch) => String.fromCharCode(65 + parseInt(ch, 26)));
      const decodeIndex = (s) => s.split('').reduce((acc, ch) => acc * 26 + (ch.charCodeAt(0) - 65), 0);
      const stash = (value) => value.replace(/<span class="tok-[a-z]+">[\s\S]*?<\/span>/g, (hit) => {
        vault.push(hit);
        return '\u0001' + encodeIndex(vault.length - 1) + '\u0002';
      });
      const wrap = (pattern, replacement) => { html = stash(html.replace(pattern, replacement)); };
      if ((lang || '').toLowerCase() === 'python') {
        wrap(/(#.*)$/gm, '<span class="tok-com">$1</span>');
        wrap(/(&#39;[^&]*?&#39;|&quot;[^&]*?&quot;)/g, '<span class="tok-str">$1</span>');
        wrap(/\b(def|return|if|else|elif|for|in|and|or|not|class|import|from|while|try|except|with|as|lambda)\b/g, '<span class="tok-kw">$1</span>');
        wrap(/\b(\d+(?:\.\d+)?)\b/g, '<span class="tok-num">$1</span>');
      } else if ((lang || '').toLowerCase() === 'json') {
        wrap(/(&quot;[^&]*?&quot;)(\s*:)/g, '<span class="tok-kw">$1</span>$2');
        wrap(/(:\s*)(&quot;[^&]*?&quot;)/g, '$1<span class="tok-str">$2</span>');
        wrap(/\b(true|false|null)\b/g, '<span class="tok-kw">$1</span>');
        wrap(/\b(\d+(?:\.\d+)?)\b/g, '<span class="tok-num">$1</span>');
      }
      return html.replace(/\u0001([A-P]+)\u0002/g, (_, index) => vault[decodeIndex(index)]);
    }
// DW-MD-CORE:render END

// ── 导出钩子 ────────────────────────────────────────────────────────────────────────────
// 🔴 本段**不在任何 DW-MD-CORE 段内** ⇒ 同步工具不会把它复制进 renderer。
//    renderer 不需要它(顶层声明本身就是页面作用域);spec 页与 Node 侧需要。
// 🔴 名单必须与上面各段里定义的符号**逐项一致** —— 少一个,spec 页就会静默缺一个函数,
//    而"静默缺"比"报错缺"贵得多。这条由 test_md_render_core.py 用正则从段里抽名字对账。
(function (global) {
  const api = {
    escapeHtml: escapeHtml,
    isOwnFileUrl: isOwnFileUrl,
    canonicalPublicOrigin: canonicalPublicOrigin,
    localFileUrl: localFileUrl,
    canonicalFileUrl: canonicalFileUrl,
    fileExtOf: fileExtOf,
    kindFromExt: kindFromExt,
    FILE_EXT_KIND: FILE_EXT_KIND,
    FILE_EXT_NEVER_INLINE: FILE_EXT_NEVER_INLINE,
    splitFencedBlocks: splitFencedBlocks,
    renderMarkdown: renderMarkdown,
    renderMarkdownText: renderMarkdownText,
    isTableStart: isTableStart,
    splitTableRow: splitTableRow,
    renderTable: renderTable,
    renderList: renderList,
    renderInline: renderInline,
    renderCode: renderCode,
    highlightCode: highlightCode,
    isInlineImageUrl: isInlineImageUrl,
    mdImageHtml: mdImageHtml,
  };
  if (global) global.DwMdCore = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : (typeof globalThis !== 'undefined' ? globalThis : this));
