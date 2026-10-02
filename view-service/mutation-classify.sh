#!/usr/bin/env bash
# 变异结果分类器——把一次「带 MUTATION=<名> 的页面回归脚本运行」归成一类，只看【退出码＋输出标记】，不看人的印象。
# 约定全文见 runbooks/CONVENTION-MUTATION-EXIT-CODES-20261002.md（板）。适用脚本（env 形态 MUTATION=<名>）：
#   console-platform-admin / console-agent-mgmt-enroll / console-agent-mgmt-hosted / console-agent-groups-vega /
#   console-login / renderer-mention-candidates   （均在本目录）
#
# 用法：
#   view-service/mutation-classify.sh <脚本.js> <变异名>      # 单个：打印一个分类词
#   view-service/mutation-classify.sh --census <脚本.js>      # 全清单：逐个跑，末尾打印三数（RED／SURVIVED／INVALID）与其余
# 环境：NODE_PATH 指向含 playwright 的 node_modules；PW_CHANNEL=chrome 用系统 Chrome（与跑回归相同）。
#
# 分类（只认下面这些，其余一律 OTHER(rc=N)，不得当成红）：
#   RED           退出码 1，且输出里有 FAIL 行，且有 MUTATION-VERDICT: <名> KILLED   ＝变异被杀（我们要的）
#   SURVIVED      退出码 0，且有 MUTATION-VERDICT: <名> SURVIVED                      ＝变异存活（对照没守住——要补格）
#   INVALID       退出码 3，且 stderr 有 MUTATION-ANCHOR-STALE｜MUTATION-UNKNOWN｜MUTATION-SWITCH-WRONG-FORM
#                                                                              ＝变异【没打上】，什么也没证明；不是红，也不是存活
#   NOT-ISOLATED  退出码 5（仅 vega：变异必须只让一条断言失败，却失败了多条）          ＝不算红，要整理夹具
#   NO-VERDICT    退出码 0 却没有 MUTATION-VERDICT 行 ＝ 变异根本没生效（例如用错开关形态）；按无效处理，绝不能当「存活」或「通过」
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"

classify_one() { # $1 脚本 $2 变异名
  local out rc
  out="$(mktemp)"
  ( cd "$HERE" && MUTATION="$2" node "$1" >"$out" 2>&1 ); rc=$?
  if   [ $rc = 3 ] && grep -qE 'MUTATION-(ANCHOR-STALE|UNKNOWN|SWITCH-WRONG-FORM)' "$out"; then echo INVALID
  elif [ $rc = 5 ] && grep -q 'MUTATION-NOT-ISOLATED' "$out"; then echo NOT-ISOLATED
  elif [ $rc = 1 ] && grep -q '^FAIL' "$out" && grep -q "MUTATION-VERDICT: $2 KILLED" "$out"; then echo RED
  elif [ $rc = 0 ] && grep -q "MUTATION-VERDICT: $2 SURVIVED" "$out"; then echo SURVIVED
  elif [ $rc = 0 ]; then echo NO-VERDICT
  else echo "OTHER(rc=$rc)"; fi
  rm -f "$out"
}

names_of() { # 变异清单 = MUTATIONS 数组（若有）∪ 脚本里所有 `mutation === '<名>'`
  node -e "
const s=require('fs').readFileSync('$HERE/$1','utf8');const set=new Set();
const m=s.match(/const MUTATIONS = \[([^\]]*)\]/s);if(m)for(const x of m[1].matchAll(/'([^']+)'/g))set.add(x[1]);
for(const x of s.matchAll(/mutation === '([a-z0-9-]+)'/g))set.add(x[1]);
console.log([...set].join(' '));"
}

if [ "${1:-}" = "--census" ]; then
  script="${2:?usage: --census <script.js>}"
  red=0; surv=0; inv=0; other=0; selftest="MISSING"
  for m in $(names_of "$script"); do
    c="$(classify_one "$script" "$m")"
    if [ "$m" = "mutation-anchor-selftest" ]; then [ "$c" = INVALID ] && selftest=OK || selftest="BROKEN($c)"; continue; fi
    echo "$m $c"
    case "$c" in RED) red=$((red+1));; SURVIVED) surv=$((surv+1));; INVALID) inv=$((inv+1));; *) other=$((other+1));; esac
  done
  echo "CENSUS $script RED=$red SURVIVED=$surv INVALID=$inv OTHER=$other selftest=$selftest"
else
  classify_one "${1:?usage: mutation-classify.sh <script.js> <mutation>}" "${2:?mutation name}"
fi
