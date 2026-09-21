// 🔴 markdown 注入形状清单 —— **单一事实源**(ts-No.76 建 · ts-No.79 起两件共用)
//
// 为什么单独成一个模块:ts-No.79(图片按类型自动显示)的注入腿要求「复用 ts-No.76 的相关形状,
// **不要另起炉灶**」(组长板件逐字)。复制一份清单 = 两份必漂移(本仓已在"两份计划件各说各话"上栽过),
// ⇒ 把清单抽出来,两件 require 同一个文件。**改形状只改这里,两件的断言同时跟着变。**
//
// 🔴 结论口径不变(组长 ts-No.76 明令):任何一件都**只能**得出「这些形状不成立」,
//    **不得**升级成「无 XSS」—— 清单是人列的,不是穷尽的。
//
// 使用方:
//   · renderer-markdown-injection-regression.js(ts-No.76)—— 全部 17 形状 + 七例 + 四条阳性对照
//   · renderer-markdown-image-regression.js(ts-No.79)—— 取其中与"URL/图片"相关的子集(见该件 IMAGE_RELEVANT)
'use strict';

// ── payload 目录 ────────────────────────────────────────────────────────────────
// 🔵 canary 一律写 `window.__dwPwned`:它被置上 = 有脚本真执行了(而不是"看起来没执行")。
const CANARY = 'window.__dwPwned=1';
const PAYLOADS = [
  { id: 'P01', text: `<script>${CANARY}</script>`, why: '裸 script 标签' },
  { id: 'P02', text: `<img src=x onerror="${CANARY}">`, why: 'img onerror(最经典:src 必然加载失败 ⇒ 必然触发)' },
  { id: 'P03', text: `<iframe src="javascript:${CANARY}"></iframe>`, why: 'iframe + javascript: 协议' },
  { id: 'P04', text: `<svg onload="${CANARY}"></svg>`, why: 'svg onload' },
  { id: 'P05', text: `<details ontoggle="${CANARY}" open>x</details>`, why: 'ontoggle(open 属性 ⇒ 不需交互就触发)' },
  { id: 'P06', text: `<a href="javascript:${CANARY}">点我</a>`, why: '裸 a + javascript:' },
  { id: 'P07', text: `[x](javascript:${CANARY})`, why: 'markdown 链接塞 javascript: 协议' },
  { id: 'P08', text: `[x](JaVaScRiPt:${CANARY})`, why: '协议名大小写绕过' },
  { id: 'P09', text: '[x](  javascript:' + CANARY + ')', why: '协议前置空白绕过' },
  { id: 'P10', text: `[x](&#106;avascript:${CANARY})`, why: 'HTML 实体绕过(href 里实体会被解码)' },
  { id: 'P11', text: '[x](data:text/html;base64,PHNjcmlwdD53aW5kb3cuX19kd1B3bmVkPTE8L3NjcmlwdD4=)', why: 'data:text/html + base64' },
  { id: 'P12', text: `[x](https://ok.example/"onmouseover="${CANARY})`, why: '🔴 链接 URL 里的**属性突破**(整段无空格无右括号 ⇒ 能穿过 [^)\\s]+ 的捕获)' },
  { id: 'P13', text: `<div style="background:url(javascript:${CANARY})">x</div>`, why: 'style 属性里的 url()' },
  // 🔵 P14 原先写成了 `<body onload="${CANARY}>`(引号没闭合)—— 那是**我写错了**,不是故意的形状;
  //    已改回本意。"不闭合"这个形状由 P17 专门承担。
  { id: 'P14', text: `<body onload="${CANARY}">`, why: 'body onload(整页级)' },
  // 🔵 以下三种是第一跑绿之后**自己加的**:组长说「以及你认为该试的其它形状」。
  { id: 'P15', text: '`<img src=x15 onerror="' + CANARY + '">`', why: '行内代码里的 payload:必须是 <code> 内的**死文本**(反引号提取发生在转义之后)' },
  { id: 'P16', text: `&lt;img src=x16 onerror="${CANARY}"&gt;`, why: '🔴 **双重编码(mXSS)**:送进来时已是转义形态,渲染器若"帮忙解码一次"就会变活' },
  { id: 'P17', text: `<img src=x17 onerror="${CANARY}"`, why: '**不闭合标签**(没有结尾 >):赌解析器宽容度' },
];
const ALL_TEXT = PAYLOADS.map((p) => p.text).join('\n\n');

// ── 反证("被转义了"而不是"被悄悄丢掉了")的期望形式 ────────────────────────────────
// 🔴 这里踩过一次坑,记下来:期望值不能拿 escapeHtml 的输出直接当。
//    payload 被转义后是**文本节点**,浏览器序列化 innerHTML 时只转 `& < >`,**引号原样**;
//    而落在**属性值**里的那部分(P12)才会序列化成 `&quot;`。两种形态不一样,得分开写。
const escTextNode = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
PAYLOADS.forEach((p) => { p.expect = [escTextNode(p.text)]; });
// P12 的 URL 以 https:// 开头 ⇒ 会被链接正则吃掉,`[x](` 前缀不会以文本出现;
// 它真正要钉的是:**引号被转义了**(所以不可能突破出 href 属性变成 onmouseover 属性)。
const P12 = PAYLOADS.find((p) => p.id === 'P12');
P12.expect = ['&quot;onmouseover=&quot;'];
// P15 的反引号会被行内代码规则消费掉 ⇒ 整条 payload 的转义形态不会原样出现,只断言 <code> 里那一段
const P15 = PAYLOADS.find((p) => p.id === 'P15');
P15.expect = [escTextNode('<img src=x15 onerror="' + CANARY + '">')];
P15.inCode = 'src=x15';   // 额外断言:它必须落在 <code> 元素里,而不是散在正文中

module.exports = { CANARY, PAYLOADS, ALL_TEXT, escTextNode };
