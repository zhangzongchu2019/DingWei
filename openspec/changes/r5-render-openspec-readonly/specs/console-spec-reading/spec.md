# Console 只读渲染 OpenSpec 增量

## MODIFIED Requirements

### Requirement: CSR-001 规格读取与内容定位
Console MUST 支持按受控仓库托管提供方/实例、仓库稳定标识、完整 commit SHA 和仓内路径读取 OpenSpec Markdown，持续显示完整来源坐标并提供对应不可变来源链接。使用 ref 选择版本时 MUST 先解析为完整 commit，文档、目录与内部跳转始终绑定该 commit。旧数据库规格只通过 R11 历史边界读取，其身份语义不能被当作 Git 授权。

#### Scenario: CSR-S001 旧数据库规格读取边界
- **WHEN** 用户打开仍获权的旧规格链接
- **THEN** 系统按历史读取边界展示旧正文，不将旧编号解释为 OpenSpec 仓库坐标

#### Scenario: CSR-S004 分支移动不混读
- **WHEN** 用户在 commit A 打开规格后该分支推进到 B，并继续打开同页内部文档
- **THEN** 页面与内部链接仍读取 A，显式切换版本后才读取 B，并更新可见坐标

#### Scenario: CSR-S005 错误仓库或不存在的 commit
- **WHEN** 用户提供仓库外的 commit、无权限仓库或不存在的版本
- **THEN** 系统不返回文档内容，不退回默认分支、当前文件或另一仓库同路径内容

### Requirement: CSR-002 Markdown 安全呈现
新 OpenSpec 阅读区 MUST 把仓库文本视为不可信输入，转义或清除原始 HTML、事件属性及可执行元素；不得执行脚本、嵌入 frame/object、提交表单或自动抓取远程资源。链接仅允许经过校验的 HTTPS 外链和经 CSR-004 校验的同仓同 commit 文档链接；拒绝危险 scheme、协议相对 URL、凭据 URL、混淆编码及属性突破。既有其它页面策略不因此被放宽。

#### Scenario: CSR-S002 原始 HTML 转义
- **WHEN** 文档含 script、事件属性、SVG/HTML 可执行片段或代码围栏里的伪标签
- **THEN** 页面不执行其代码、事件或请求，代码围栏内容仍按文本呈现

#### Scenario: CSR-S003 普通超链接
- **WHEN** 新阅读区包含普通 HTTPS 外链
- **THEN** 仅经用户点击才打开，经独立窗口打开时隔离 opener 并不发送 referrer；不把外链当成服务器抓取指令

#### Scenario: CSR-S006 恶意链接
- **WHEN** 文档含 javascript/data/file/vbscript scheme、HTTP、协议相对地址、userinfo 或编码混淆链接
- **THEN** 不产生可触发的跳转/加载元素，可见文字仍安全显示

## ADDED Requirements

### Requirement: CSR-003 仓库权限继承
每次返回目录、正文、搜索摘要或缓存结果前，服务端 MUST 验证当前主体对该仓库的有效读取权限；首月 Console 用户与按提供方用户的绑定由管理员维护，平台使用只读令牌逐主体核权，绑定本身不授予仓库权限；平台角色、服务账号权限或他人缓存均不能代替仓库授权。失权、身份映射不明或权限服务不可用时 MUST 拒绝返回内容；缓存不得在主体、仓库、commit 或路径间串用。R5 不派送正文给 Agent，后续派审不得因选中 Agent 而扩大其仓库权限。

#### Scenario: CSR-S007 服务账号有权但当前用户无权
- **WHEN** 后端凭据可读某仓库而当前用户没有该仓库读取权限，或管理员已维护身份绑定但该主体仓库 read 仍未获权（两格独立执行）
- **THEN** 用户不能取得目录、正文、摘要或他人缓存内容

#### Scenario: CSR-S008 缓存后撤权或权限服务失败
- **WHEN** 文档曾被缓存，随后当前主体被撤权、停用或权限检查不可用
- **THEN** 新读取请求被拒绝，页面不把旧缓存当作本次成功结果，也不回显缓存摘要

### Requirement: CSR-004 仓库坐标与路径校验
读取和内部链接 MUST 限于受控仓库托管提供方实例、已映射仓库、固定 commit 与 `openspec/` 内允许的 Markdown 普通文件；服务端 MUST 校验目录/API 响应实际坐标。拒绝绝对路径、路径穿越、分隔符/重复解码混淆、NUL、symlink、gitlink/submodule 及任意用户指定 host；重定向不能绕过同等校验。内部链接不得继承原目录之外的权限。代码由 context.md 中的安全 HTTPS 链接前往仓库托管平台查看，平台 MUST NOT 抓取或渲染 openspec/ 外的源码；该源码链接仍绑定同仓同 commit，用户主动打开且不传凭据。

#### Scenario: CSR-S009 路径穿越与跨 host
- **WHEN** 请求路径或 Markdown 链接使用 ../、编码分隔符、双重编码、绝对路径或任意外部 API 地址逃离允许范围，或 context.md 链接到 openspec/ 外源码（各格独立执行）
- **THEN** 拒绝读取目标，不访问仓库外路径或其它 host；合法归一化后仍在范围内的相对文档可在同 commit 打开；源码的安全 HTTPS 提供方链接仅由用户主动前往平台查看，服务器不代抓源码

#### Scenario: CSR-S010 链接文件与错误响应坐标
- **WHEN** tree 项是符号链接、子模块，或 API 返回不匹配的提供方/实例/仓库/commit/path（含迁移后同名仓库）
- **THEN** 拒绝呈现，不跟随到其它目录、提供方、仓库或版本，不沿用旧身份映射/缓存授权

### Requirement: CSR-005 图片与资源不自动加载
OpenSpec 阅读区 MUST 在未有已审批准入策略前按 R5-PM01 已批 A 仅显示图片替代文本，不生成任何图片网络加载（含 HTTPS 远程、自有文件、仓库相对图片和 HTML/CSS 图片）。远程图片禁止由浏览器、预取器或服务端代理自动抓取，不能通过图片泄露主体、referrer 或仓库阅读行为。

#### Scenario: CSR-S011 远程图片和跟踪资源
- **WHEN** 文档包含 Markdown 图片、HTML img/picture、CSS URL 或外部跟踪 URL
- **THEN** 渲染与滚动均不产生对应网络请求，只展示安全的替代文字

### Requirement: CSR-006 有界读取与明确失败
系统 MUST 将单文件上限固定为 2 MiB（2,097,152 字节），等于上限可完整读取，超过上限不得返回截断正文。合法 70 KB 单文档 MUST 完整呈现（验收同时覆盖 70 KiB），不得经 64 KiB 消息通道截断。超过单文件上限 MUST 显示「文件过大，请在仓库托管平台查看」并附同仓同 commit 文件的安全来源链接，不以文件服务转传。tree、分页、并发与超时仍须在实施前核定有界值。超时、编码错误、限流或取件失败 MUST 显示明确原因及请求坐标，不把旧成功缓存或其它版本作为本次成功。

#### Scenario: CSR-S012 大规格完整读取
- **WHEN** 获权用户分别读取合法 70 KB、70 KiB 及恰好 2,097,152 字节的 UTF-8 OpenSpec 文档（三格独立执行）
- **THEN** 全文字节与原 blob 一致，末尾标记存在，页面 commit 正确且无截断

#### Scenario: CSR-S013 超限和失败
- **WHEN** 单文件为 2,097,153 字节，或 tree 超限、分页未完成、UTF-8 无效、提供方超时/限流（各格独立执行）
- **THEN** 单文件超限显示「文件过大，请在仓库托管平台查看」及同仓同 commit 来源链接；其它格显示可区分中文原因，均不显示截断正文或报告完整成功


### Requirement: CSR-007 只读原文与状态边界
Console MUST 只读展示当前规格、context、变更和归档文档，保持需求/场景/任务编号及 tasks.md 勾选原意；不得从本阅读区编辑、勾选写回 Git 或旧 Spec，也不得由 tasks 全勾选、归档目录或结构校验通过推断业务验收、确认或发布完成。

#### Scenario: CSR-S014 全勾选与 archive
- **WHEN** 用户浏览 tasks 全勾选或已在 archive 目录的变更
- **THEN** 页面只呈现原文状态，不自动产生通过、确认、发布或任务完成记录

#### Scenario: CSR-S015 查看文件类型与只读操作
- **WHEN** 用户在同 commit 下查看 spec、context、proposal、design、tasks 或归档文档
- **THEN** 页面保持原文编号且不提供正文保存/勾选写回操作，不向仓库或旧 Spec 发起写请求
