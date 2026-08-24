# speclint 语料 v2（grammar 版本随规则改动 bump；v1→v2：由单规则行式改为覆盖全规则的块式）
#
# 格式：
#   CASE <POS|NEG> <rule-kind> [USEFIXTURE]
#   <被测文档片段，可多行>
#   ENDCASE
#
# NEG = 该片段【须命中 rule-kind 这一条规则】（不是「被拦住就行」）
# POS = 该片段【须不被 rule-kind 命中】（防规则过宽）
# USEFIXTURE = 片段前自动拼上下方 FIXTURE（供 count-ref 类需要受保护节的用例）
#
# ⚠️ 语料须有牙：改规则后除跑本语料外，还须以【变异体】验证——
#    故意改坏一条规则，其 NEG 例必须随之失守；不失守说明该 NEG 没在测这条规则。

FIXTURE
### 9.9.9 合成受保护节
下列各条须【同时】满足（不写死条数，增删以本表为准）：
| # | 判据 |
|---|---|
| 1 | 甲 |
| 2 | 乙 |
ENDFIXTURE

# ============ count-ref：引用「不写死条数」的节却带计数 ============
CASE NEG count-ref USEFIXTURE
判定不满足六条而仍放行数 见 §9.9.9
ENDCASE
CASE NEG count-ref USEFIXTURE
见 §9.9.9 第七条
ENDCASE
CASE NEG count-ref USEFIXTURE
第 1–6 条只保护行文本 —— 见 §9.9.9
ENDCASE
CASE NEG count-ref USEFIXTURE
§9.9.9 全部字段（现为 6 项）
ENDCASE
CASE POS count-ref USEFIXTURE
判定不满足 §9.9.9 当时有效条集而仍放行数
ENDCASE
CASE POS count-ref USEFIXTURE
见 §9.9.9，曾写「第七条」
ENDCASE
CASE POS count-ref USEFIXTURE
未引用受保护节时的六条不受本规则管辖
ENDCASE

# ============ id-gap / id-dup / id-orphan：受控 ID 唯一且连续 ============
CASE NEG id-gap
## E.1 第 I 类
| **1** | 甲 |
| **3** | 丙 |
## E.2 第 II 类
## E.3 第 III 类
## E.4 第 IV 类
## E.5
ENDCASE
CASE NEG id-dup
## E.1 第 I 类
| **1** | 甲 |
| **1** | 甲重复 |
| **2** | 乙 |
## E.2 第 II 类
## E.3 第 III 类
## E.4 第 IV 类
## E.5
ENDCASE
CASE NEG id-orphan
## E.1 第 I 类
| **1** | 甲 |
| **2** | 乙 |
| **3** | 丙 |
| **5a** | 挂在不存在的基数 5 上（基数 1–3 本身连续，故不触发 id-gap）|
## E.2 第 II 类
## E.3 第 III 类
## E.4 第 IV 类
## E.5
ENDCASE
CASE POS id-gap
## E.1 第 I 类
| **1** | 甲 |
| **2** | 乙 |
| **2a** | 乙的插入项 |
| **3** | 丙 |
## E.2 第 II 类
## E.3 第 III 类
## E.4 第 IV 类
## E.5
ENDCASE
CASE POS id-gap
## E.1 第 I 类
| **1** | 甲 |
| **3** | 丙（原 #2 已并入本行，空号保留不复用）|
## E.2 第 II 类
## E.3 第 III 类
## E.4 第 IV 类
## E.5
ENDCASE

CASE POS id-dup
## E.1 第 I 类
| **1** | 甲 |
| **2** | 乙 |
## E.2 第 II 类
## E.3 第 III 类
## E.4 第 IV 类
## E.5
ENDCASE
CASE POS id-orphan
## E.1 第 I 类
| **1** | 甲 |
| **1a** | 甲的插入项 |
| **2** | 乙 |
## E.2 第 II 类
## E.3 第 III 类
## E.4 第 IV 类
## E.5
ENDCASE
CASE POS link-dangling
### 11.2 甲
见 §11.2
ENDCASE

# ============ fkey：跨表 typed 外键完整 ============
CASE NEG fkey
## E.1 第 I 类
| **1** | 甲 ｜复合 → E.4 #99 |
## E.2 第 II 类
## E.3 第 III 类
## E.4 第 IV 类
| **1** | 人工项 | 复合 ← E.1 #1 |
## E.5
ENDCASE
CASE NEG fkey
## E.1 第 I 类
| **1** | 甲 |
## E.2 第 II 类
## E.3 第 III 类
## E.4 第 IV 类
| **1** | 人工项 | 复合 ← E.1 #88 |
## E.5
ENDCASE
CASE POS fkey
## E.1 第 I 类
| **1** | 甲 ｜复合 → E.4 #1 |
## E.2 第 II 类
## E.3 第 III 类
## E.4 第 IV 类
| **1** | 人工项 | 复合 ← E.1 #1 |
## E.5
ENDCASE

# ============ authdef：权威定义唯一 ============
CASE NEG authdef
### 11.2 归属域 〔权威定义〕`domain_level`
### 11.9 又一处 〔权威定义〕`domain_level`
ENDCASE
CASE NEG authdef
### 11.2 归属域（无标记）
ENDCASE
CASE POS authdef
### 11.2 归属域 〔权威定义〕`domain_level`
### 11.4a 可见性 〔权威定义〕`visibility`
### 11.3 生命周期 〔权威定义〕生命周期
### 10.1 记忆类型 〔权威定义〕记忆类型
### 22.4 凭据分级 〔权威定义〕凭据分级
ENDCASE

# ============ head-dup：heading 编号唯一 ============
CASE NEG head-dup
### 11.2 归属域四级
### 11.2 又一个同号章节
ENDCASE
CASE POS head-dup
### 11.2 归属域四级
### 11.3 生命周期
ENDCASE

# ============ fence：代码围栏闭合 ============
CASE NEG fence
正文
```python
x = 1
ENDCASE
CASE POS fence
正文
```python
x = 1
```
ENDCASE

# ============ link-ambig / link-dangling：内部链接唯一解析 ============
CASE NEG link-ambig
### 11.2 甲
### 11.2 乙
见 §11.2
ENDCASE
CASE NEG link-dangling
### 11.2 甲
见 §99.9
ENDCASE
CASE POS link-ambig
### 11.2 甲
见 §11.2
ENDCASE

# ============ table：孤立表行 / 缺分隔行 ============
CASE NEG table
正文
| 孤立的一行 | 值 |
正文
ENDCASE
CASE POS table
| 头 | 值 |
|---|---|
| 行 | 值 |
ENDCASE

# ============ symbol-context：受控符号出现在不属于它的语境 ============
CASE NEG symbol-context
### 19.2 出站闸层级
本节只谈转述与收件面，此处却出现了 L2。
ENDCASE
CASE POS symbol-context
### 19.2 出站闸层级
本节只谈转述与收件面，此处写 G2 是对的。
ENDCASE
CASE POS symbol-context
### 5.3 风险级别
本节按风险定级，L2 属中风险。
ENDCASE

# ============ typed-ref：§N 后跟的符号须属该节符号族 ============
CASE NEG typed-symbol
§19.2 L2 应为 G 系符号
ENDCASE
CASE POS typed-symbol
§19.2 G2 是正确的出站闸符号
ENDCASE

# ⛔ head-count（标题所载数量 vs 表行数）曾实现后【撤回】:6 命中全为误报
#    —— 章节号「12.4 条目类型」被读成「4 条」、「五要素中四项」两个数都不是行数、
#    「只有一条合法路径」是行文而非计数。判定「这个数指的是不是这张表」属语义，机器分不出。
#    该项仍归 E.3 #36 人审。见 E.3 #36b。
