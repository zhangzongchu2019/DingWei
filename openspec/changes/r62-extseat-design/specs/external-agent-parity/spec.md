# external-agent-parity Specification

## Purpose

本变更把外部 MCP Agent 从强制临时配对的受限通信路径扩展为可选配对与普通编组同权的设计目标，目的是落实业主认可的独立生命周期、可控摘要抄送和稳定身份。涉及外部席位设计、组授权与展示，不影响本单之外的产品代码、已冻结 TTL 或部署状态。

## ADDED Requirements

### Requirement: EAP-001 External lifetime policy

External temporary groups and Hub inbox/outbox messages SHALL have a seven-day lifetime. External tokens SHALL retain their current fourteen-day sliding lifetime. Temporary-group polling MUST NOT extend its lifetime, and extending a group or token MUST NOT extend an individual message.

#### Scenario: Independent expiry windows

- **WHEN** an external Agent has a temporary AG, an accepted message, and a valid token
- **THEN** the AG and message expire after seven days under their own clocks, while token expiry remains fourteen days; polling alone does not renew the AG

#### Scenario: Ordinary groups do not inherit AG expiry

- **WHEN** an external Agent has joined an ordinary `np_group` and its bootstrap AG expires
- **THEN** the ordinary membership remains active until ordinary-group removal, Agent revocation, or the existing group lifecycle removes it

### Requirement: EAP-002 Stable address with Unicode display identity

The system SHALL display the verified Feishu Unicode name separately from the canonical ASCII Agent address. Display names MUST NOT be used as identity, uniqueness, authorization, or routing keys. The system MUST show a disambiguating canonical address for duplicate display names and MUST NOT require department data.

#### Scenario: Duplicate Unicode names

- **WHEN** two active Agents have the same Unicode display name
- **THEN** each list and group view shows a distinct canonical address and selects by that address or Agent ID

#### Scenario: Unicode display does not change addressing

- **WHEN** a Feishu display name contains non-ASCII characters or later changes
- **THEN** the existing ASCII address and stored group/identity references remain unchanged

#### Scenario: Cross-tenant display

- **WHEN** external Agents from two explicitly admitted tenants share a group and have the same display name
- **THEN** each is shown with an administrator-assigned tenant label and canonical address; raw tenant keys, open IDs, and union IDs are not displayed

### Requirement: EAP-003 Owner-controlled summary copy

系统 SHALL 为内外 Agent 提供每席飞书摘要抄送偏好。内部缺省 MUST 关，外部缺省 MUST 开；发送仍须唯一可信收件人，缺省不得覆盖显式偏好。verified owner MAY 用严格命令开关；仅一个可控 Agent 时可省略 selector，多 Agent 歧义 MUST 不改设置。环境 gate MUST NOT 强开内部缺省或覆盖 opt-out；摘要 MUST NOT 包含全文或凭据。

#### Scenario: Internal default off

- **WHEN** 内部 Agent 没有显式抄送偏好，即使 owner 有唯一 verified 飞书映射
- **THEN** 不生成抄送；owner 显式开启后才按收件人检查发送，重复开关幂等

#### Scenario: External default on

- **WHEN** 外部 Agent 没有显式偏好且有唯一可信飞书收件人
- **THEN** 默认生成摘要抄送；已有显式 off 时不生成，已存偏好不因缺省改变被覆盖

#### Scenario: Single Agent command

- **WHEN** a verified owner controls exactly one eligible Agent and sends the copy off command without an Agent selector
- **THEN** only that Agent's copy preference is disabled and the command is idempotent

#### Scenario: Multiple Agent ambiguity

- **WHEN** a verified owner controls multiple eligible Agents and omits the selector
- **THEN** the system requests an explicit canonical Agent selector and changes no preference

#### Scenario: Internal and external copy destinations

- **WHEN** a copy is enabled for an internal or external Agent and the owner has exactly one verified Feishu recipient mapping
- **THEN** a summary is delivered to that verified account without changing the primary Agent message result

#### Scenario: No verified Feishu destination

- **WHEN** an owner has no verified Feishu mapping or multiple unresolved mappings
- **THEN** the system does not guess a recipient and reports that copy is unavailable or requires account selection

### Requirement: EAP-004 External ordinary-group parity

已准入外部 Agent SHALL 可由有权 owner/admin 加入多个普通组，入组 MUST 保留原临时配对且不续其期限。共同身份/租户检查通过后，无显式组直发 SHALL 在共享有效真实普通组或双方恰为有效 AG 原成员时放行；两路皆有时优先普通组，多个普通组按稳定引用固定序选取并记录。显式组请求 MUST 只核该组，不跨组回退。临时组镜像 MUST NOT 作为独立普通组授权；外部席 MUST NOT 自行入组或扩展配对目标。

#### Scenario: Bootstrap retained on ordinary-group join

- **WHEN** owner 把已有有效临时配对的外部 Agent 加入首个或后续普通组
- **THEN** 普通 membership 新增，原 AG/目标/期限保持不变，两类授权并存

#### Scenario: Group-only communication

- **WHEN** 双方既不共享真实普通组，也不是同一有效临时 AG 的两个原成员
- **THEN** 拒绝通信；临时目标所在组的第三人不因该配对获得权限

#### Scenario: Explicit group cannot borrow another grant

- **WHEN** 请求显式绑定的 AG/普通组已失效，但双方另有有效组授权
- **THEN** 拒绝该绑定请求，不将旧件改投另一组或换目标；新的无显式组直发可独立核验其合法路径

#### Scenario: Deterministic direct route with both grants

- **WHEN** 双方同时共享多个真实普通组并有有效临时配对，发起无显式组的稳定身份直发
- **THEN** 按真实普通组稳定引用的固定序选择并记录唯一授权路径，原目标不变且不刷新临时 AG 时钟

#### Scenario: Expired AG mirror is not ordinary authorization

- **WHEN** 临时 AG 已过期/解散而其可达性镜像尚有成员行，双方没有其它真实普通组
- **THEN** 镜像不能放行；双方若共享另一个真实有效普通组，则该普通组路径独立可用

#### Scenario: Leave final ordinary group

- **WHEN** 外部 Agent 离开一个或最后一个普通组
- **THEN** 只移除对应 membership；仍有效原配对仅对原目标继续可用，无有效路径则拒绝，不重建或重开失效 AG

#### Scenario: Token expiry preserves membership

- **WHEN** an external Agent token expires
- **THEN** MCP authentication stops, but the Agent identity and ordinary group memberships remain; same-principal reauthentication resumes the existing identity

#### Scenario: AG expiry leaves ordinary membership intact

- **WHEN** 保留的临时 AG 到期或解散，外部 Agent 仍有真实普通组 membership
- **THEN** 只关闭 AG 授权，普通组通信按其权限独立判断；普通组流量不刷新 AG，轮询不续期

#### Scenario: Tenant not admitted

- **WHEN** an external principal belongs to a tenant outside the explicit tenant-admission policy
- **THEN** it cannot be added to a group, and a matching display name cannot bypass admission

### Requirement: EAP-005 Bounded Hub history per external Agent

The Hub SHALL retain no more than 500 external-Agent message records per external Agent across inbound and outbound directions. It MUST NOT prune in-flight records. When 500 active records prevent safe pruning, the Hub MUST return a visible capacity rejection. Send idempotency MUST remain valid for the full seven-day message lifetime even if a terminal body record is pruned.

#### Scenario: Prune oldest terminal message

- **WHEN** an external Agent has 500 retained records and at least one terminal record is eligible for pruning
- **THEN** enqueue prunes the oldest eligible terminal record and retains the new message without exceeding 500

#### Scenario: All records are in flight

- **WHEN** all 500 retained records are active or leased
- **THEN** the next message is rejected visibly and no active record is deleted

#### Scenario: Retry after body pruning

- **WHEN** a client retries the same client message ID within seven days after its terminal body record was pruned
- **THEN** the retained idempotency tombstone returns the original outcome and does not create a second delivery

### Requirement: EAP-006 File upload remains investigation-only

This change SHALL document the feasibility, permission, size, storage, retention, ACL, and abuse-control requirements for an MCP file-upload capability. It MUST NOT add an upload tool, endpoint, file ticket, or token scope.

#### Scenario: Design validation

- **WHEN** this change is reviewed or validated
- **THEN** only the feasibility investigation is present and all upload implementation tasks remain outside this change

### Requirement: EAP-007 Optional temporary pairing onboarding

飞书验证和租户准入通过后，系统 SHALL 允许不指定目标且不建临时配对的开户，仍分配稳定身份、不造 AG 或默认目标。无 AG 且无普通组时，MCP通信工具 MUST 以既有 HTTP403/协议1003、reason not_permitted 拒绝，不返他席/正文、不建消息或租约、不续组。Console MUST 显示“已验证／未编组”“临时配对未创建”。有权管理员入普通组后 SHALL 按 EAP-004 授权；身份验证和工具发现不等于通信授权。

#### Scenario: Verified but ungrouped

- **WHEN** 验证通过的新席位明确不建配对、尚未加入普通组
- **THEN** 不创建 AG/目标；send、recv、check、list、clear/ack及别名按上述 not_permitted 拒绝且不返回消息/他席信息，Console显示未编组而非已解散

#### Scenario: Later ordinary-group admission

- **WHEN** 有权管理员把无配对席位加入真实普通组
- **THEN** 同一稳定身份按组范围通信，不强制补建临时 AG；无共同授权的其它席位仍不可达

#### Scenario: No fabricated pairing for compatibility

- **WHEN** 旧版仅支持 AG 的入口无法表达无配对席位或普通组上下文
- **THEN** 明确拒绝该不支持的调用，不生成 AG0、不替用户挑目标/组、不把缺少配对当成现有 AG 已解散
