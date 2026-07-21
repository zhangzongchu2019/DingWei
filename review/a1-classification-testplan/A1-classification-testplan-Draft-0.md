# v06-A1-classification-testplan-DRAFT-0 · 日志脱敏 classification 测试方案

## §0 元信息 [必填]

| 字段 | 值 |
|---|---|
| 任务 ID | A1 (classification/脱敏) |
| 任务名称 | 日志脱敏 classification 验证（S1a/S1b） |
| 测试方案版本 | Draft-0 |
| 配对设计文档 | A1 0e148998 (review/a1-logging @ deeef45, BLOCK-1已修) + A0 97ab377 FROZEN |
| 测试负责人 | shencedeepseek |
| 设计负责人 | devof5 (Go/hub) + shencedeepseek (Python VS/helper) |
| CR | shenceclaude |
| 状态 | DRAFT |
| 最后更新 | 2026-07-21 07:00 UTC |

## §1 测试范围 [必填]

### 1.1 被测对象

A1 日志脱敏原语的 **classification 正确性**——验证三端（rs/VS/helper）日志输出：
- 敏感低熵值（解锁码/PII/secret）完全不落 hash/HMAC/明文
- 非敏感高熵 code_hash = 完整 64hex，Go≡Python 一致
- cross-language test vector 3 例逐字节相同
- component = rs/vs/helper/fsch，rs.log 文件名

### 1.2 不测什么

| 不覆盖项 | 原因 | 在哪里测 |
|---|---|---|
| 5G 轮转/buffer overflow | A1 实现测试 | A1 实现测试方案 |
| zerolog/structlog 接入正确性 | A1 实现测试 | A1 实现测试方案 |
| hub missing_message_id fail-closed (BLOCK-1) | devof5 Go 侧修 | A1 Go 侧测试 |

### 1.3 依赖前置

| 依赖 | 提供方 | 状态 |
|---|---|---|
| A0 97ab377 (component rs/vs/helper/fsch) | devof5 | FROZEN |
| A1 0e148998 (脱敏设计) | devof5 | DRAFT (BLOCK-1 修中) |
| A0 §7 3-vector | devof5 | FROZEN |

## §2 产品锚点 [必填]

| # | 验收条款 | 来源 |
|---|---|---|
| 1 | 低熵敏感值绝不落 hash/HMAC/明文，只 present/type/text_len | A0 §7 D8 |
| 2 | PII (open_id/sender_open_id) v0.6 零 hash 零 HMAC | A0 §7 S1b |
| 3 | 非敏感高熵 code_hash = 完整 sha256 64hex | A0 §7 |
| 4 | Go≡Python cross-language 3-vector 一致 | A0 §7 test vector |
| 5 | component = rs/vs/helper/fsch，rs.log | A0 §5 (97ab377) |

## §3 测试矩阵 [必填]

### 3.1 低熵敏感值：零 hash 零 HMAC

**契约引用**：A0 §7 D8 + S1b

**产品锚点**：#1/#2

| 项目 | 内容 |
|---|---|
| **Green 1.1** | 解锁码 `redact_sensitive("123456", "unlock_code")` → `{present:true, type:"unlock_code", text_len:6}`，日志中零 code_hash 零 HMAC → 追溯: A0 §7 |
| **Green 1.2** | token `redact_sensitive("sk-abc123...", "api_key")` → `{present:true, type:"api_key", text_len:N}`，零 hash → 追溯: A0 §7 |
| **Green 1.3** | PII `redact_pii("ou_abc123def")` → `{present:true, type:"open_id", text_len:12}`，零 hash 零 HMAC（v0.6 DECIDED 无）→ 追溯: A0 §7 |
| **Green 1.4** | 飞书 secret → `{present:true, type:"app_secret", text_len:N}`，零 hash → 追溯: A0 §7 |
| **Red 1.1** | 解锁码日志出现 code_hash → 分级违反（低熵不应有无盐 hash）→ baseline 失败 |
| **Red 1.2** | PII open_id 日志出现 HMAC → v0.6 无 HMAC → baseline 失败 |
| **Red 1.3** | PII open_id 日志出现明文 → 明文泄漏 → baseline 失败 |
| **Red 1.4** | token/sk- 日志出现明文 → baseline 失败 |
| **故障注入** | (a) 4 位短码 → redact 不产 hash (b) 手机号 → redact 不产 hash |
| **可观测判据** | `jq 'select(.type=="unlock_code") \| has("code_hash")' rs.log helper.log vs.log` 期望全 false；`grep -iE 'ou_[a-z0-9]+' *.log` 期望零命中 |

### 3.2 非敏感高熵：code_hash 64hex 一致性

**契约引用**：A0 §7

**产品锚点**：#3

| 项目 | 内容 |
|---|---|
| **Green 2.1** | `summarize("hello")` → `{text_len:5, code_hash:"sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"}`（完整64hex，golden #2），Go≡Python一致 → 追溯: A0 §7 |
| **Green 2.2** | `code_hash` 正则 `^[0-9a-f]{64}$`（仅非敏感高熵）→ 追溯: A0 §7 |
| **Green 2.3** | `text_len` = Unicode scalar 数（非 UTF-8 字节）→ 追溯: A0 §7 |
| **Red 2.1** | Go 与 Python 对同输入产出不同 code_hash → cross-language 一致性失败 → baseline 失败 |
| **Red 2.2** | code_hash 被截断为 16hex → baseline 失败（16hex 已作废） |
| **Red 2.3** | text_len 误用字节数（中文"你好\n世界"=13 非 5）→ baseline 失败 |
| **Red 2.4** | 非法 UTF-8 输入（`61 E4 B8 FF 41`）→ ingress 处置（**张宗楚 X/Y PENDING**: X=拒整条不投递 / Y=ingress sanitize 到合法后投递）；raw-byte code_hash 若漏检两端同 56e416ad。判据：ingress 处置（非比两端 redact——redact 不该见非法）→ 追溯: shenceclaude 裁决 (a)=fail-closed+raw-byte |
| **故障注入** | (a) 非法 UTF-8 → ingress 处置（shenceclaude 裁决(a): fail-closed+raw-byte, **X/Y 待张宗楚**） (b) NFC/NFKC 隐式归一化 → 禁止（code_hash 应不同） |
| **可观测判据** | Go/Python 各跑 3 vector→diff 输出期望零差异 |

**test vector（3 例，A0 §7 冻结，完整 64hex）：**

| 用例 | text_len(rune) | code_hash(sha256 完整 64hex) |
|------|:---:|------|
| `""` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `hello` | 5 | `2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824` |
| `你好\n世界` | 5 | `9d4fd5df825a6e1422c2390c63653794766ac7a4fd847ce0e89021de878bf382` |

### 3.3 component = rs + rs.log

**契约引用**：A0 §5 (97ab377)

**产品锚点**：#5

| 项目 | 内容 |
|---|---|
| **Green 3.1** | rs 侧日志 `component=rs` → 追溯: A0 §5 |
| **Green 3.2** | 日志文件名 `rs.log`（非 hub.log）→ 追溯: A0 §5 |
| **Green 3.3** | 枚举全集 `{rs,vs,helper,fsch}` → 追溯: A0 §5 |
| **Red 3.1** | component=hub（旧名残留）→ baseline 失败 |
| **Red 3.2** | 日志文件名 hub.log（旧名残留）→ baseline 失败 |
| **可观测判据** | `jq '.component' rs.log \| sort -u` 期望 `rs`；`ls *.log` 期望无 `hub.log` |

### 3.4 classification-aware：先分类后脱敏

**契约引用**：A0 §7 + A1 §4

**产品锚点**：#1/#2/#3 — classification 在 hashing 之前

| 项目 | 内容 |
|---|---|
| **Green 4.1** | 输入先经 `classify(content)` → 判为 `sensitive_low_entropy\|pii\|nonsensitive_highentropy` → 再走对应脱敏路径 → 追溯: A1 §4 |
| **Green 4.2** | classify 误判 nonsensitive 的内容若含低熵 → Red 挂（防御性） |
| **Red 4.1** | classify 将低熵判为高熵 → 落 hash → baseline 失败（S1a 分类门） |
| **Red 4.2** | classify 将 PII 判为普通 → 落明文 → baseline 失败（S1b PII 门） |
| **故障注入** | (a) 4 位数字串 → classify 必须判为 sensitive_low_entropy (b) 含 `ou_` 前缀 → classify 必须判为 pii |
| **可观测判据** | classification 决策日志 `jq '{input_type, classification}'` 可审计 |

## §4 安全反例 [必填]

| # | 攻击/违规场景 | 预期结果（fail-closed） | 硬门 |
|---|---|---|---|
| 1 | 解锁码出现在日志的 SHA256 hash | 拒绝——只 present/type/text_len | 🔴 |
| 2 | open_id 明文出现在日志 | 拒绝——v0.6 零 hash 零 HMAC | 🔴 |
| 3 | Go/Python code_hash 不一致 | 拒绝——cross-language vector 必一致 | 🔴 |
| 4 | code_hash 截断为 16hex | 拒绝——16hex 已作废 | 🔴 |
| 5 | classify 误判低熵为高熵 | 拒绝——落 hash = 枚举攻击面 | 🔴 |
| 6 | 非法 UTF-8 输入（`61 E4 B8 FF 41`）→ ingress 处置（X/Y PENDING 张宗楚） | 拒绝——不进 redact（shenceclaude 裁决(a): fail-closed+raw-byte） | 🔴（X/Y 待 owner 裁后 lock） |

## §5 跨切面/组合场景 [选填]

| 场景 | 步骤 | 验证点 |
|---|---|---|
| cross-language 3-vector | Go `summarize` → Python `summarize` → diff | text_len/code_hash 逐字节相同 |
| 三端脱敏一致性 | rs/VS/helper 各跑同输入 | 相同敏感→全零hash；相同高熵→64hex相同 |
| 非法 UTF-8 ingress 处置 | 输入 `61 E4 B8 FF 41` → ingress adapter 检测 → 处置（X/Y PENDING 张宗楚） | 不进 redact；raw-byte code_hash 两端同 56e416ad（若漏检）；合法 3 golden 不变 |

## §6 测试环境 [必填]

| 项目 | 配置 | 获取方式 |
|---|---|---|
| A1 设计文档 | 0e148998 | 工蜂 review/a1-logging @ deeef45 |
| A0 冻结契约 | 97ab377 | 工蜂 |
| test vector | A0 §7 3 例 | 本地 fixture |

## §6b 候选制品 manifest [必填·每次执行]

| 组件 | SHA | 备注 |
|---|---|---|
| A1 Go (hub) | TBD | devof5 |
| A1 Python (VS/helper) | TBD | shencedeepseek |
| A0 frozen | 97ab377 | ref |

## §7 MANIFEST [必填]

| # | 跳过项 | 原因 | 补测时机 |
|---|---|---|---|
| 1 | P0 rig 实证 | A1 实现未完成 | A1 实现后 |

## §8 不可测规则 [必填]

无不可测规则——所有分类/脱敏断言均可 grep/jq/diff 判定。

## §9 执行记录 [必填·验收时填写]

待实现验收后填写。

## §10 变更记录 [必填]

| 日期 | 版本 | 变更 |
|---|---|---|
| 2026-07-21 | Draft-0 | A1 classification 测试方案初始版。tester复审通过。 |
| 2026-07-21 | Draft-0u1 | CR修入：A1锚全0e148998+Green2.1 hello golden#2+非法UTF-8标PENDING+§4#6。shenceclaude PASS。 |
| 2026-07-21 | Draft-0u2 | 非法UTF-8裁决落地：shenceclaude裁决(a)=fail-closed+raw-byte+vector 61E4B8FF41。 |
| 2026-07-21 | Draft-0u3 | X-specific回退PENDING(shenceclaude:X/Y未裁)。保留ingress落点+raw-byte 56e416ad。X/Y待张宗楚。 |
