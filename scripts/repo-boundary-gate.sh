#!/bin/sh
# SP7a 收口闸 —— 仓储包边界（架构设计 A§12 甲组 / SpecDoc §22.2c #1）。
#
# 立此闸的直接原因（本席自查）：
#   判据「仓储包外 sql.Open 恒零」此前【没有任何闸执行】——
#   specs/evidence/collect.sh 只把数字记进证据、整篇无一处非零退出，speclint-gate.sh 也没调过它。
#   判据不接进失败条件，等于没有判据。故 SP7a 顺序为【先立闸、后收编】。
#
# 🔴 v2（同日补，仍在 SP7a 开工当天）：v1 只数 sql.Open，漏了 §22.2c #1 的【后半句】。
#   该条判据原文是「*sql.DB / *pgx.Conn 不导出、**任何函数不返回它**」—— 两半。
#   v1 只实现前半句，于是 board.go 的 `func (b *Board) DB() *sql.DB`（12 处调用方）
#   一个字都拦不住：sql.Open 收到 0 的同时，裸句柄照样递给包外任何人。
#   ⇒ 指标达标而收口未达成，即「安全属性互相冒充」。
#   教训与今天早些时候「用单一指标推整个作用域」是同一条：**判据有几半，闸就得有几半。**
#
# 用法：
#   scripts/repo-boundary-gate.sh              仓根执行
#   scripts/repo-boundary-gate.sh --selftest   反测：证明本闸拦得住
#   scripts/repo-boundary-gate.sh ROOT ALLOW BASE   （反测内部用）
set -u

DEF_ALLOW=specs/gates/repo-packages.txt
DEF_BASE=specs/gates/boundary-baseline.txt

# 判据按【效果】列，不按 driver 名列 —— 换 driver 名或改用 pgxpool/sqlx 都得拦得住。
# 「按名字判非按效果判」是已归纳的自造缺陷之一。
PAT_OPEN='(sql\.Open|sql\.OpenDB|sqlx\.(Open|Connect|MustConnect)|pgxpool\.(New|NewWithConfig|Connect)|pgx\.(Connect|ConnectConfig))\('
# 裸库句柄类型（用于「逃逸」判据）
H='\*(sql\.(DB|Tx|Conn)|pgx\.(Conn|Tx)|pgxpool\.(Pool|Conn))'
# 逃逸两形态：① 导出函数/方法的返回值里出现裸句柄 ② 导出字段持有裸句柄
PAT_ESC_FUNC="^func (\([^)]+\) )?[A-Z][A-Za-z0-9_]*\([^)]*\)[^{]*$H"
PAT_ESC_FIELD="^[[:space:]]+[A-Z][A-Za-z0-9_]*[[:space:]]+$H"

_scan() { # _scan ROOT PATTERN  -> path:line:内容（已剥 root 前缀、已去 vendor）
  grep -rnE "$2" --include=*.go "$1" 2>/dev/null | sed "s|^$1/||" | grep -v '^vendor/'
}

_drop_allowed() { # 从标准输入滤掉允许清单命中的行
  _allow=$1
  while IFS= read -r line; do
    _f=${line%%:*}; _skip=0
    while IFS= read -r p; do
      case "$p" in ''|'#'*) continue;; esac
      case "$_f" in "$p"*) _skip=1; break;; esac
    done < "$_allow"
    [ "$_skip" -eq 0 ] && printf '%s\n' "$line"
  done
}

_count() { [ -z "$1" ] && echo 0 || printf '%s\n' "$1" | wc -l | tr -d ' '; }

_baseval() { # _baseval BASEFILE KEY -> 值；缺键回 ABSENT
  awk -v k="$2" '$1==k && $0 !~ /^#/ {print $2; found=1} END{if(!found) print "ABSENT"}' "$1"
}

check() {
  _root=$1; _allow=$2; _basef=$3; _quiet=${4:-0}

  # 🔴 依赖缺失是【闸坏了】，不是【本次免检】。speclint-gate.sh 已为同一条栽过一次。
  [ -d "$_root" ]  || { echo "✗ 闸依赖缺失：扫描根 $_root 不存在 —— 闸坏了，不是免检"; return 2; }
  [ -f "$_allow" ] || { echo "✗ 闸依赖缺失：允许清单 $_allow 不存在"; return 2; }
  [ -f "$_basef" ] || { echo "✗ 闸依赖缺失：基线 $_basef 不存在 —— 无基线即无棘轮，不得放行"; return 2; }

  for k in open escape; do
    v=$(_baseval "$_basef" "$k")
    case "$v" in
      ABSENT) echo "✗ 闸依赖损坏：基线缺键 $k —— 判据少一条即少一半，不得放行"; return 2;;
      ''|*[!0-9]*) echo "✗ 闸依赖损坏：基线键 $k 的值非自然数（读到：$v）"; return 2;;
    esac
    eval "BASE_$k=\$v"
  done

  # 🔴 口径已由业主钉死为【严口径：含 _test.go】（coordinator 转达 2026-08-25T01:07:11Z）。
  #    「不设生产码/测试双值，不留 INCLUDE_TESTS 逃生开关 —— 开关本身就是第二套口径。」
  #    本席先前为等裁定做过一个可切换项，裁定一到即拆。此处立判据防它回潮：
  #    基线里一旦再出现这三个键，即判【闸坏了】，不是「多了个键不要紧」。
  for k in include_tests open_prod open_all; do
    if [ "$(_baseval "$_basef" "$k")" != "ABSENT" ]; then
      echo "✗ 基线出现已被裁撤的键 $k —— 业主已裁严口径、不设双值不留开关，第二套口径不得回潮"; return 2
    fi
  done

  while IFS= read -r p; do
    case "$p" in ''|'#'*) continue;; esac
    [ -e "$_root/$p" ] || { echo "✗ 允许清单指向不存在的路径：$p —— 清单已漂移，闸坏了"; return 2; }
  done < "$_allow"

  # 判据一：仓储包外直接建库句柄 —— 允许清单豁免（清单内就是仓储包，它本来就该开库）
  OUT_open=$(_scan "$_root" "$PAT_OPEN" | _drop_allowed "$_allow")
  # 判据二：裸库句柄逃出包外 —— 🔴 允许清单【不】豁免。
  #   仓储包自己导出一个 DB() 访问器，正是「句柄出了仓储包」本身；豁免它等于把这条判据豁免掉。
  OUT_escape=$( { _scan "$_root" "$PAT_ESC_FUNC"; _scan "$_root" "$PAT_ESC_FIELD"; } | sort -u )
  CUR_open=$(_count "$OUT_open"); CUR_escape=$(_count "$OUT_escape")

  if [ "$_quiet" -eq 0 ]; then
    echo "  仓储包允许清单（本次生效的全部条目）："
    _n=0
    while IFS= read -r p; do
      case "$p" in ''|'#'*) continue;; esac
      _n=$((_n+1)); echo "    - $p"
    done < "$_allow"
    # 整份打印 —— 靠往清单里加路径把计数洗下去这条路，必须在日志里看得见。
    [ "$_n" -eq 0 ] && echo "    （空：仓储包尚未建立，故「仓储包外」＝全仓）"
    echo "  判据一·仓储包外建库句柄：$CUR_open 处（基线 $BASE_open，终值 0）"
    [ -n "$OUT_open" ] && printf '%s\n' "$OUT_open" | sed 's/^/    /'
    echo "  判据二·裸库句柄逃出包外：$CUR_escape 处（基线 $BASE_escape，终值 0；允许清单对本条不豁免）"
    [ -n "$OUT_escape" ] && printf '%s\n' "$OUT_escape" | sed 's/^/    /'
  fi

  # 🔴 两条判据【各判各的】，不看和 —— 合成一个总数就允许「一个降一个升」蒙混过关。
  rc=0
  [ "$CUR_open"   -gt "$BASE_open" ]   && rc=1
  [ "$CUR_escape" -gt "$BASE_escape" ] && rc=1
  return $rc
}

# ---- 反测（--selftest）：不证明「闸跑了」，证明「闸拦得住」 ----
selftest() {
  T=$(mktemp -d) || return 2
  fail=0; pass_n=0
  # 项数由通过计数实算，不写死在结尾那句话里 —— 写死的数字与实际项数一分家，
  # 结尾那句「N 项全过」就成了本席刚点过的同一类缺陷（数字与产出两处不同源）。
  ok()  { pass_n=$((pass_n+1)); echo "  ✓ $1"; }
  bad() { echo "  ✗ 反测未过：$1"; fail=1; }

  mk() { # 造一棵干净的假树（两条判据均 0）
    rm -rf "$T/r"; mkdir -p "$T/r/internal/pkg" "$T/r/internal/store"
    printf 'package pkg\n\nfunc F() {}\n'   > "$T/r/internal/pkg/a.go"
    printf 'package store\n\nfunc G() {}\n' > "$T/r/internal/store/s.go"
    : > "$T/allow"; printf 'open 0\nescape 0\n' > "$T/base"
  }
  run() { check "$T/r" "$T/allow" "$T/base" 1; }

  # ① 干净树两条判据都判 0 —— 闸若恒报违例，后面全部不算数
  mk; run
  [ $? -eq 0 ] && [ "$CUR_open" -eq 0 ] && [ "$CUR_escape" -eq 0 ] \
    && ok "干净树两条判据均判 0（闸不误报）" || bad "干净树被误判 open=$CUR_open escape=$CUR_escape"

  # ② 判据一：仓储包外新增建库
  mk; printf 'package pkg\n\nfunc H() { db, _ := sql.Open("sqlite", "x"); _ = db }\n' > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 1 ] && ok "拦住：仓储包外新增 sql.Open" || bad "仓储包外新增 sql.Open 未被拦"

  # ③ 判据一：换 API / 换 driver 名都得拦（按效果判，不按名字判）
  mk; printf 'package pkg\n\nfunc H() { p, _ := pgxpool.New(ctx, "dsn"); _ = p }\n' > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 1 ] && ok "拦住：pgxpool.New（换 API 绕不过）" || bad "pgxpool.New 未被拦"
  mk; printf 'package pkg\n\nfunc H() { db, _ := sql.Open("mysql", "dsn"); _ = db }\n' > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 1 ] && ok "拦住：sql.Open 换 driver 名" || bad "换 driver 名未被拦"

  # ④ 判据一：_test.go 计入（本席已裁定取较严一侧，避免证据与闸两套口径）
  mk; printf 'package pkg\n\nfunc TestX() { db, _ := sql.Open("pgx", "x"); _ = db }\n' > "$T/r/internal/pkg/b_test.go"
  run; [ $? -eq 1 ] && ok "拦住：_test.go 内的建库（判据含测试文件）" || bad "_test.go 内的建库被漏过"

  # ⑤ 判据二：导出方法返回裸句柄 —— 这正是 v1 漏掉、board.DB() 钻过去的那半句
  mk; printf 'package pkg\n\ntype B struct{ db *sql.DB }\n\nfunc (b *B) DB() *sql.DB { return b.db }\n' > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 1 ] && ok "拦住：导出方法返回 *sql.DB（v1 的漏洞）" || bad "导出方法返回 *sql.DB 未被拦"

  # ⑥ 判据二：多返回值形态也得拦（(*sql.DB, error) 是最常见的写法）
  mk; printf 'package pkg\n\nfunc Handle() (*sql.DB, error) { return nil, nil }\n' > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 1 ] && ok "拦住：导出函数返回 (*sql.DB, error)" || bad "多返回值形态未被拦"

  # ⑦ 判据二：导出字段持裸句柄
  mk; printf 'package pkg\n\ntype B struct {\n\tDB *sql.DB\n}\n' > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 1 ] && ok "拦住：导出字段持 *sql.DB" || bad "导出字段持句柄未被拦"

  # ⑧ 判据二不得误伤【未导出】的包内用法 —— 句柄留在包内不算逃逸，
  #    误伤会逼人把仓储包写不出来，闸就会被绕过而不是被满足。
  mk; printf 'package pkg\n\ntype b struct{ db *sql.DB }\n\nfunc (x *b) handle() *sql.DB { return x.db }\n\nfunc use(tx *sql.Tx) {}\n' > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 0 ] && [ "$CUR_escape" -eq 0 ] && ok "不误伤：未导出的包内句柄用法（含 *sql.Tx 作参数）" || bad "误伤包内未导出用法（escape=$CUR_escape）"

  # ⑨ 允许清单豁免判据一
  mk; printf 'package store\n\nfunc G() { db, _ := sql.Open("sqlite", "x"); _ = db }\n' > "$T/r/internal/store/s.go"
  printf 'internal/store\n' > "$T/allow"
  run; [ $? -eq 0 ] && [ "$CUR_open" -eq 0 ] && ok "豁免：允许清单内的建库不计入判据一" || bad "允许清单对判据一未生效"

  # ⑩ 🔴 允许清单【不】豁免判据二 —— 否则把仓储包加进清单，DB() 访问器就地合法化，
  #    判据二等于被清单豁免掉了。这一项是本条设计的骨。
  mk; printf 'package store\n\ntype B struct{ db *sql.DB }\n\nfunc (b *B) DB() *sql.DB { return b.db }\n' > "$T/r/internal/store/s.go"
  printf 'internal/store\n' > "$T/allow"
  run; [ $? -eq 1 ] && ok "允许清单【不】豁免判据二（仓储包也不许导出句柄）" || bad "允许清单把判据二一起豁免了"

  # ⑪ 🔴 两条判据不得合并看总数：一降一升必须拒。
  #    合并成一个总数就允许「收了一处建库、同时新开一个 DB() 访问器」而总数不变地过闸。
  mk; printf 'open 1\nescape 0\n' > "$T/base"
  printf 'package pkg\n\ntype B struct{ db *sql.DB }\n\nfunc (b *B) DB() *sql.DB { return b.db }\n' > "$T/r/internal/pkg/b.go"
  run  # open 实为 0（降），escape 实为 1（升）；若看总数则 1 vs 1 持平会放行
  [ $? -eq 1 ] && ok "一降一升必拒（两条判据各判各的，不看总和）" || bad "一降一升被放行 —— 判据被合并了"

  # ⑫ 基线缺失 / 非数字 / 缺键 须判「闸坏了」，不得当成通过（第六种失效形态）
  mk; rm -f "$T/base"; run >/dev/null 2>&1
  [ $? -eq 2 ] && ok "基线缺失判为闸坏（非通过）" || bad "基线缺失被当成通过"
  mk; printf 'open eight\nescape 0\n' > "$T/base"; run >/dev/null 2>&1
  [ $? -eq 2 ] && ok "基线值非数字判为闸坏（非通过）" || bad "基线非数字被当成通过"
  mk; printf 'open 0\n' > "$T/base"; run >/dev/null 2>&1
  [ $? -eq 2 ] && ok "基线缺 escape 键判为闸坏（判据少一半即不得放行）" || bad "基线缺键被当成通过"

  # ⑫a 已裁撤的开关键若回潮，须判「闸坏了」——业主裁定「不留逃生开关」要有牙，
  #     否则谁都可以再加一个 include_tests 回来，闸照旧打印通过。
  for k in include_tests open_prod open_all; do
    mk; printf "open 0\nescape 0\n$k 0\n" > "$T/base"; run >/dev/null 2>&1
    [ $? -eq 2 ] && ok "拦住：已裁撤的键 $k 回潮（判闸坏，非通过）" || bad "$k 回潮被放行"
  done

  # ⑬ 允许清单漂移须判「闸坏了」
  mk; printf 'internal/nosuchpkg\n' > "$T/allow"; run >/dev/null 2>&1
  [ $? -eq 2 ] && ok "允许清单漂移判为闸坏（非通过）" || bad "允许清单漂移被放行"

  rm -rf "$T"
  if [ $fail -eq 0 ]; then echo "--- 反测 $pass_n 项全过（闸有牙）"; else echo "--- 反测未过（闸没牙，不得依赖它的 ✓）"; fi
  return $fail
}

case "${1:-}" in --selftest) selftest; exit $? ;; esac

ROOT=${1:-.}; ALLOW=${2:-$DEF_ALLOW}; BASE=${3:-$DEF_BASE}
echo "⑦ 仓储包边界（SP7a 甲组·收口 / §22.2c #1 两半）"
check "$ROOT" "$ALLOW" "$BASE"
rc=$?
[ $rc -eq 2 ] && exit 1
if [ $rc -eq 1 ]; then
  [ "$CUR_open"   -gt "$BASE_open" ]   && echo "✗ 判据一由 $BASE_open 升至 $CUR_open —— 棘轮只许下行。新代码请走仓储包。"
  [ "$CUR_escape" -gt "$BASE_escape" ] && echo "✗ 判据二由 $BASE_escape 升至 $CUR_escape —— 裸库句柄不得逃出包外（§22.2c #1）。"
  echo "  确需例外，改的是 $ALLOW（属评审面，非执笔方自决）；且它对判据二不豁免。"
  exit 1
fi
# 降了必须落盘。不落盘则下次可悄悄退回旧基线，棘轮等于没上。
moved=0
if [ "$CUR_open" -lt "$BASE_open" ] || [ "$CUR_escape" -lt "$BASE_escape" ]; then
  awk -v o="$CUR_open" -v e="$CUR_escape" '
    /^#/ || NF==0 {print; next}
    $1=="open"   {print "open " o; next}
    $1=="escape" {print "escape " e; next}
    {print}' "$BASE" > "$BASE.tmp" && mv "$BASE.tmp" "$BASE"
  [ "$BASE" = "$DEF_BASE" ] && git add "$BASE" 2>/dev/null
  echo "  ⑦ 收编进展：建库 $BASE_open→$CUR_open · 逃逸 $BASE_escape→$CUR_escape，基线已收紧并入暂存区"
  moved=1
fi
if [ "$moved" -eq 0 ]; then
  if [ "$CUR_open" -eq 0 ] && [ "$CUR_escape" -eq 0 ]; then
    echo "  ⑦ 两条判据均恒零 ✓（甲组收口达标；甲组只证收口证不了隔离 —— 乙组四项另验）"
  else
    echo "  ⑦ 持平（建库 $CUR_open · 逃逸 $CUR_escape）✓ 未回升；终值均为 0，尚未达标"
  fi
fi
exit 0
