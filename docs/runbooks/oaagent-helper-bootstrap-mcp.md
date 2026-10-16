# oaagent.lbl 手动首升执行单（含稳定 MCP 转发器，待制品审核）

本件是准备稿，不是安装授权。执行人是业主侧能登录 oaagent 宿主、具有 root 和 Docker 权限的人。本文编写时没有访问宿主、安装、重启或操作任何 Agent。真实 Linux Docker 门及首升 preflight 仍须实际执行、记录，不以本件当作通过证据。

源码基线为 r15 `b5faf73c8fe4d5c71720daf4eed5b133b8d45dbd`，本次增量位于 `review/coder/mcp-forwarder`。执行单继承已审 `review/coder/oaagent-bootstrap-runbook@1d33f757`，增加稳定 MCP 转发器及验收门。目标版本 **0.2.3.2**，旧版本预期仍为 **0.2.3.0 加 A6 补丁**。必须由 prodops 提供本次经审新包的完整 commit/tree、URL、SHA、bytes 和有效期，不能沿用 r14/r15 旧包坐标。

本件用于尚未首升的原布局。已首升的 oaagent 若仅 MCP 不可见，先在获准窗口重启 Codex或开新会话，并验收现有 factory 脚本配置；无需为此反复重启 helper。该最小恢复不自动补齐稳定转发器。自升级只替换私有槽位，不替换挂载根：旧首升布局中缺少的转发器不会因升级到 0.2.3.2 自动出现。要补根转发器，须另经审核授权的宿主修复/手动安装方案；若已有 journal/current/pending，不能直接套用本执行单覆盖或清空状态。

兼容停止门：r14/r15 首升时安装的旧 `helper_update_package.py` 白名单不认识本次包新增的根 `sessionhelper/mcp_shim.py`，会拒绝为 `manifest_invalid`。因此本包不能直接下发给旧首升布局。下文的自升级验收仅针对已手动安装本次新稳定布局的 Agent；旧布局须由 leader 另定修复或兼容制品方案，不改旧校验器、不删成员重打包来绕过此门。

## 执行前约定

- 等 leader 明确首升窗口及 Hub/Console 能力接口可用后执行。安排 **两次计划重启**：第一次在旧代码下验证状态目录保留；第二次在安装后启动新代码。均仅 `docker restart`，不重建容器。
- 在宿主同一个 root Bash 终端按顺序执行。任何非零退出、断言失败、输出不符都停下；不跳过检查、不修改 env/凭据、venv、容器入口或挂载以求通过。不要开启 `set -x`，不要输出 secret/env、完整 Docker inspect 或未经筛选的日志。
- 源码挂载根只放稳定启动器、guard 和内置出厂代码；槽位、指针、journal 在容器私有状态目录的 `update/`。实际绝对路径由 preflight 解析并记录，**本件不假称已实测**。容器可写层只保证同一容器 `restart` 保留，不保证删除/重建容器保留。
- 旧 helper 的更新器不能用来首升。源码包及说明可按业主例外放文件服务；包不得包含 key、凭据、env、inbox、ledger 或私有状态。不给文件服务发送 Agent 凭据，不使用旧匿名 `/dl`。
- 首次手动安装 **不创建升级作业**。出厂版本无 armed pending，正常情况下不需要 job/confirmed 回执；不要伪造作业或修改指针。以后 Console 下发的升级才要求严格作业绑定确认。

已知布局（任一实测不一致即停下报告）：

| 项目 | 值 |
|---|---|
| 容器 | `dw123oaagent` |
| 宿主 bind 源目录 | `/data/agent-staging/dw123.cmjrt.com/dw123oaagent/data/sessionhelper-src/tools/sessionhelper` |
| 容器只读挂载点 | `/opt/dingwei/sessionhelper-src` |
| venv | `/opt/dingwei/sessionhelper-venv`，Python 3.12.3 |
| 入口 | tini → bash → source `/run/secrets/sessionhelper_env` → venv python `-m sessionhelper` |
| 有效 PYTHONPATH | `/opt/dingwei/sessionhelper-src` |
| restart policy | `unless-stopped` |

## 1. 取得包与说明，独立核 SHA

本次候选尚无核准上传制品。执行人必须从 prodops 经审交付记录独立取得下列值，全部 export；缺失即停。旧包不含稳定转发器，不能用旧包 SHA 替代。

- `OA_PACKAGE_URL`、`OA_PACKAGE_SHA256`、`OA_PACKAGE_BYTES`、`OA_PACKAGE_EXPIRES_UTC`
- `OA_COMMIT`、`OA_TREE`：包含稳定转发器的经审精确提交及 tree
- `OA_INSTRUCTIONS_URL`、`OA_INSTRUCTIONS_SHA256`
- runtime lock 保持 `5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0`

SHA 须与本份经审执行单及 prodops 核准的 Git 制品记录独立核对，不能只信下载响应。本文件定稿审过后，由 prodops 上传说明并在外部交付记录提供独立 URL/SHA。执行人先 export 这两个变量；缺失或为空即停，不能猜造链接或把本文件自己的哈希写回文件。到期后停止下载并向 leader 申请新链接，不能自行换包或接受新 SHA。

```bash
set -euo pipefail
umask 077
[ "$(id -u)" = 0 ]
OA_CONTAINER=dw123oaagent
OA_MOUNT=/data/agent-staging/dw123.cmjrt.com/dw123oaagent/data/sessionhelper-src/tools/sessionhelper
for OA_REQUIRED in OA_PACKAGE_URL OA_PACKAGE_SHA256 OA_PACKAGE_BYTES OA_PACKAGE_EXPIRES_UTC OA_COMMIT OA_TREE OA_INSTRUCTIONS_URL OA_INSTRUCTIONS_SHA256; do
  [ -n "${!OA_REQUIRED:-}" ] || { echo "STOP: missing $OA_REQUIRED"; exit 1; }
done
[[ "$OA_COMMIT" =~ ^[0-9a-f]{40}$ && "$OA_TREE" =~ ^[0-9a-f]{40}$ ]]
OA_LOCK_SHA256=5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0
OA_WORK=$(mktemp -d /root/oaagent-bootstrap.XXXXXXXX)
OA_PACKAGE="$OA_WORK/helper-release.tgz"
OA_STAGE="$OA_WORK/stage"
OA_BACKUP="$OA_WORK/installed-source-backup"
export OA_WORK OA_MOUNT OA_COMMIT OA_TREE OA_LOCK_SHA256
[ -d "$OA_MOUNT" ] && [ ! -L "$OA_MOUNT" ]
command -v docker
command -v python3
python3 --version
```

预期：宿主 root，工具存在，工作目录在挂载目录之外。这里宿主 Python 仅用标准库，不要求使用容器 venv。不输出任何凭据。

**A，默认：文件服务包 + SHA。** URL 必须是 prodops 发出的 HTTPS 最终直链，不能有认证材料。以下不跟随重定向、不读取 curlrc、不带凭据；3xx 也拒绝。

```bash
[[ "$OA_PACKAGE_URL" = https://* ]]
[ "$(date -u +%s)" -lt "$(date -u -d "$OA_PACKAGE_EXPIRES_UTC" +%s)" ]
[[ "$OA_PACKAGE_SHA256" =~ ^[0-9a-f]{64}$ ]]
[[ "$OA_PACKAGE_BYTES" =~ ^[0-9]+$ ]]
OA_HTTP=$(curl --disable --silent --show-error --fail --noproxy '*' \
  --proto '=https' --connect-timeout 10 --max-time 180 \
  --output "$OA_PACKAGE" --write-out '%{http_code}' "$OA_PACKAGE_URL")
[ "$OA_HTTP" = 200 ]
printf '%s  %s\n' "$OA_PACKAGE_SHA256" "$OA_PACKAGE" | sha256sum --check -
[ "$(stat -c %s "$OA_PACKAGE")" = "$OA_PACKAGE_BYTES" ]
# 说明 URL/SHA 来自外部 export；缺失、为空或格式错误即停。
[[ "$OA_INSTRUCTIONS_URL" = https://* ]]
[[ "$OA_INSTRUCTIONS_SHA256" =~ ^[0-9a-f]{64}$ ]]
OA_HTTP=$(curl --disable --silent --show-error --fail --noproxy '*' \
  --proto '=https' --connect-timeout 10 --max-time 60 \
  --output "$OA_WORK/instructions.md" --write-out '%{http_code}' "$OA_INSTRUCTIONS_URL")
[ "$OA_HTTP" = 200 ]
printf '%s  %s\n' "$OA_INSTRUCTIONS_SHA256" "$OA_WORK/instructions.md" | sha256sum --check -
```

预期：包 HTTP 200、SHA `OK`、字节数等于核准 OA_PACKAGE_BYTES；说明也必须 HTTP 200、SHA `OK`。SHA 不符立即停，**不得解包、执行包内脚本或安装**。过期、404、重定向、TLS 错误、任何 SHA/大小不符即停；不加 `-k`、`-L` 或改用旧更新器。

**B，备选：从 Git 精确 commit 构建。** 仅在执行人已有仓库只读权限时采用，用此块替代 A 的包下载；说明可直接读取该 runbook 的精确 Git 提交。不要临时把 Git 私钥放进包或容器。

```bash
[[ "$OA_PACKAGE_SHA256" =~ ^[0-9a-f]{64}$ ]]
[[ "$OA_PACKAGE_BYTES" =~ ^[0-9]+$ ]]
git clone --no-checkout git@git.code.tencent.com:wego-ai/dingwei.git "$OA_WORK/git"
git -C "$OA_WORK/git" checkout --detach "$OA_COMMIT"
[ "$(git -C "$OA_WORK/git" rev-parse HEAD^{tree})" = "$OA_TREE" ]
[ -z "$(git -C "$OA_WORK/git" status --porcelain)" ]
python3 -B "$OA_WORK/git/tools/sessionhelper/package_release.py" \
  --repo "$OA_WORK/git" --commit "$OA_COMMIT" --output "$OA_PACKAGE" \
  > "$OA_WORK/build.json"
printf '%s  %s\n' "$OA_PACKAGE_SHA256" "$OA_PACKAGE" | sha256sum --check -
[ "$(stat -c %s "$OA_PACKAGE")" = "$OA_PACKAGE_BYTES" ]
cat "$OA_WORK/build.json"
```

预期：checkout 精确 commit/tree，白名单/禁止私有状态扫描通过，输出 commit/tree/version/sha256/bytes/lock_sha256 与 prodops 记录一致。构建不含当前工作区未提交代码；不在线装依赖。若与发布包 SHA 不同即停，不能用本地产物的新 SHA 自行替代核准值。

**A/B 共用：安全展开及清单校验。** 此时外层包 SHA 已验证。只接受普通文件，拒绝链接、越界、重复路径和超限；不直接对未知包 `tar -xf`。

```bash
mkdir "$OA_STAGE"
python3 -B - "$OA_PACKAGE" "$OA_STAGE" <<'PY'
import pathlib, sys, tarfile
archive, stage = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
assert archive.stat().st_size <= 32 * 1024 * 1024
seen, total = set(), 0
with tarfile.open(archive, 'r:gz') as stream:
    for member in stream:
        path = pathlib.PurePosixPath(member.name)
        assert member.isfile() and path.parts[0] == 'helper-release'
        assert not path.is_absolute() and '..' not in path.parts
        assert str(path) == member.name and member.name not in seen
        seen.add(member.name); total += member.size
        assert len(seen) <= 2048 and 0 <= member.size and total <= 32 * 1024 * 1024
        target = stage.joinpath(*path.parts)
        target.parent.mkdir(parents=True, exist_ok=True)
        with stream.extractfile(member) as source, target.open('xb') as output:
            output.write(source.read())
assert seen
print('SAFE_EXTRACT_OK')
PY
OA_RELEASE="$OA_STAGE/helper-release"
export OA_RELEASE
python3 -B - "$OA_PACKAGE" "$OA_PACKAGE_SHA256" <<'PY'
import importlib.util, os, pathlib, sys
root = pathlib.Path(os.environ['OA_RELEASE'])
def load(name, file):
    spec = importlib.util.spec_from_file_location(name, root / file)
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    return module
manifest, _ = load('package_check', 'helper_update_package.py').verify(sys.argv[1], sys.argv[2])
assert load('bootstrap_check', 'bootstrap.py').verified_source(root) == manifest
assert manifest['commit'] == os.environ['OA_COMMIT']
assert manifest['tree'] == os.environ['OA_TREE']
assert manifest['version'] == '0.2.3.2' and manifest['platform'] == 'linux'
assert manifest['lock_sha256'] == os.environ['OA_LOCK_SHA256']
print('MANIFEST_OK commit/tree/version/platform/lock and every file')
PY
# 只处理已验证、无私有状态的展开源码；容器非 root 用户也须可读。
find "$OA_RELEASE" -type d -exec chmod 0755 {} +
find "$OA_RELEASE" -type f -exec chmod 0644 {} +
```

预期：`SAFE_EXTRACT_OK`、`MANIFEST_OK ...`。manifest、锁或任一文件 pin 不符停下。后续使用 `python3 -B`，不在包目录生成 `__pycache__` 或日志，否则 verified_source 会因额外文件拒绝。

## 2. preflight：只读挂载、入口、身份、状态及跨重启保留

**只读 preflight**：下面这一步无重启、无状态目录创建、不替换源码；它只读 Docker/容器配置、依赖版本与已有状态。输出文件仅写在宿主执行记录目录。此阶段禁止安装。

```bash
OA_CONTAINER_ID=$(docker inspect --format '{{.Id}}' "$OA_CONTAINER")
[ "$(docker inspect --format '{{.State.Running}}' "$OA_CONTAINER")" = true ]
python3 -B "$OA_RELEASE/bootstrap.py" --preflight \
  --mount "$OA_MOUNT" --container "$OA_CONTAINER" \
  > "$OA_WORK/preflight-readonly.json"
cat "$OA_WORK/preflight-readonly.json"
OA_UPDATE_STATE=$(python3 -B -c 'import json,sys;print(json.load(open(sys.argv[1]))["state"])' \
  "$OA_WORK/preflight-readonly.json")
export OA_UPDATE_STATE
```

预期 JSON：`version` 为 `0.2.3.2`，`state` 是容器内解析出的绝对 `.../update` 路径。首次 `restart_verified:false` 正常；这里不是旧 helper 版本输出。脚本同时检查精确 RO bind 源/目的、unless-stopped、已认可的原入口、有效 PYTHONPATH、锁定依赖版本；拒绝被其他运行容器共享/重叠使用的源码目录。失败会报 `bootstrap rejected; inspect local preflight conditions`，须停下人工查明，不能绕过。

定义后续容器检查入口：从原 secret 文件加载但不输出内容，使用默认容器用户，不能擅自加 `docker exec -u root`。

```bash
oa_python() {
  docker exec -i "$OA_CONTAINER" bash -c \
    'set -a; source /run/secrets/sessionhelper_env; exec "$@"' oaagent-bootstrap \
    /opt/dingwei/sessionhelper-venv/bin/python -B "$@"
}
oa_python - <<'PY' | tee "$OA_WORK/state-before.json"
import json, os, pathlib, sys
state = pathlib.Path(os.path.expanduser(os.environ.get('SH_STATE_DIR', '~/.dingwei'))) / 'update'
assert state.is_absolute() and not state.is_symlink()
assert not state.resolve().is_relative_to(pathlib.Path('/opt/dingwei/sessionhelper-src'))
assert sys.version_info[:3] == (3, 12, 3)
print(json.dumps({'python':sys.version.split()[0], 'uid':os.getuid(), 'gid':os.getgid(),
 'update_state':str(state.resolve()), 'exists':state.exists(),
 'owner':state.stat().st_uid if state.exists() else None,
 'mode':oct(state.stat().st_mode & 0o777) if state.exists() else None}))
if state.exists():
    assert state.is_dir() and state.stat().st_uid == os.getuid()
    assert not state.stat().st_mode & 0o077
    # 首次首升不应已有安装槽位指针/试运行；若有，先核实历史，不删状态。
    assert not any((state / name).exists() for name in ('current','previous','journal.json'))
PY
```

预期 Python3.12.3、实际 UID/GID、解析状态路径；已存在时 owner=运行用户且 mode 无组/其他权限。记录该实际路径及它是容器可写层还是哪个现有持久卷（用下列**筛选**输出查映射，禁止打印完整 inspect）：

```bash
docker inspect --format '{{range .Mounts}}{{printf "%s -> %s rw=%t\n" .Source .Destination .RW}}{{end}}' \
  "$OA_CONTAINER" > "$OA_WORK/mount-map.txt"
cat "$OA_WORK/mount-map.txt"
```

状态必须在容器可写区域，不能位于 `/opt/dingwei/sessionhelper-src` 之下；若是其他挂载，其映射必须可写。`bootstrap.py` 下一步的真实 marker 验证才是跨重启证据，不能只凭目录名判断。

**以下另属有状态的持久性实测，不是只读 preflight。** 它会写临时 marker/proof 并进行第一次计划重启（仍运行旧代码）。只读 preflight 通过后，执行前先通知业主，确认允许中断当前回合；未获执行窗口则停在上一阶段。

```bash
python3 -B "$OA_RELEASE/bootstrap.py" --preflight \
  --mount "$OA_MOUNT" --container "$OA_CONTAINER" --state "$OA_UPDATE_STATE" \
  --check-restart-persistence > "$OA_WORK/preflight-restart.json"
cat "$OA_WORK/preflight-restart.json"
[ "$(docker inspect --format '{{.Id}}' "$OA_CONTAINER")" = "$OA_CONTAINER_ID" ]
python3 -B - "$OA_WORK/preflight-restart.json" "$OA_UPDATE_STATE" <<'PY'
import json, sys
value = json.load(open(sys.argv[1]))
assert value['restart_verified'] is True and value['state'] == sys.argv[2]
print('RESTART_PERSISTENCE_OK')
PY
oa_python - <<'PY' | tee "$OA_WORK/state-after.json"
import json, os, pathlib
p=pathlib.Path(os.path.expanduser(os.environ.get('SH_STATE_DIR','~/.dingwei')))/'update'
assert p.is_dir() and p.stat().st_uid == os.getuid() and not p.stat().st_mode & 0o077
print(json.dumps({'update_state':str(p.resolve()),'uid':os.getuid(),
                 'owner':p.stat().st_uid,'mode':oct(p.stat().st_mode & 0o777)}))
PY
```

预期 `restart_verified:true`、`RESTART_PERSISTENCE_OK`、容器 ID 不变、owner 正确。脚本会以容器原用户创建 0700 状态目录和 0600 随机 marker，重启后核对内容与属主，再清理 marker，成功才写 proof。记录 resolved 路径、映射、UID/mode、时间和这两份 JSON。失败只留下待查现场，**不能安装**；没有通过不准手工伪造 proof。

proof 绑定容器 ID、MANIFEST 哈希，**仅 600 秒有效**。后续备份/安装如超时，要重新经批准执行本节持久性检查（会再重启），不延长/编辑 proof。

## 3. 本地备份（不含 secret/env/私有状态）

先被动记录当前容器的主进程连接版本。下列函数仅由宿主标准库读取 Docker 日志，不导入或执行 sessionhelper/guard。每次检查总期限120秒，单次 Docker 查询受剩余期限约束；不跟随无限日志流，不打印原日志/凭据。

```bash
oa_wait_connection() {
  python3 -B - "$OA_CONTAINER" "$1" "$2" <<'PYLOG'
import re, subprocess, sys, time
container, since, expected = sys.argv[1:]
pattern = re.compile(r'\[sessionHelper\] connected self=\S+ version=' + re.escape(expected) + r'(?:\s|$)')
deadline = time.monotonic() + 120
while True:
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise SystemExit('STOP: authenticated connection version not observed within 120 seconds')
    try:
        result = subprocess.run(['docker','logs','--since',since,container],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=min(10, remaining))
    except subprocess.TimeoutExpired:
        continue
    if result.returncode:
        raise SystemExit('STOP: Docker logs unavailable; no version acceptance')
    if any(pattern.search(line) for line in (result.stdout + result.stderr).decode(errors='replace').splitlines()):
        print('PASSIVE_CONNECTED_VERSION_OK version=' + expected)
        break
    time.sleep(min(2, max(0, deadline - time.monotonic())))
PYLOG
}
OA_OLD_STARTED=$(docker inspect --format '{{.State.StartedAt}}' "$OA_CONTAINER")
oa_wait_connection "$OA_OLD_STARTED" 0.2.3.0 | tee "$OA_WORK/version-before.txt"
# 只备份本次六项源码路径；不复制 Agent 整目录、env、venv、inbox 或 ledger。
python3 -B - "$OA_MOUNT" "$OA_WORK/source-before.tar" <<'PY'
import pathlib, sys, tarfile
root, output = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
names=('sessionhelper','factory','helper_update_guard.py','helper_update_package.py',
       'requirements-runtime.lock','MANIFEST.json')
for name in names:
    p=root/name
    assert not p.is_symlink()
    if p.is_dir():
        assert all(not q.is_symlink() and (not q.is_file() or q.suffix in ('.py','.pyc'))
                   for q in p.rglob('*'))
with tarfile.open(output,'w') as archive:
    for name in names:
        if (root/name).exists(): archive.add(root/name,arcname=name)
print('SOURCE_BACKUP_OK')
PY
sha256sum "$OA_WORK/source-before.tar" > "$OA_WORK/source-before.sha256"
chmod 0600 "$OA_WORK/source-before.tar" "$OA_WORK/source-before.sha256"
```

预期 `PASSIVE_CONNECTED_VERSION_OK version=0.2.3.0`、`SOURCE_BACKUP_OK`，本地私有备份包含现有 A6 补丁。备份仅留宿主 root 工作目录，不上传。遇到软链或目录夹杂非 Python 文件停下，不移动/删除它们继续。安装器另会在替换前生成六项限定路径的可恢复备份与 receipt；下节 `--backup` 路径必须尚不存在。

## 4. 安装稳定启动器、guard 与出厂代码

确认步骤 2 的 proof 仍在 600 秒内；若任何步骤异常，先停，勿重复覆盖。此步不重启、不重建容器，不在线安装依赖。

```bash
[ ! -e "$OA_BACKUP" ]
python3 -B "$OA_RELEASE/bootstrap.py" --install \
  --mount "$OA_MOUNT" --container "$OA_CONTAINER" --state "$OA_UPDATE_STATE" \
  --backup "$OA_BACKUP" > "$OA_WORK/install.json"
cat "$OA_WORK/install.json"
[ -f "$OA_BACKUP/receipt.json" ]
# 仅这六项已核准源码变为容器用户可读，不改任何私有状态权限。
python3 -B - "$OA_MOUNT" <<'PY'
import os, pathlib, sys
root=pathlib.Path(sys.argv[1])
for name in ('sessionhelper','factory','helper_update_guard.py','helper_update_package.py',
             'requirements-runtime.lock','MANIFEST.json'):
    p=root/name
    assert not p.is_symlink()
    if p.is_dir():
        os.chmod(p,0o755)
        for q in p.rglob('*'):
            assert not q.is_symlink()
            os.chmod(q,0o755 if q.is_dir() else 0o644)
    else: os.chmod(p,0o644)
print('SOURCE_READ_PERMISSIONS_OK')
PY
```

预期 JSON 含 version0.2.3.2、restart_verified:true、receipt 为 `$OA_BACKUP/receipt.json`，以及 `SOURCE_READ_PERMISSIONS_OK`。替换仅六项；稳定 `sessionhelper`、guard/package 校验器和 `factory/sessionhelper` 均来自已验包。槽位和指针不会写到 RO 挂载里。安装器会先备份，复制异常时尝试按 receipt 恢复；这不是整个挂载目录的原子切换。任何失败先查 receipt/备份是否完好，按步骤 7 恢复，不盲目 restart。

## 5. 第二次计划重启，保持原容器及入口

```bash
docker restart "$OA_CONTAINER"
OA_RESTART_UTC=$(docker inspect --format '{{.State.StartedAt}}' "$OA_CONTAINER")
[ "$(docker inspect --format '{{.Id}}' "$OA_CONTAINER")" = "$OA_CONTAINER_ID" ]
[ "$(docker inspect --format '{{.State.Running}}' "$OA_CONTAINER")" = true ]
[ "$(docker inspect --format '{{.HostConfig.RestartPolicy.Name}}' "$OA_CONTAINER")" = unless-stopped ]
oa_wait_connection "$OA_RESTART_UTC" 0.2.3.2 | tee "$OA_WORK/version-after.txt"
```

预期 `PASSIVE_CONNECTED_VERSION_OK version=0.2.3.2`，容器 ID/入口/挂载不变。`OA_RESTART_UTC` 取自本次重启的 Docker `StartedAt`（含小数精度），只接受这个边界之后的 `connected self=` 主进程日志；旧日志、源码版本字符串或 manifest 不能替代新连接。最多等120秒，日志不可读或超时即停报 leader，不反复重启。

如需核对文件，只读 `MANIFEST.json` 和已验证文件的原始字节/哈希，不导入或执行 sessionhelper、guard，也不运行其任何子命令。首次首升无 job，生产原入口启动稳定guard及出厂代码，不因缺job自动回滚；**本执行单不另起helper/guard进程**。持续退出、setup提示、权限/导入错误或版本不上线则停并回滚；下一节还须核对真实认证Console投影和功能。

## 6. 人工验收及当前连接能力

### 6.1 当前连接自报版本与能力（只读 Console）

业主登录 dw123 Console，确认 **oaagent.lbl** 在线、重连时间晚于步骤 5、自报 **0.2.3.2**。可在该 Console 页浏览器 DevTools 执行以下只读请求；不要把 cookie/token 粘到终端或交件里：

```javascript
const bootstrapStatus = await fetch('/api/console/helper-updates', {credentials:'same-origin'});
if (!bootstrapStatus.ok) throw new Error('STOP: helper-update projection unavailable');
const bootstrapProjection = await bootstrapStatus.json();
console.table(bootstrapProjection.agents.map(({agent_id,name,version,online,eligible,reason}) =>
  ({agent_id,name,version,online,eligible,reason})));
```

预期精确选定该 Agent（记录其 agent_id，勿只按显示名猜）：online:true、version0.2.3.2、eligible:true、无拒绝 reason。此投影的 eligible 同时检查当前认证连接的 `helper_update_v1`，不是仅看版本或 env；保留截图/去敏结果。404/未部署/无能力/身份不符时本项 **未完成**，停止并报 leader，不 POST offer 来试探，不把“本地版本正确”当能力已声明。首升不要求 job 已 confirmed，页面也可能没有已登记包可选。

### 6.2a 首升后重启 Codex，检查 MCP 配置形态并验收 tools/list

容器/helper 重启不一定重启 `daemon_ws` 对接的独立 Codex。首升前启动的 MCP 进程不会自动换代码。由业主/CLI 负责人在获准中断窗口保存当前上下文，再重启对应 Codex 进程或打开全新 Codex 会话；本件不提供猜测 PID 的 kill 命令，不重启无关会话。记录 Codex 重启/新会话 UTC。仅重启容器或只见 IPC socket 不足以通过此门。

负责人只读检查该 Codex 实际使用的 config.toml 中 `mcp_servers.dingwei-send` 的 command、args、env.PYTHONPATH，记录以下形态编号；不要输出整个配置、env 表或认证值。Python 应是现有 venv 的解释器，PYTHONPATH 仅记录此一非敏感字段。配置不可定位或出现不认识的启动命令即停，不能试跑任意配置命令。

1. `-m sessionhelper.mcp_shim` + `PYTHONPATH=/opt/dingwei/sessionhelper-src`：新稳定转发器，只读选择 journal 的已确认 current；无有效 current 时用 factory。pending/previous 不参与选择，不经 guard、不锁写、不记启动预算。
2. `/opt/dingwei/sessionhelper-src/factory/sessionhelper/mcp_shim.py` 脚本：仍可列出九工具，但始终为 factory 版本，自升级后不自动跟随 current。
3. `-m sessionhelper.mcp_shim` + `PYTHONPATH=/opt/dingwei/sessionhelper-src/factory`：从挂载目录之外运行时可用，始终为 factory；不要把它当活动槽位验证。

不使用 `python -m sessionhelper --mcp` 作为本门验收。稳定转发器随首升包安装；自升级不会修复旧稳定根缺文件的问题。须同时核对新包 MANIFEST 确有 `sessionhelper/mcp_shim.py`，且实际挂载字节哈希符合该 pin。

在宿主运行以下一次隔离的 MCP stdio 探测：只发送 initialize/initialized/tools/list，不调用任何工具、不发送业务消息。它不是 helper/guard 子命令。

```bash
: "${OA_MCP_EXPECTED_VERSION:=0.2.3.2}"
docker exec -i "$OA_CONTAINER" bash -c \
  'set -a; source /run/secrets/sessionhelper_env; exec "$@"' oaagent-mcp-check \
  /opt/dingwei/sessionhelper-venv/bin/python -B - "$OA_MCP_EXPECTED_VERSION" <<'PY' | tee "$OA_WORK/mcp-tools-list.json"
import json, os, subprocess, sys
requests = [
 {'jsonrpc':'2.0','id':1,'method':'initialize','params':{}},
 {'jsonrpc':'2.0','method':'notifications/initialized'},
 {'jsonrpc':'2.0','id':2,'method':'tools/list','params':{}}]
env = dict(os.environ, PYTHONPATH='/opt/dingwei/sessionhelper-src', PYTHONDONTWRITEBYTECODE='1')
result = subprocess.run(['/opt/dingwei/sessionhelper-venv/bin/python','-B','-m','sessionhelper.mcp_shim'],
 input=''.join(json.dumps(x)+'\n' for x in requests), text=True,
 capture_output=True, timeout=15, env=env, cwd='/data/ai-dev-data')
assert result.returncode == 0 and not result.stderr, 'STOP: MCP stdio failed; retain private error locally'
replies = {x['id']:x for x in map(json.loads,result.stdout.splitlines())}
version = replies[1]['result']['serverInfo']['version']
assert version == sys.argv[1], 'STOP: unexpected MCP version'
names = sorted(x['name'] for x in replies[2]['result']['tools'])
assert names == sorted(['send_message','dw_inbox','dw_wait_message','dw_reply_message',
 'heartbeat_report','project_context','dw_spec_create','dw_spec_update','upload_file'])
print(json.dumps({'mcp':'TOOLS_LIST_OK','version':version,'tool_count':len(names),'tools':names}))
PY
```

预期 `TOOLS_LIST_OK`、版本0.2.3.2、九个精确工具名。之后还须在刚重启/新建的真实 Codex 会话确认 DingWei 九工具可见，记录界面证据；独立 stdio 成功但 Codex 仍不可见属于未通过。此处不自动修改 MCP 配置。旧根无转发器、tools/list 超时/导入错误、工具缺失均停报 leader，保留原配置及状态。

### 6.2 MCP 套接字（不发送业务消息的探测）

```bash
oa_python - <<'PY' | tee "$OA_WORK/ipc-check.txt"
import json, os, pathlib, socket, stat
raw=os.environ.get('SH_MCP_IPC_SOCK') or os.environ.get('DINGWEI_MCP_IPC_SOCK') or ''
assert raw, 'STOP: IPC socket is not configured'
p=pathlib.Path(os.path.expanduser(raw))
assert p.is_absolute() and len(os.fsencode(str(p))) <= 107
assert stat.S_ISSOCK(p.stat().st_mode), 'STOP: socket missing'
with socket.socket(socket.AF_UNIX) as client:
    client.settimeout(5);client.connect(str(p))
    client.sendall(b'{"type":"mcp_send"}\n');client.shutdown(socket.SHUT_WR)
    data=b''
    while b'\n' not in data and len(data)<65536:
        block=client.recv(4096)
        if not block: break
        data+=block
    reply=json.loads(data)
assert reply.get('ok') is False and reply.get('error') == 'to is required'
print('IPC_LISTENER_OK (missing-target request rejected; no business send)')
PY
```

预期 `IPC_LISTENER_OK ...`。无目标的请求只验证监听/解析，不会派发消息，不等于真实往返。套接字尚在初始化时等待主进程就绪后最多重查一次；仍缺失、路径过长或响应不符停下，不临时改 IPC 路径。

### 6.3 一次真实收发往返

业主在 Console/View 给 oaagent.lbl 发一次独特的无敏感测试句，例如“首升验收 OA-日期时间，请原样回复标记”。等待真实 CLI 完成并在同一会话看到对应标记回复。记录发出/收到 UTC、Agent ID、标记和结果，不靠 socket 错误探测代替，也不批量向其他 Agent 发送。未收到、回到错误会话或重复注入即停报 leader；不要反复重发掩盖故障。

### 6.4 A6 真实磁盘指标

打开 oaagent 的 View，待新 helper 样本与页面刷新后，确认磁盘总量/可用量为数字且属于实际 Codex 配置工作目录所在文件系统，非未知/旧缓存。helper 通常每 30 秒采样，页面读取约 300 秒（TTL360秒）；允许等待下一样本并刷新页面，记录观察时间。持续 `workdir_unbound`、unavailable、未知或无法证明确为新连接样本时不通过，报 leader；不把 helper 自身 cwd 的容量当替代证明。

最终验收清单：认证重连0.2.3.2、IPC、真实一次往返、A6、当前 helper_update_v1 及 Codex 重启/真实九工具可见，共六项全部记录。缺一项不报首升完成。手动首升阶段不创建 Spec、审批或升级作业；通过后再单独进行下节由业主发起的自升级测试。宿主私有留存包/清单/哈希、preflight、容器 ID、resolved 状态路径及映射/属主/marker 结果、备份 receipt、验收时间。对外只给去敏摘要；不上传备份或私有状态。

### 6.5 业主从 Console 单次下发自升级验收（首升完成后单独进行）

1. 业主打开 Console 的 helper 升级入口，确认列表能看到精确 **oaagent.lbl**（核 agent_id），在线版本0.2.3.2且可升级。看不到或不可选则停下，记录 reason，不能改权限/能力或用别的 Agent 替代。
2. **本次手动首升候选本身是0.2.3.2。** `self_update.py` 的 `_prepare` 明确拒绝目标版本小于等于运行版本（`version_not_new`）。因此首升后再下发当前这个包不能证明安装/重启/确认成功。完整测试须由运维另行登记经审、授权的**高于0.2.3.2**版本包，锁哈希保持不变，并由 leader 确认该验收窗口。若目录只有当前包，停在此处报“自升级验收待可用新版本包”，不能把同版本拒绝记为通过，也不能手改包清单、伪造版本或放宽产品限制。
3. 具备上述新包后，Console 仅选 oaagent 这一个 Agent，选择已登记的目标 SHA，**下发一次**。此时才创建升级作业；记录 job_id、目标版本、SHA、下发 UTC。不得批量选择、重复点击或自填 URL。
4. 观察同一作业的下载、安装、重启及最终 confirmed/上线新版本结果。核目标版本真实重连，当前能力仍有效，重做 IPC/一次往返/A6，并重启对应 Codex或开新会话，重做6.2a 的 module tools/list（先将 `OA_MCP_EXPECTED_VERSION` 显式设为核准新目标，版本须与之相等）；factory 脚本形态也须九工具可用但版本仍为原出厂0.2.3.2，不把 factory 输出当活动槽位版本；保留去敏状态。只有 confirmed 且版本/Agent/job/SHA 对应、功能检查正常，才报自升级验收完成。
5. 失败/rolled_back/expired、同版本拒绝或长时间未 confirmed 都记为失败或未完成，立即停报 leader，不重发、不改 journal/current。不执行本节时，本件只能证明手动首升结果，不能宣称自升级已验收。

## 7. 回滚（仅执行人在已批准首升/回滚窗口内使用）

触发：安装/新版本启动失败、关键验收失败，或 leader 要求回退。先保留工作目录、原包、receipt 和去敏错误摘要，不删私有状态，不重建容器。

若安装根本未开始、没有 receipt，也未替换源码：停下报告，保持旧源码。若安装器失败并自动恢复，仍核对本地 receipt 后按下列步骤验证旧版；restore 本身失败则停止、保留现场交 leader，不能从不完整备份继续。

```bash
[ -f "$OA_BACKUP/receipt.json" ]
docker stop "$OA_CONTAINER"
python3 -B "$OA_RELEASE/bootstrap.py" --restore --receipt "$OA_BACKUP/receipt.json"
docker start "$OA_CONTAINER"
OA_ROLLBACK_UTC=$(docker inspect --format '{{.State.StartedAt}}' "$OA_CONTAINER")
[ "$(docker inspect --format '{{.Id}}' "$OA_CONTAINER")" = "$OA_CONTAINER_ID" ]
oa_wait_connection "$OA_ROLLBACK_UTC" 0.2.3.0 | tee "$OA_WORK/version-rollback.txt"
```

预期 restore 输出 `source restored; owner must restart container and verify old version`，原容器重新启动，120秒内观察本次启动之后的 `PASSIVE_CONNECTED_VERSION_OK version=0.2.3.0`，再由认证Console被动核对旧版连接，原A6补丁随源备份恢复。超时/无连接即停，不改为执行helper/guard子命令核验。restore 不需要 import 候选代码或运行中的容器，只恢复 receipt 中六项允许路径。不要误用新版本整个包覆盖旧备份；不要恢复/覆盖 env、凭据、inbox、ledger 或 venv。以步骤6的 IPC、一次往返、A6重新验证旧版，Console 更新能力恢复旧版不支持是预期。

**本节六路径回退只针对手动首升。** 如果已执行6.5并存在后续自升级作业/槽位，须先报 leader 使用该作业的回滚流程，不可直接套用本节覆盖源码。首次首升无 armed job，通常没有 current/pending 槽位；如果发现意外的已武装升级、后续作业或指针，停止本执行单并由 leader 另定状态回退，不能清空 update/ 来强行恢复。源备份留存到业主验收与 leader 允许清理后；本件不给清理授权。

## 交件与执行记录（待执行人填写）

- 制品 URL / SHA / bytes / runtime lock SHA：已填步骤1，来源为 leader 转交的 prodops 记录；执行人下载后仍须实际核对。
- Console 单次自升级验收：待经审的新于0.2.3.2版本包与业主执行窗口；不能用当前同版本包记安装成功。
- 执行授权/窗口、执行人、UTC：未执行。
- resolved update 路径、UID/GID/mode、容器可写层或卷映射：未实测。
- 两次 restart 的容器 ID/marker 与版本结果：未执行。
- 认证版本/能力、IPC、往返、A6、Codex 重启/配置形态/九工具：未执行。
- 回滚是否触发及结果：未执行。
- 本文静态检查仅验证文档命令语法与 bootstrap/package 接口，不代替真实 Docker、首升或升级验收。
