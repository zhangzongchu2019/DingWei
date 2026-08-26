# 已知问题（已解决）：macOS 首次部署 CLI 不启动 / /view 空白

> 状态：**已定位并解决**（2026-07-09）。版本：sessionHelper 2.1.0。Linux 不受影响。

## ✅ 根因与解决（结论）
**真正根因**：claude 首次在某目录启动会弹"**信任此文件夹**"安全提示；sessionHelper 用 pexpect（非交互）起 claude，**应答不了弹窗 → claude 秒退（带 `--dangerously-skip-permissions` 时 exit 1）→ /view 空白、日志无任何 `[cli]`**。美洲机（Linux）正常是因为其工作目录早已信任过。
**解决**：首次手动信任 `SH_CLI_CWD` 目录一次（`cd ~/dingwei-work && claude-deepseek --dangerously-skip-permissions` → Enter 信任 → /exit）。信任记入 `~/.claude-deepseek`，之后 sessionHelper 起 claude 不再弹。
**叠加的三个 launchd 环境坑（已随 run.sh/install 脚本修复）**：① `install-launchd.sh` run.sh 路径 `../../`→`../sessionhelper/`；② launchd 无 `~/.local/bin` → run.sh 自补 PATH；③ launchd 无 `TERM`/`LANG` → run.sh 自补。
> 下方为定位过程中的排查记录（保留备查）。

## 症状
macOS 上用 **launchd 后台**运行 sessionHelper（2.1.0）时：
- sessionHelper 正常连上 DingWei（日志有 `connected ... version=2.1.0`）、收到 skill/清单、keepalive 正常；
- 但 **CLI（claude-deepseek）始终不启动** → 网页终端 `/view` 一片空白；
- 日志里**没有任何 `[cli]` 行**（既无成功、也无 `[cli] start failed`）、**没有 claude 进程**。
- **前台运行（用户交互 shell）正常**（CLI 能起）。
- **Linux（美洲机 2.1.0）完全正常**（8 个 CLI 在跑）——macOS 专属。

## 已排除（都不是根因）
1. `install-launchd.sh` 的 run.sh 路径 bug（`../../run.sh`→`../sessionhelper/run.sh`）——**已修**；
2. PATH 缺 `~/.local/bin`——run.sh 已自补 `~/.local/bin:/opt/homebrew/bin:/usr/local/bin`；
3. 缺 `TERM`/`LANG`——run.sh 已自补 `TERM=xterm-256color`/`LANG`；
4. node 找不到——最小环境下 `claude-deepseek --version` 输出 `2.1.170`，**CLI 本身正常**；
5. macOS fork 安全（ObjC）——加 `OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES` **无效**，排除。

## 已知代码事实
- CLI 起动在 `app.py:terminal_loop`（gather 中）→ 条件满足则 `asyncio.create_task(asyncio.to_thread(self.adapter.start))`（app.py:606-612）；
- adapter = `CLIAdapter`（mode=cli），**有** `next_terminal_chunk`，所以不该卡在 line 608 的 `await asyncio.Future()`；
- `CLIAdapter.start`→`start_pty`→`pexpect.spawn`（cli.py:97/128/134），失败会打 `[cli] start failed`（未见）；
- `to_thread(adapter.start)` 自 6677bb9（PTY 原型）就有，非 2.1.0 新增；2.1.0 新增的是 provision（含 `import urllib.request`）。

## 下一步诊断（未做）
1. **直接 pexpect 复现**：用 cli.py 同参数 `pexpect.spawn('claude-deepseek', [], ..., dimensions=(40,140))`，看交互式 claude 能否起、`isalive()`、输出——一刀切开"pexpect/claude 问题"还是"sessionHelper 流程问题"；
2. **前台运行时确认 CLI 是否真起**：`ps aux|grep claude`（运行期间）+ 等满 90s 看是否出 `[cli] start failed: CLI did not become ready`（区分"没起"vs"起了没到 ready"）；
3. 在 `terminal_loop` line 612 前后加日志，确认是否真的走到起 CLI 那步、`to_thread(start)` 是否抛异常被吞；
4. 对比 launchd vs 前台的 env 差异（`launchctl print` 看实际传给进程的环境）。

## 当前缓解方案（用户在用）
前台 + 后台化 + 输出重定向（用完整 shell env，CLI 能起）：
```bash
cd ~/dingwei
nohup env WORKPULSE_SH_CONFIG_FILE="$PWD/config-zzc-mac" ./sessionhelper/run.sh > ~/.dingwei/logs/zzc-mac.log 2>&1 &
```
代价：无 launchd 的"崩溃/开机自动重启"。

## 影响
- macOS 用户暂时**无法用 launchd 守护**（缺自动重启）；Linux/美洲机不受影响；
- 属**部署体验**问题，不影响协作/总控/provision 等功能。
