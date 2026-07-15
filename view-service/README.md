# DingWei View Service

View Service 已从 `transport-v2` 毕业并入 DingWei 权威仓。它通过 Hub 内网接口读取结构化事件、转发页面输入，并提供折叠式会话页面。

## 本地验证

```bash
cd view-service
python3 -m unittest -v test_view_service.py
python3 -m http.server 19312 --bind 127.0.0.1
```

浏览器回归使用 Playwright 1.45.3 Docker 镜像，覆盖桌面与移动视口、等候区接纳转移、回合分组、工具折叠和横向溢出：

```bash
mkdir -p /tmp/renderer-pw
docker run --rm --network host \
  -e RENDERER_URL=http://127.0.0.1:19312/renderer.html \
  -e NODE_PATH=/deps \
  -v /home/ai-dev/.npm/_npx/b86aa1bd01ce2ae2/node_modules:/deps:ro \
  -v "$PWD":/work:ro \
  -v /tmp/renderer-pw:/tmp/renderer-pw \
  -w /work mcr.microsoft.com/playwright:v1.45.3-jammy \
  node renderer-regression.js
```

## 生产部署映射

- 仓库 `dingwei/view-service/` 同步到 `/home/ai-dev/dingwei-viewservice/`。
- `VS_RENDERER=/home/ai-dev/dingwei-viewservice/renderer.html`。
- `VS_STATE_FILE=/home/ai-dev/dingwei-viewservice/view-state.json`；文件原子替换并保持 `0600`，用于 View Service 重启后复用 viewer/token。
- 保持 `VS_HUB_INTERNAL=http://127.0.0.1:8792` 和 `VS_HUB_PUBLIC=http://127.0.0.1:8791`，除非生产端口另有配置。
- 同步代码和 renderer 后执行 `systemctl restart dingwei-viewservice`。renderer 每请求读取，也支持先热替换再重启服务。
- 验证页面仍能复用原 viewer、解锁状态和输入权，无需刷新或重新发送 `#unlock`。

本版本不持久化 Hub 自身的写授权。Hub 重启后页面事件流会重连并重新注册 viewer，但用户仍需用新码执行一次 `#unlock`。
