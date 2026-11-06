# DingWei View Service

View Service 已从 `transport-v2` 毕业并入 DingWei 权威仓。它通过 Hub 内网接口读取结构化事件、转发页面输入，并提供折叠式会话页面。

## 本地验证

```bash
cd view-service
python3 -m unittest -v test_view_service.py
python3 -m http.server 19312 --bind 127.0.0.1
```

## markdown 渲染共享件（`md-render-core.js`）

`view-service/md-render-core.js` 是 markdown 渲染的**单一事实源**；
`renderer-canary-sse.html` 里带 `// DW-MD-CORE:<段名> BEGIN/END` 标记的 6 段是它的**生成副本**。

```bash
cd view-service
python3 md-render-core-sync.py --check         # 只核：漂移即 rc=1，并指名是哪一段
python3 md-render-core-sync.py                 # 刷副本（改完源必须跑，然后两份一起提交）
python3 -m unittest -v test_md_render_core.py  # 漂移闸 12 格（含 5 条“闸有牙”腿）
NODE_PATH=<playwright 所在> node md-render-core-parity.js   # 行为对账闸：共享件 vs 产品页，99 项输出逐字节相同（≈0.9s）
```

🔴 改法顺序不能反：**改 `md-render-core.js` → 跑 sync → 两份一起提交**。
不要直接改 renderer 里那 6 段——它是生成物，下次同步会覆盖你的改动。

🔴 这两条闸目前**都没有任何自动执行路径**（仓内无 CI；`build.sh` 只做 `go build` ＋ `go version`，
不跑 python / node 测试）⇒ 改完共享件**必须手动跑两条**。
漂移闸比的是**源字节**，对账闸比的是**行为输出**——🔵 源字节相同 ≠ 行为相同（遮蔽/重定义/依赖段外全局都能让两者分家），
所以两条都要跑。接线归属待组长裁（见 `runbooks/DELIVERY-MD-RENDER-CORE-0.2.1.50-qwen38maxkhovvob4.md` §七）。

浏览器回归使用 Playwright 1.45.3 Docker 镜像，覆盖桌面与移动视口、等候区接纳转移、回合分组、进行中工具折叠、完成后工具区移除和横向溢出：

```bash
mkdir -p /tmp/renderer-pw
docker run --rm --network host \
  -e RENDERER_URL=http://127.0.0.1:19312/renderer-canary-sse.html \
  -e NODE_PATH=/deps \
  -v /home/ai-dev/.npm/_npx/b86aa1bd01ce2ae2/node_modules:/deps:ro \
  -v "$PWD":/work:ro \
  -v /tmp/renderer-pw:/tmp/renderer-pw \
  -w /work mcr.microsoft.com/playwright:v1.45.3-jammy \
  node renderer-regression.js
```

## 生产部署映射

- 仓库 `dingwei/view-service/` 同步到 `/home/ai-dev/dingwei-viewservice/`。
- 生产 renderer 固定为 `/home/ai-dev/dingwei-viewservice/renderer-canary-sse.html`，不支持请求或环境变量切轨。

### View unlock v2 签名路径约束

双向 HMAC 都把 HTTP path 纳入签名原文，部署时不得在 View Service 与 Hub
之间改写以下内部路径：

- Hub → VS：`/internal/control/view-page/unlock`
- VS → Hub：`/internal/view-v2/{session}/input`

Hub 按实际发送路径签名，VS 按实际收到的 path 验签；反向同理。若中间 nginx、
sidecar 或 service mesh 添加/删除前缀，验签会 fail closed。需要增加代理层时应保持
path 原样透传，并在发布前用双向签名集成测试验证，不能通过放宽验签绕过。
- `VS_STATE_FILE=/home/ai-dev/dingwei-viewservice/view-state.json`；文件原子替换并保持 `0600`，用于 View Service 重启后复用 viewer/token。
- 保持 `VS_HUB_INTERNAL=http://127.0.0.1:8792`，除非生产内网端口另有配置。
- `VS_CANONICAL_PUBLIC_ORIGIN` 只读部署配置，填写 Hub 的规范 HTTPS 来源（与 `DW_PUBLIC_ORIGIN` 一致）；View Service 在页面头部注入它，浏览器仍把文件 GET 发到当前页面同源路径。
- 同步代码和 renderer 后执行 `systemctl restart dingwei-viewservice`。renderer 每请求读取，也支持先热替换再重启服务。
- 验证页面仍能复用原 viewer、解锁状态和输入权，无需刷新或重新发送 `#unlock`。

本版本不持久化 Hub 自身的写授权。Hub 重启后页面事件流会重连并重新注册 viewer，但用户仍需用新码执行一次 `#unlock`。
