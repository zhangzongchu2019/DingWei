## ADDED Requirements

### Requirement: ESG-001 无配对稳定身份
服务 SHALL 支持经验证主体显式选择不创建 AG；新地址为短名加 EX 稳定随机标识，既有 AG 地址不变，回调不得接受未绑定的选择。

#### Scenario: ESG-S001 无配对开户
- **WHEN** 验证主体以已绑定 OAuth state 选择不配对且功能开启
- **THEN** 创建可认证的稳定席位，不创建 AG，Console 显示已验证未编组，通信返回 not_permitted

### Requirement: ESG-002 权威普通组授权
平台 SHALL 统一检查有效身份与真实共享普通组或原有效 AG；镜像 AG 不独立授予普通组权限，跨租户默认拒绝，保留既有 leader_only 限制。

#### Scenario: ESG-S002 普通组互通与越组拒绝
- **WHEN** 内外或两个外部席发送消息
- **THEN** 仅有效共享组允许通信，越组请求拒绝且不泄露正文与对端

### Requirement: ESG-003 显式组与旧请求兼容
通信 SHALL 支持 direct 或指定 kind/ref 的 group 选择；无 routing 的旧请求仅选原 AG，显式失效组不得跨组借授权。

#### Scenario: ESG-S003 显式失效组
- **WHEN** 指定组已失效但双方另有有效普通组
- **THEN** 指定组请求仍拒绝，无 routing 的旧请求不自动改成普通组

### Requirement: ESG-004 路由绑定与幂等
系统 SHALL 持久绑定首次受理的路由及双方稳定身份；未投影、收件、确认按原路由重核，重放不换组；direct 普通组优先且稳定排序，普通流量不续 AG。

#### Scenario: ESG-S004 原路由失效
- **WHEN** 同 ID 重试或待投影件的原组失效而另一组有效
- **THEN** 不生成新投递、不切换组、不续原 AG

### Requirement: ESG-005 受控成员管理与回退
系统 SHALL 只允许可信属主或平台管理员管理外部入组，保留普通组管理限制；功能开关关闭仅阻止新增，不删除现有成员、不重建 AG。

#### Scenario: ESG-S005 外部自拉人
- **WHEN** 外部 MCP 席尝试自行入组或邀请成员
- **THEN** 请求拒绝且成员关系不变

### Requirement: ESG-006 授权与身份分离
系统 SHALL 允许无组身份进行工具发现，但所有通信工具均需独立授权；无授权不返回正文、在线对端或租约。令牌过期停止认证，普通组与 AG 的生命周期彼此独立。

#### Scenario: ESG-S006 普通组离开与令牌过期
- **WHEN** 外部席离开普通组但仍有有效 AG，或其令牌到期
- **THEN** 前者仅按仍有效 AG 通信且不重建配对，后者认证失败且成员行保留
