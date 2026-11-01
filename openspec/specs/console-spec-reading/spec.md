# Console 规格读取最小基线

## Purpose

本变更把 Console 规格浏览能力从页面实现中的隐含行为改为可检验的最小基线，目的是准确区分现有数据库规格读取与计划中的 Git 仓库规格读取。涉及 Console 规格页面和 Markdown 渲染模块，不影响既有版本及部署流程。

本文件基于 a8bb5e5847884b62ba01f6f48fbce3772e2d8b04 的代码；未核现网。此锚未提供 OpenSpec 仓库浏览或工蜂权限继承。

## Requirements

### Requirement: CSR-001 规格读取与内容定位
Console SHALL 按旧规格编号读取详情，显示正文、修订摘要及相关属性；旧列表和身份授权读取沿用 Console principal，旧共享读取沿用独立共享语义，不把它们冒认为 Git 仓库授权。

#### Scenario: CSR-S001 读取旧规格详情
- **WHEN** 允许访问某旧规格的用户打开该规格详情
- **THEN** 页面读取该规格正文与修订信息并呈现其可见内容

### Requirement: CSR-002 Markdown 安全呈现
现有规格页面 SHALL 经公共 Markdown 渲染器转义原始 HTML，并按该渲染器的 HTTP/HTTPS 链接和自有文件图片规则生成内容。此基线不保证已符合 OpenSpec 的仓库权限、路径或远程资源约束。

#### Scenario: CSR-S002 原始 HTML 转义
- **WHEN** 规格正文含原始 HTML 标签
- **THEN** 渲染器按文本转义该标签，不直接把原始标签作为可执行 DOM 插入

#### Scenario: CSR-S003 普通超链接
- **WHEN** 正文包含非图片的 HTTP/HTTPS Markdown 链接
- **THEN** 现有渲染器生成带 noreferrer 的独立窗口链接
