# DingWei 产品规划路线图 v0.1

> DingWei = 飞书 + AI-CLI/Agent 协作平台。中央 Hub 做可靠路由 + 智能总控,各机器一个 sessionHelper 托管 AI CLI,网页终端看/控,统一发信协作。
> 本文档是**大方向规划**,随交付滚动更新。所有文档以代码为准。

---

## 一、现状(已交付并上线)
- **接入**:sessionHelper(Linux+macOS)托管 AI CLI(claude/codex/opencode/claude-deepseek),PTY 模式;
- **看/控**:网页终端 `/view`(resize 铺满 / #unlock 打字 / 自动重连保活);
- **发现/协作**:在线清单文件(心跳新鲜度)+ 统一发信 `send.py`(owner 范围跨 key 路由);
- **智能总控**:L1 规则过滤 + L2 deepseek 分诊/编排(decompose/aggregate)+ 任务队列保交付 + 看板(已上线验证);
- **下发**:provision 客户端(update_self/skill/mcp,sha256+鉴权+回滚,2.1.0)+ **发送方✅已上线**(/admin/provision:三种目标+灰度+上传自动sha256+审计);
- **弹性**:CLI 忙时缓存 + 空闲逐条灌;CLI 更新退出自动重生;
- **工程规范**:版本号 semver(每交付 bump)、推送前敏感信息门禁、真名/claude 合规。

---

## 二、路线图(六大方向)

### 方向 A · 规模化:支持更高在线用户量
- **目标**:从单机数十会话 → 数百/上千会话稳定在线。
- **抓手**:
  1. **会话注册中心外置**:当前 `sessionClients` 在单实例内存 → 抽到共享存储(Redis/DB),支持**多 DingWei 实例**水平扩;
  2. **跨实例路由**:实例间转发信封(消息总线),owner 范围路由跨实例生效;
  3. **连接层优化**:WS 心跳/背压/连接数上限调优;SQLite → WAL 调优或按需换更高并发存储;
  4. **会话分片**:按 owner/key 分片到不同实例。
- **依赖**:总控队列已具备削峰;先做注册中心外置(单点瓶颈)。

### 方向 B · 高并行:更高并行度的 LLM 调用(总控 L2 + 直连模式)
- **目标**:L2 分诊 / 直连 LLM 的吞吐随负载线性扩。
- **抓手**:
  1. **L2 worker 池动态扩缩**(现 N=4 固定 → 按队列深度自适应);
  2. **LLM 连接池 + 多 key/多 provider 轮询**(deepseek/glm/qwen…分摊限额);
  3. **限流/重试/降级**统一(429/超时);批处理可选;
  4. 队列削峰已有,重点扩 worker + LLM 侧并发。

### 方向 C · 易扩展:更方便的功能扩展方案
- **目标**:加新能力不改核心、可热插拔。
- **抓手**:
  1. **数据化/可配置**继续推进(L1 决策表已数据化 → 命令、路由规则、总控策略都走配置);
  2. **扩展点/插件接口**:定义清晰的 hook(入站/路由/回执/总控 action);
  3. **扩展开发 SDK + 示例**,降低二次开发门槛。

### 方向 D · skill/MCP 快捷安装
- **目标**:任意 skill/MCP 一键装到目标会话。
- **抓手**:
  1. **provision 发送方**(✅已上线)—— install_skill/install_mcp 的服务端入口(/admin/provision);
  2. **skill/MCP 注册中心/市场**:列可用项、版本、一键下发;
  3. 结合**自助 onboarding**(飞书 `#申请` 发 key + 自动装基础 skill)。

### 方向 E · Windows 版 sessionHelper
- **目标**:Windows 机器也能接入。
- **抓手**:
  1. **首选 WSL2**:零改代码(和 Linux 一致),文档化;
  2. **原生 Windows**:PTY 用 pywinpty/ConPTY 替代 pexpect(无 tmux);桥接/清单/发信/provision/忙缓存(纯 Python)可移植;
  3. provision 的 update_self 重启在 Windows 用计划任务/服务;
- **依赖**:OS 识别已有(detect_os 返回 windows);先 WSL2 兜底,再原生。

### 方向 F · 各类 AI 编程 GUI 客户端(Linux/Mac/Windows)
- **目标**:不止 CLI,支持 GUI 类 AI 编程客户端接入协作网。
- **抓手**:
  1. 调研主流 GUI(VS Code 系扩展 / Cursor 等)的接入方式;
  2. **GUI 伴生接入**:一个轻量伴生进程/扩展,把 GUI 会话接入 DingWei(收发消息、上报在线、可选看/控);
  3. 跨平台伴生程序(Electron/Tauri 或各 IDE 扩展);
  4. 协议复用现有 WS/信封体系,GUI 侧适配注入/采集。
- **规模**:较大,建议独立立项分期。

---

## 三、既有 backlog(并入规划)
- 🐛 **[bug·待定位] macOS 下 launchd 后台运行时 CLI 不启动**(2.1.0,/view 空白;前台正常;Linux 不受影响)——诊断线索见 [known-issue-macos-launchd-cli.md](known-issue-macos-launchd-cli.md);当前以"前台 nohup 后台化 + 输出重定向"缓解;
- is3/本机会话升级到最新客户端(2.1.0,+改名 zzc-*);
- 远程成员部署包更新到新方案 + 发放(亚洲+美洲机);
- 灰度 8792 下线;
- 自助申请 key(飞书 `#申请` + 审批);
- L2 triage prompt 调精准(现分解偏激进);
- 文档整合(产品规范×1 / 设计×1,以代码为准)。

---

## 四、优先级建议(滚动调整)
1. **近期收口**:provision 发送方(补下发闭环)→ is3/远程成员客户端升级 → 灰度下线 → 文档整合;
2. **中期能力**:方向 D(skill/mcp 市场+自助)、方向 B(LLM 并行扩)、方向 C(扩展 SDK);
3. **中期平台**:方向 E(Windows,先 WSL2)、方向 A(规模化,注册中心外置);
4. **较大独立项**:方向 F(GUI 客户端接入)。

> 每项落地遵循:规格进 docs/ → 团队实现(developer)→ 评审(manager)→ 双平台测试(tester)→ 合规终检 → 合并 → 灰度→全量。
