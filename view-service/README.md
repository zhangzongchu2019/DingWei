# DingWei View Service

View Service 已从 `transport-v2` 毕业并入 DingWei 权威仓。它通过 Hub 内网接口读取结构化事件、转发页面输入，并提供折叠式会话页面。

## 本地验证

```bash
cd view-service
python3 -m unittest -v test_view_service.py
python3 -m http.server 19312 --bind 127.0.0.1
```

浏览器回归使用 Playwright 1.45.3 Docker 镜像，覆盖桌面与移动视口、等候区接纳转移、回合分组、进行中工具折叠、完成后工具区移除和横向溢出：

```bash
mkdir -p /tmp/renderer-pw
docker run --rm --network host \
  -e RENDERER_URL=http://127.0.0.1:19312/renderer.html \
  -e NODE_PATH=/deps \
  -v "${PLAYWRIGHT_NODE_MODULES}:/deps:ro" \
  -v "$PWD":/work:ro \
  -v /tmp/renderer-pw:/tmp/renderer-pw \
  -w /work mcr.microsoft.com/playwright:v1.45.3-jammy \
  node renderer-regression.js
```

## View unlock v2 签名路径约束

双向 HMAC 都把 HTTP path 纳入签名原文，View Service 与 Hub 之间不得改写以下内部路径：

- Hub → VS：`/internal/control/view-page/unlock`
- VS → Hub：`/internal/view-v2/{session}/input`

Hub 按实际发送路径签名，VS 按实际收到的 path 验签；反向同理。任何中间层添加或删除前缀都会使验签 fail closed。集成时必须保持 path 原样，并用双向签名测试验证，不能通过放宽验签绕过。

状态文件路径与 Hub 地址均由 `VS_STATE_FILE`、`VS_HUB_INTERNAL`、`VS_HUB_PUBLIC` 配置；源码不内置外部主机或组织环境值。状态文件应原子替换并使用仅当前用户可读写的权限。

本版本不持久化 Hub 自身的写授权。Hub 重启后页面事件流会重连并重新注册 viewer，但用户仍需用新码执行一次 `#unlock`。
