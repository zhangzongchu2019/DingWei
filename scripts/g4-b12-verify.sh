#!/usr/bin/env bash
# g4-b12-verify.sh — 逐家验通制（V6 判据 B12）干净容器假凭据验通脚本（r2：复验 tosonnet55tc2k 修订）
#
# 用法（在 G1 托管节点，有 docker；prodops 代跑）：
#   BASE_IMAGE=hub.local/dingwei/base-codex@sha256:<digest> ./g4-b12-verify.sh            # 默认只跑 Codex 两条
#   BASE_IMAGE=<ref> ./g4-b12-verify.sh codex-apikey codex-tokenfile claude-apikey ...    # 指定组合
#
# 判定（B12 口径，r3 收紧——组长令）：PASS 只认两类——① 401 且紧邻 Unauthorized/HTTP/status/
# code 字样（窗口 40 字符内）；② 明确的 API 鉴权失败判词（incorrect api key／invalid api key／
# authentication_error／invalid x-api-key）。裸 401 子串、裸 unauthorized 不算数。
# 🔴 r2 修订（复验第三节）：
#   · "not logged in"/"not authenticated" 不再算 PASS——令牌文件路径下这恰恰是「凭据没被读到」；
#   · docker/镜像层错误（pull access denied／registry unauthorized／manifest unknown／
#     connection／dns lookup）一律 FAIL——容器或 CLI 根本没跑起来，谈不上验通；
#   · 判词匹配带词边界（\b），网络错误里的请求号子串（如 id 4012ab）不再误中 401；
#   · 默认不带参数时跑 Codex 两条（r1 的 "${@:-…}" 带引号整串是一个词，默认根本不跑）。
# 🔴 容器参数与生产同口径（nodeagent buildSpec）：只读根＋带限 tmpfs＋非 root＋降权。
# 🔴 凭据纪律：只用假凭据；一次性容器＋tmp HOME；绝不对在用身份执行 login 类命令。
#
# 读数格式（逐行，供判读）：COMBO <组合> <PASS|FAIL> <判词摘录>；末尾 VERDICT <PASS|FAIL>。

set -u
BASE_IMAGE="${BASE_IMAGE:?BASE_IMAGE 必填（固定仓库＋sha256 digest 的基镜像）}"
FAKE_KEY="sk-test-invalid-b12"
FAKE_TOKEN_JSON='{"access_token":"tok-test-invalid-b12","account_id":"acct_test"}'

pass=0; fail=0
report() {
  echo "COMBO $1 $2 $3"
  if [ "$2" = PASS ]; then pass=$((pass+1)); else fail=$((fail+1)); fi
}

# 鉴权失败判词（词边界）⇒ PASS；基建/镜像/网络错误 ⇒ FAIL；其余 ⇒ FAIL
judge() {
  local combo="$1" out="$2"
  if printf '%s' "$out" | grep -qiE 'pull access denied|manifest unknown|connection (reset|refused)|dns lookup|no such host|Error response from daemon|authentication required'; then
    report "$combo" FAIL "$(printf '%s' "$out" | grep -oiE 'pull access denied|manifest unknown|connection (reset|refused)|dns lookup|no such host|Error response from daemon|authentication required' | head -1)（基建层失败，未验到凭据通路）"
    return
  fi
  if printf '%s' "$out" | grep -qiE '\bnot logged in\b|\bnot authenticated\b'; then
    report "$combo" FAIL "not-logged-in（凭据没被读到——令牌件路径恰是要验的东西）"
    return
  fi
  local hit
  # r3：401 须有紧邻伴生词（Unauthorized/HTTP/status/code，40 字符窗口）；API 鉴权短语单列
  hit=$(printf '%s' "$out" | grep -oiE '(\b401\b.{0,40}(unauthorized|HTTP|status|code)|(unauthorized|HTTP|status|code).{0,40}\b401\b|incorrect api key|invalid api key|authentication_error|invalid x-api-key)' | head -1)
  if [ -n "$hit" ]; then
    report "$combo" PASS "$hit"
  else
    report "$combo" FAIL "$(printf '%s' "$out" | tail -2 | tr '\n' ' ' | cut -c1-120)"
  fi
}

# 生产同款容器参数（nodeagent buildSpec 口径：只读根、带限 tmpfs、非 root、降权、no-new-privileges）
DOCKER_RUN=(docker run --rm --network bridge
  --read-only --tmpfs /tmp:size=8m --user 10001:10001
  --cap-drop ALL --security-opt no-new-privileges
  -e HOME=/tmp/b12home)

run_case() { # <combo> <script>
  local combo="$1" script="$2"
  local out rc
  out=$("${DOCKER_RUN[@]}" "$BASE_IMAGE" \
        sh -c "mkdir -p /tmp/b12home && $script" 2>&1)
  rc=$?  # r3：docker run 的退出码不吞——非零即 FAIL（容器/CLI 没跑成）
  if [ "$rc" -ne 0 ]; then
    report "$combo" FAIL "docker rc=$rc（$(printf '%s' "$out" | tail -1 | cut -c1-100)）"
    return
  fi
  judge "$combo" "$out"
}

if [ $# -eq 0 ]; then set -- codex-apikey codex-tokenfile; fi

for combo in "$@"; do
  case "$combo" in
    codex-apikey)
      # Codex API key 路径①：stdin 送 key（SPIKE：with-api-key 从 stdin 读）后非交互调用
      run_case codex-apikey "echo '$FAKE_KEY' | codex login --with-api-key 2>&1; codex exec --skip-git-repo-check 'ping' 2>&1 || true"
      ;;
    codex-tokenfile)
      # Codex 订阅令牌文件路径②：平台写入 auth.json 后直接用（无需任何 login）
      run_case codex-tokenfile "mkdir -p /tmp/b12home/.codex && printf '%s' '$FAKE_TOKEN_JSON' > /tmp/b12home/.codex/auth.json && chmod 600 /tmp/b12home/.codex/auth.json && codex exec --skip-git-repo-check 'ping' 2>&1 || true"
      ;;
    claude-apikey)
      # Claude Code：启动脚本读文件后进程内导出（B7：不经 docker env）——这里等价模拟
      run_case claude-apikey "printf '%s' '$FAKE_KEY' > /tmp/b12home/.k && export ANTHROPIC_API_KEY=\$(cat /tmp/b12home/.k) && claude -p 'ping' 2>&1 || true"
      ;;
    deepseek-settings)
      # DeepSeek：~/.deepcode/settings.json 的 env.API_KEY（不认环境变量——SPIKE 实测）
      run_case deepseek-settings "mkdir -p /tmp/b12home/.deepcode && printf '{\"env\":{\"API_KEY\":\"$FAKE_KEY\"}}' > /tmp/b12home/.deepcode/settings.json && deepcode 'ping' 2>&1 || true"
      ;;
    opencode-env)
      run_case opencode-env "printf '%s' '$FAKE_KEY' > /tmp/b12home/.k && export OPENAI_API_KEY=\$(cat /tmp/b12home/.k) && opencode run 'ping' 2>&1 || true"
      ;;
    *)
      echo "COMBO $combo SKIP 未知组合（验通脚本未收录——逐家验通制：没验过的组合不得上线）"
      fail=$((fail+1))
      ;;
  esac
done

if [ "$fail" -eq 0 ] && [ "$pass" -gt 0 ]; then echo "VERDICT PASS"; exit 0; else echo "VERDICT FAIL"; exit 1; fi
