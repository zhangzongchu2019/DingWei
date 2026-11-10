# ZERO-CONSENT 合并 r56.2 新 tip 交审

CODEX_THREAD_ID=01a1bf6a-20da-7533-8984-532fd2ec65a3。依据组长常设条件接管：2026-11-09T08:32:29Z 本机工蜂远端 tip=d311864c289e0f6633afe9bd3d1dfe007fdbfc50；旧树 status --porcelain 空且 HEAD 同值；ps -eo pid,ppid,user,lstart,comm,args 以及 /proc comm/cwd/cmdline 未发现旧 owner／工作树 go test、git 或编辑进程。旧锁 stale-20261109T083303Z 保留；本线程 owner 与 last_tip 已写入新锁。新树 /data/ai-dev-data/mcp-zero-consent-01a1bf6a，旧树未改。

冻结 merge **2c403d430dd60e882cfb5ebcaaa82181324cbfd0**，tree **340dc4ab507a3eec24bc9c0a94ec7de9df794ec1**。两个父提交依次为 **d311864c289e0f6633afe9bd3d1dfe007fdbfc50** 和 **d5a6fbbca6cda0c0627342e8905372f052638b2a**；没有 rebase／改写。VERSION 仍为 0.2.3.33。

## 冲突解法

连接页 CSP 完整采用 r56.2：NewHandler 调 authorizeOriginOf，仅解析已配置 OAuth 授权 URL 的 HTTPS 精确源，交 connectPageCSPFor。保留 defaults、自动命名、单／多目标及三域隔离逻辑；撤销原连接页 FormRedirectPolicy 逻辑，并撤销 mcpgw.connect 追加 a.Redirect 的逻辑。连接页不会含回调／回环源。选择页 selection.go 不改，仅加本次已校验持久化 redirect_uri 的源。default-src／base-uri／frame-ancestors 与 Origin／CSRF 守卫不放宽。

## 真实浏览器与撤除变异

Chromium 真表单＋本地真 Hub 主接线＋真实临时 PG；飞书为 HTTPS 替身，确认页面通过浏览器点击身份确认链接后继续回调。此处改进了旧替身直接 302 的行为：r56.2 仅接受 HTTPS 授权源，真实上游授权有自己的身份确认页面；夹具不改产品响应头、不伪造 Origin、不拦截回调导航。测试只模拟上游身份，不使用真实飞书账号。

单目标无编辑输入，多目标两项单选后提交；两格均实际接收授权码、按原 PKCE 换票成功，originRejected=0。新增断言检查连接页含实际 HTTPS 飞书源且不含客户端回环源。

- mut-connect：Go overlay 撤 NewHandler 的 authorizeOriginOf 调用，退回 self；Hub rc0、浏览器 rc1，console 明确出现 form-action 拒绝，未进入飞书确认页。
- mut-selection：仅撤选择页 a.Redirect；Hub rc0、浏览器 rc1，console 明确出现 form-action 拒绝，未到客户端。

两刀都有实际 CSP 拒绝原文，超时只是阻断后的等待结果，未将编译／环境失败算作目标红。

## 指定门与环境

Go 官方 1.22.2，普通 uid1001，测试环境白名单、隔离 HOME／TMPDIR、GOPROXY=off、PG 严格模式。feishulogin 99/0，mcpgw 237/0；m8 全包 -p 1 为 1512/0/37；strict 29/0。canonical 完整重跑 rc0：claude／codex 各1129/1129，91文件，27Python／2shell，BUILD_TESTS_OK；命令与原文见 canonical/。37 个跳过项见 SKIPS.json，条件原文保留在 m8/run.stdout。

首轮 canonical 因系统 Python 缺少 websockets 导致四模块发现失败，尚未执行用例；换本机既有锁定依赖 venv（websockets15.0.1）重跑，不改依赖清单、不缩减测试。首轮 npx --no-install 在隔离 HOME 找不到已安装工具，改显式本机钉版 OpenSpec1.16.0 路径，最终 strict rc0。其后 canonical 的 helper 两 profile 各1129/1129通过，但长 TMPDIR 导致桌面认领子进程失败；实读 socket 路径124字符。改 TMPDIR=/tmp 后，同一10格认领测试全部通过（shortpath-confirmation），再完整重跑 canonical。所有失败原文在 superseded-*，最终读数仅取 canonical/。

派单后核 rulings dcfd24ef、tasks 7afbfebc、main fce2ede8，与首轮相同，无本单规格漂移。只经 Git 交付；证据未复制 HOME、env、CA 私钥、数据库或凭据。未部署／重启／改线上配置，未操作线上席位，不启动真实推理；桌面未测，不自验收。
