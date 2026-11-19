## Why

外部MCP客户端用未带bearer的initialize、tools/list或ping探测资源服务器，通过401和WWW-Authenticate发现OAuth入口。当前网关却先按版本和私有头解码，03-26、11-25和缺头请求在鉴权前得到400，导致客户端无法触发授权。本变更只修复鉴权次序和初始化版本协商，保留已鉴权的Origin、参数、权限和业务边界。

## What Changes

- /mcp每请求先鉴权，失败不读body、不按协议分流，返回既有401挑战。
- 标准initialize非空未知版本协商到完整支持的2025-06-18；不宣称实现03旧batch或11新tasks。
- 无协议头且无私有版本依据时按2025-03-26识别；非initialize因该版本尚未支持而返回明确Unsupported version，不能误落私有HeaderMismatch。
- 只接收documented clientInfo可选description/websiteUrl/icons，不使用或外取其URL。

## 需求来源

组长S-MCP-AUTH-FIRST，2026-11-19T03:01:23Z，入站dw-dlv_a17e4b9ea5895bdcd9cdb4ddf8eb8476；main206d41b2。依据用户提供的Antigravity诊断，不冒称本席连过dw123或跑过真实Antigravity。

## 待确认假设

无新增业务假设。最小技术选择A-AUTH-01已向组长报告：三版本请求协商到既有06-18，不自动声明三版本完整codec。

## 待 PM 决策

无新增PM项。发布路线、构建件和上线授权由组长另定。
