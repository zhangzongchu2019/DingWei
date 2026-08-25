#!/bin/sh
# SP7a 收口闸 —— 仓储包外不得直接持有库句柄（架构设计 A§12 甲组第一条 / SpecDoc §22.2c 第一项）。
#
# 立此闸的直接原因（本席自查，非他审）：
#   判据「仓储包外 sql.Open 恒零」此前【没有任何闸执行】——
#   specs/evidence/collect.sh 只把数字记进证据，且它整篇没有一处非零退出；
#   scripts/speclint-gate.sh 也从没调用过它。
#   于是「恒为 0」只是文档里的一句话。收编做完之后，没有任何机制阻止它退回去。
#   ⇒ 这正是 lint-rule-must-have-teeth 里那一类：判据写了、数字也印了，就是没接进失败条件。
#
# 因此本闸的顺序是【先立闸、后收编】：基线从现状起（不假装已达标），
# 收一处降一处，回升即拒提交，最终钉死在 0。
#
# 用法：
#   scripts/repo-boundary-gate.sh              仓根执行，对本仓判定
#   scripts/repo-boundary-gate.sh --selftest   反测：证明本闸真能拦住违例
#   scripts/repo-boundary-gate.sh ROOT ALLOW BASE   （反测内部用，对任意树判定）
set -u

DEF_ALLOW=specs/gates/repo-packages.txt
DEF_BASE=specs/gates/db-handle-baseline.txt

# 判据按【效果】列，不按 driver 名字列。
# 立此写法的直接原因：证据脚本原来只数 sql.Open("pgx"|"sqlite") 两个字面 driver 名 ——
# 换个 driver 名、或改用 pgxpool/sqlx，数字照样是 0，而库句柄照样出了仓储包。
# 「按名字判非按效果判」是本席已归纳过的自造缺陷之一，此处不再犯。
# （今日实测：加宽后全仓仍为 8，与旧口径同值 —— 加宽没有偷改基线。）
PAT='(sql\.Open|sql\.OpenDB|sqlx\.(Open|Connect|MustConnect)|pgxpool\.(New|NewWithConfig|Connect)|pgx\.(Connect|ConnectConfig))\('

# hits ROOT ALLOWFILE  ->  逐行打印 允许清单外的命中（path:line:内容）
hits() {
  _root=$1; _allow=$2
  grep -rnE "$PAT" --include=*.go "$_root" 2>/dev/null \
    | sed "s|^$_root/||" \
    | grep -v '^vendor/' \
  | while IFS= read -r line; do
      _f=${line%%:*}
      _skip=0
      # 允许清单：前缀匹配。清单为空时无一命中被豁免（＝现状：仓储包尚不存在）。
      while IFS= read -r p; do
        case "$p" in ''|'#'*) continue;; esac
        case "$_f" in "$p"*) _skip=1; break;; esac
      done < "$_allow"
      [ "$_skip" -eq 0 ] && printf '%s\n' "$line"
    done
}

check() {
  _root=$1; _allow=$2; _basef=$3; _quiet=${4:-0}

  # 🔴 依赖缺失是【闸坏了】，不是【本次免检】。
  #    speclint-gate.sh 里已为同一条栽过一次（原写 exit 0），此处不重蹈。
  [ -d "$_root" ]    || { echo "✗ 闸依赖缺失：扫描根 $_root 不存在 —— 闸坏了，不是免检"; return 2; }
  [ -f "$_allow" ]   || { echo "✗ 闸依赖缺失：允许清单 $_allow 不存在 —— 无清单则无从判定「仓储包外」"; return 2; }
  [ -f "$_basef" ]   || { echo "✗ 闸依赖缺失：基线 $_basef 不存在 —— 无基线即无棘轮，不得放行"; return 2; }

  _base=$(tr -d ' \t\r\n' < "$_basef")
  case "$_base" in
    ''|*[!0-9]*) echo "✗ 闸依赖损坏：基线 $_basef 内容非自然数（读到：$_base）—— 不得放行"; return 2;;
  esac

  # 允许清单里的路径必须真实存在，否则说明清单已与代码漂移（改名 / 删包而清单没跟）。
  while IFS= read -r p; do
    case "$p" in ''|'#'*) continue;; esac
    [ -e "$_root/$p" ] || { echo "✗ 允许清单指向不存在的路径：$p —— 清单已漂移，闸坏了"; return 2; }
  done < "$_allow"

  _out=$(hits "$_root" "$_allow")
  if [ -z "$_out" ]; then _cur=0; else _cur=$(printf '%s\n' "$_out" | wc -l | tr -d ' '); fi

  if [ "$_quiet" -eq 0 ]; then
    echo "  仓储包允许清单（本次生效的全部条目）："
    _n=0
    while IFS= read -r p; do
      case "$p" in ''|'#'*) continue;; esac
      _n=$((_n+1)); echo "    - $p"
    done < "$_allow"
    # 每次都把清单整份印出来 —— 靠加清单把计数洗下去这条路，必须在日志里看得见。
    [ "$_n" -eq 0 ] && echo "    （空：仓储包尚未建立，故「仓储包外」＝全仓）"
    echo "  仓储包外的库句柄：$_cur 处（基线 $_base，判据终值 0）"
    [ -n "$_out" ] && printf '%s\n' "$_out" | sed 's/^/    /'
  fi

  RESULT_CUR=$_cur; RESULT_BASE=$_base
  [ "$_cur" -gt "$_base" ] && return 1
  return 0
}

# ---- 反测（--selftest）：不证明「闸跑了」，证明「闸拦得住」 ----
# 每一项都对应一种本席已经犯过、或这道闸最可能悄悄失效的形态。
selftest() {
  T=$(mktemp -d) || return 2
  fail=0; pass_n=0
  # 项数实算，不写死在结尾那句话里 —— 写死的数字与实际项数一分家，
  # 结尾那句「N 项全过」就成了本席刚在报告里点过的同一类缺陷（数字与产出两处不同源）。
  ok()  { pass_n=$((pass_n+1)); echo "  ✓ $1"; }
  bad() { echo "  ✗ 反测未过：$1"; fail=1; }

  mk() { # mk <目录> —— 造一棵干净的假树（0 命中）
    rm -rf "$T/r"; mkdir -p "$T/r/internal/pkg" "$T/r/internal/store"
    printf 'package pkg\n\nfunc F() {}\n'   > "$T/r/internal/pkg/a.go"
    printf 'package store\n\nfunc G() {}\n' > "$T/r/internal/store/s.go"
    : > "$T/allow"; printf '0\n' > "$T/base"
  }

  # ① 干净树必须判 0 —— 闸若恒报违例，后面几项就都不算数了。
  mk
  check "$T/r" "$T/allow" "$T/base" 1
  [ $? -eq 0 ] && [ "$RESULT_CUR" -eq 0 ] && ok "干净树判 0（闸不误报）" || bad "干净树被误判为 $RESULT_CUR"

  # ② 新增一处仓储包外的 sql.Open 必须被拦（这是本闸的主职）
  mk
  printf 'package pkg\n\nfunc H() { db, _ := sql.Open("sqlite", "x") ; _ = db }\n' > "$T/r/internal/pkg/b.go"
  check "$T/r" "$T/allow" "$T/base" 1
  [ $? -eq 1 ] && ok "拦住：仓储包外新增 sql.Open" || bad "仓储包外新增 sql.Open 未被拦"

  # ③ 换个 driver / 换个建连 API 也必须被拦 —— 按效果判，不按名字判。
  #    这一项针对的是旧口径只数 pgx|sqlite 两个字面名的漏洞。
  mk
  printf 'package pkg\n\nfunc H() { p, _ := pgxpool.New(ctx, "dsn") ; _ = p }\n' > "$T/r/internal/pkg/b.go"
  check "$T/r" "$T/allow" "$T/base" 1
  [ $? -eq 1 ] && ok "拦住：pgxpool.New（换 API 绕不过）" || bad "pgxpool.New 未被拦"
  mk
  printf 'package pkg\n\nfunc H() { db, _ := sql.Open("mysql", "dsn") ; _ = db }\n' > "$T/r/internal/pkg/b.go"
  check "$T/r" "$T/allow" "$T/base" 1
  [ $? -eq 1 ] && ok "拦住：sql.Open 换 driver 名（按效果判）" || bad "换 driver 名未被拦"

  # ④ _test.go 里的也必须计入 —— 本席已裁定判据含测试文件（较严的一侧）。
  #    不裁就会有两套口径：证据里 8、闸里 6，谁都说自己对。
  mk
  printf 'package pkg\n\nfunc TestX() { db, _ := sql.Open("pgx", "x") ; _ = db }\n' > "$T/r/internal/pkg/b_test.go"
  check "$T/r" "$T/allow" "$T/base" 1
  [ $? -eq 1 ] && ok "拦住：_test.go 内的库句柄（判据含测试文件）" || bad "_test.go 内的库句柄被漏过"

  # ⑤ 允许清单内的路径必须【不】计入 —— 否则收编做完了闸也过不了，闸会被绕过而不是被满足。
  mk
  printf 'package store\n\nfunc G() { db, _ := sql.Open("sqlite", "x") ; _ = db }\n' > "$T/r/internal/store/s.go"
  printf 'internal/store\n' > "$T/allow"
  check "$T/r" "$T/allow" "$T/base" 1
  [ $? -eq 0 ] && [ "$RESULT_CUR" -eq 0 ] && ok "豁免：允许清单内的仓储包不计入" || bad "允许清单未生效（仍判 $RESULT_CUR）"

  # ⑥ 基线缺失 / 非数字 必须判「闸坏了」，不得当成通过。
  #    这是第六种形态「没测却报通过」，speclint-gate.sh 已为它栽过一次。
  mk; rm -f "$T/base"
  check "$T/r" "$T/allow" "$T/base" 1 >/dev/null 2>&1
  [ $? -eq 2 ] && ok "基线缺失判为闸坏（非通过）" || bad "基线缺失被当成通过"
  mk; printf 'about eight\n' > "$T/base"
  check "$T/r" "$T/allow" "$T/base" 1 >/dev/null 2>&1
  [ $? -eq 2 ] && ok "基线非数字判为闸坏（非通过）" || bad "基线非数字被当成通过"

  # ⑦ 允许清单指向不存在的路径必须判「闸坏了」—— 清单与代码漂移时不许静悄悄放行。
  mk; printf 'internal/nosuchpkg\n' > "$T/allow"
  check "$T/r" "$T/allow" "$T/base" 1 >/dev/null 2>&1
  [ $? -eq 2 ] && ok "允许清单漂移判为闸坏（非通过）" || bad "允许清单漂移被放行"

  rm -rf "$T"
  if [ $fail -eq 0 ]; then echo "--- 反测 $pass_n 项全过（闸有牙）"; else echo "--- 反测未过（闸没牙，不得依赖它的 ✓）"; fi
  return $fail
}

case "${1:-}" in
  --selftest) selftest; exit $? ;;
esac

ROOT=${1:-.}; ALLOW=${2:-$DEF_ALLOW}; BASE=${3:-$DEF_BASE}
echo "⑦ 仓储包边界（SP7a 甲组·收口）"
check "$ROOT" "$ALLOW" "$BASE"
rc=$?
if [ $rc -eq 2 ]; then exit 1; fi
if [ $rc -eq 1 ]; then
  echo "✗ 仓储包外的库句柄由 $RESULT_BASE 升至 $RESULT_CUR —— 棘轮只许下行。"
  echo "  新代码请走仓储包；确需例外，改的是 $ALLOW（属评审面，非执笔方自决）。"
  exit 1
fi
if [ "$RESULT_CUR" -lt "$RESULT_BASE" ]; then
  # 降了就必须落盘。不落盘则下次可以悄悄退回到旧基线，棘轮等于没上。
  printf '%s\n' "$RESULT_CUR" > "$BASE"
  [ "$BASE" = "$DEF_BASE" ] && git add "$BASE" 2>/dev/null
  echo "  ⑦ 收编进展：$RESULT_BASE → $RESULT_CUR，基线已收紧并入暂存区"
elif [ "$RESULT_CUR" -eq 0 ]; then
  echo "  ⑦ 仓储包外库句柄恒零 ✓（甲组第一条达标；甲组只证收口，证不了隔离 —— 乙组四项另验）"
else
  echo "  ⑦ 持平 $RESULT_CUR ✓（未回升；判据终值为 0，尚未达标）"
fi
exit 0
