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

  for k in open_prod open_all escape; do
    v=$(_baseval "$_basef" "$k")
    case "$v" in
      ABSENT) echo "✗ 闸依赖损坏：基线缺键 $k —— 判据少一条即少一半，不得放行"; return 2;;
      ''|*[!0-9]*) echo "✗ 闸依赖损坏：基线键 $k 的值非自然数（读到：$v）"; return 2;;
    esac
    eval "BASE_$k=\$v"
  done
  # 「扫不扫 _test.go」是可切换项（coordinator 2026-08-25 归口，业主尚未落定）。
  # 🔴 开关只决定【哪个计数是达标判据】，不决定【哪个计数被监视】——
  #    能被悄悄拨到让闸通过的开关，就是没牙的闸。故未裁期间两个计数都棘轮。
  INCTESTS=$(_baseval "$_basef" include_tests)
  case "$INCTESTS" in
    undecided|yes|no) ;;
    ABSENT) echo "✗ 闸依赖损坏：基线缺键 include_tests —— 判据口径未声明，不得放行"; return 2;;
    *) echo "✗ 闸依赖损坏：include_tests 只能是 undecided/yes/no（读到：$INCTESTS）"; return 2;;
  esac

  while IFS= read -r p; do
    case "$p" in ''|'#'*) continue;; esac
    [ -e "$_root/$p" ] || { echo "✗ 允许清单指向不存在的路径：$p —— 清单已漂移，闸坏了"; return 2; }
  done < "$_allow"

  # 判据一：仓储包外直接建库句柄 —— 允许清单豁免（清单内就是仓储包，它本来就该开库）
  OUT_open_all=$(_scan "$_root" "$PAT_OPEN" | _drop_allowed "$_allow")
  OUT_open_prod=$(printf '%s\n' "$OUT_open_all" | grep -v '_test\.go:' || true)
  # 判据二：裸库句柄逃出包外 —— 🔴 允许清单【不】豁免。
  #   仓储包自己导出一个 DB() 访问器，正是「句柄出了仓储包」本身；豁免它等于把这条判据豁免掉。
  OUT_escape=$( { _scan "$_root" "$PAT_ESC_FUNC"; _scan "$_root" "$PAT_ESC_FIELD"; } | sort -u )
  CUR_open_all=$(_count "$OUT_open_all"); CUR_open_prod=$(_count "$OUT_open_prod")
  CUR_escape=$(_count "$OUT_escape")

  if [ "$_quiet" -eq 0 ]; then
    echo "  仓储包允许清单（本次生效的全部条目）："
    _n=0
    while IFS= read -r p; do
      case "$p" in ''|'#'*) continue;; esac
      _n=$((_n+1)); echo "    - $p"
    done < "$_allow"
    # 整份打印 —— 靠往清单里加路径把计数洗下去这条路，必须在日志里看得见。
    [ "$_n" -eq 0 ] && echo "    （空：仓储包尚未建立，故「仓储包外」＝全仓）"
    case "$INCTESTS" in
      undecided) echo "  🔴 include_tests=undecided —— 业主尚未裁定判据含不含 _test.go（coordinator 归口回问中）。";
                 echo "     未裁期间两个计数【都棘轮】，取保守侧；此行不会自己消失，裁定落定前每次提交都会看见它。";;
      yes)       echo "  include_tests=yes —— 达标判据为 open_all（含测试文件）";;
      no)        echo "  include_tests=no —— 达标判据为 open_prod；open_all 转参考值，回升不再拒";;
    esac
    echo "  判据一·仓储包外建库句柄：生产码 $CUR_open_prod（基线 $BASE_open_prod） · 含测试 $CUR_open_all（基线 $BASE_open_all） · 终值均 0"
    [ -n "$OUT_open_all" ] && printf '%s\n' "$OUT_open_all" | sed 's/^/    /'
    echo "  判据二·裸库句柄逃出包外：$CUR_escape 处（基线 $BASE_escape，终值 0；允许清单对本条不豁免）"
    [ -n "$OUT_escape" ] && printf '%s\n' "$OUT_escape" | sed 's/^/    /'
  fi

  # 🔴 两条判据【各判各的】，不看和 —— 合成一个总数就允许「一个降一个升」蒙混过关。
  rc=0
  [ "$CUR_open_prod" -gt "$BASE_open_prod" ] && rc=1
  # open_all 只在【非 no】时接进失败条件：业主若裁「不含测试」，新增集成测试自建库应放行。
  if [ "$INCTESTS" != "no" ] && [ "$CUR_open_all" -gt "$BASE_open_all" ]; then rc=1; fi
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
    : > "$T/allow"; printf 'include_tests undecided\nopen_prod 0\nopen_all 0\nescape 0\n' > "$T/base"
  }
  run() { check "$T/r" "$T/allow" "$T/base" 1; }

  # ① 干净树两条判据都判 0 —— 闸若恒报违例，后面全部不算数
  mk; run
  [ $? -eq 0 ] && [ "$CUR_open_all" -eq 0 ] && [ "$CUR_open_prod" -eq 0 ] && [ "$CUR_escape" -eq 0 ] \
    && ok "干净树三个计数均判 0（闸不误报）" || bad "干净树被误判 prod=$CUR_open_prod all=$CUR_open_all escape=$CUR_escape"

  # ② 判据一：仓储包外新增建库
  mk; printf 'package pkg\n\nfunc H() { db, _ := sql.Open("sqlite", "x"); _ = db }\n' > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 1 ] && ok "拦住：仓储包外新增 sql.Open" || bad "仓储包外新增 sql.Open 未被拦"

  # ③ 判据一：换 API / 换 driver 名都得拦（按效果判，不按名字判）
  mk; printf 'package pkg\n\nfunc H() { p, _ := pgxpool.New(ctx, "dsn"); _ = p }\n' > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 1 ] && ok "拦住：pgxpool.New（换 API 绕不过）" || bad "pgxpool.New 未被拦"
  mk; printf 'package pkg\n\nfunc H() { db, _ := sql.Open("mysql", "dsn"); _ = db }\n' > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 1 ] && ok "拦住：sql.Open 换 driver 名" || bad "换 driver 名未被拦"

  # ④ 开关三态 —— 「扫不扫 _test.go」是可切换项(coordinator 归口，业主未落定)。
  #    🔴 反测要证明的不是「开关能拨」，而是【拨到哪一档都不会出现无人看守的缺口】。
  TESTOPEN='package pkg\n\nfunc TestX() { db, _ := sql.Open("pgx", "x"); _ = db }\n'
  PRODOPEN='package pkg\n\nfunc H() { db, _ := sql.Open("pgx", "x"); _ = db }\n'

  # ④a undecided：测试码新增建库也拒（保守侧 —— 未裁期间不许任何新口子进来）
  mk; printf "$TESTOPEN" > "$T/r/internal/pkg/b_test.go"
  run; [ $? -eq 1 ] && ok "undecided：拦住 _test.go 内新增建库（保守侧）" || bad "undecided 下测试码新增建库被放行"

  # ④b yes：同样拒
  mk; printf 'include_tests yes\nopen_prod 0\nopen_all 0\nescape 0\n' > "$T/base"
  printf "$TESTOPEN" > "$T/r/internal/pkg/b_test.go"
  run; [ $? -eq 1 ] && ok "yes：拦住 _test.go 内新增建库" || bad "yes 下测试码新增建库被放行"

  # ④c no：测试码放行（这才是开关的用处：业主若裁不含测试，新增集成测试不该被闸挡）
  mk; printf 'include_tests no\nopen_prod 0\nopen_all 0\nescape 0\n' > "$T/base"
  printf "$TESTOPEN" > "$T/r/internal/pkg/b_test.go"
  run; [ $? -eq 0 ] && ok "no：放行 _test.go 内新增建库（开关确实生效）" || bad "no 下测试码新增建库仍被拒"

  # ④d 🔴 no 也【不得】放行生产码 —— 开关只松测试这一维，不许连生产码一起松开。
  #     这一项是开关设计的骨：拨到最松的一档，生产码仍须被看住。
  mk; printf 'include_tests no\nopen_prod 0\nopen_all 0\nescape 0\n' > "$T/base"
  printf "$PRODOPEN" > "$T/r/internal/pkg/b.go"
  run; [ $? -eq 1 ] && ok "no：仍拦住生产码新增建库（开关不连带松生产码）" || bad "no 把生产码一起放行了"

  # ④e include_tests 非法值 / 缺键 -> 判闸坏，不得当成通过
  mk; printf 'include_tests maybe\nopen_prod 0\nopen_all 0\nescape 0\n' > "$T/base"
  run >/dev/null 2>&1; [ $? -eq 2 ] && ok "include_tests 非法值判闸坏（非通过）" || bad "include_tests 非法值被当成通过"
  mk; printf 'open_prod 0\nopen_all 0\nescape 0\n' > "$T/base"
  run >/dev/null 2>&1; [ $? -eq 2 ] && ok "include_tests 缺键判闸坏（口径未声明不得放行）" || bad "include_tests 缺键被当成通过"

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
  run; [ $? -eq 0 ] && [ "$CUR_open_all" -eq 0 ] && ok "豁免：允许清单内的建库不计入判据一" || bad "允许清单对判据一未生效（all=$CUR_open_all）"

  # ⑩ 🔴 允许清单【不】豁免判据二 —— 否则把仓储包加进清单，DB() 访问器就地合法化，
  #    判据二等于被清单豁免掉了。这一项是本条设计的骨。
  mk; printf 'package store\n\ntype B struct{ db *sql.DB }\n\nfunc (b *B) DB() *sql.DB { return b.db }\n' > "$T/r/internal/store/s.go"
  printf 'internal/store\n' > "$T/allow"
  run; [ $? -eq 1 ] && ok "允许清单【不】豁免判据二（仓储包也不许导出句柄）" || bad "允许清单把判据二一起豁免了"

  # ⑪ 🔴 两条判据不得合并看总数：一降一升必须拒。
  #    合并成一个总数就允许「收了一处建库、同时新开一个 DB() 访问器」而总数不变地过闸。
  mk; printf 'include_tests undecided\nopen_prod 1\nopen_all 1\nescape 0\n' > "$T/base"
  printf 'package pkg\n\ntype B struct{ db *sql.DB }\n\nfunc (b *B) DB() *sql.DB { return b.db }\n' > "$T/r/internal/pkg/b.go"
  run  # open 实为 0（降），escape 实为 1（升）；若看总数则 1 vs 1 持平会放行
  [ $? -eq 1 ] && ok "一降一升必拒（两条判据各判各的，不看总和）" || bad "一降一升被放行 —— 判据被合并了"

  # ⑫ 基线缺失 / 非数字 / 缺键 须判「闸坏了」，不得当成通过（第六种失效形态）
  mk; rm -f "$T/base"; run >/dev/null 2>&1
  [ $? -eq 2 ] && ok "基线缺失判为闸坏（非通过）" || bad "基线缺失被当成通过"
  mk; printf 'include_tests undecided\nopen_prod eight\nopen_all 8\nescape 0\n' > "$T/base"; run >/dev/null 2>&1
  [ $? -eq 2 ] && ok "基线值非数字判为闸坏（非通过）" || bad "基线非数字被当成通过"
  mk; printf 'include_tests undecided\nopen_prod 0\nopen_all 0\n' > "$T/base"; run >/dev/null 2>&1
  [ $? -eq 2 ] && ok "基线缺 escape 键判为闸坏（判据少一半即不得放行）" || bad "基线缺键被当成通过"

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
  [ "$CUR_open_prod" -gt "$BASE_open_prod" ] && echo "✗ 判据一(生产码)由 $BASE_open_prod 升至 $CUR_open_prod —— 棘轮只许下行。新代码请走仓储包。"
  if [ "$INCTESTS" != "no" ] && [ "$CUR_open_all" -gt "$BASE_open_all" ]; then
    echo "✗ 判据一(含测试)由 $BASE_open_all 升至 $CUR_open_all —— 口径未裁期间取保守侧，测试码新增建库同样拒。"
    echo "  若业主裁定不含测试，把 $BASE 的 include_tests 改为 no 即放行（改判据属评审面，非执笔方自决）。"
  fi
  [ "$CUR_escape" -gt "$BASE_escape" ] && echo "✗ 判据二由 $BASE_escape 升至 $CUR_escape —— 裸库句柄不得逃出包外（§22.2c #1）。"
  echo "  确需例外，改的是 $ALLOW（属评审面）；且它对判据二不豁免。"
  exit 1
fi
# 降了必须落盘。不落盘则下次可悄悄退回旧基线，棘轮等于没上。
# 三个计数一律只下行：即便 open_all 当前不接失败条件（include_tests=no），
# 记录它的改善也无害，而记录它的回升才是危险的 —— 故只写更小值。
NP=$BASE_open_prod; NA=$BASE_open_all; NE=$BASE_escape; moved=0
[ "$CUR_open_prod" -lt "$BASE_open_prod" ] && { NP=$CUR_open_prod; moved=1; }
[ "$CUR_open_all"  -lt "$BASE_open_all"  ] && { NA=$CUR_open_all;  moved=1; }
[ "$CUR_escape"    -lt "$BASE_escape"    ] && { NE=$CUR_escape;    moved=1; }
if [ "$moved" -eq 1 ]; then
  awk -v p="$NP" -v a="$NA" -v e="$NE" '
    /^#/ || NF==0 {print; next}
    $1=="open_prod" {print "open_prod " p; next}
    $1=="open_all"  {print "open_all "  a; next}
    $1=="escape"    {print "escape "    e; next}
    {print}' "$BASE" > "$BASE.tmp" && mv "$BASE.tmp" "$BASE"
  [ "$BASE" = "$DEF_BASE" ] && git add "$BASE" 2>/dev/null
  echo "  ⑦ 收编进展：生产码 $BASE_open_prod→$NP · 含测试 $BASE_open_all→$NA · 逃逸 $BASE_escape→$NE，基线已收紧并入暂存区"
  exit 0
fi
# 达标宣布 —— 🔴 口径未裁时【不得宣布达标】，哪怕数字已经是 0。
case "$INCTESTS" in
  yes) DONE_N=$CUR_open_all;  DONE_W="含测试" ;;
  no)  DONE_N=$CUR_open_prod; DONE_W="生产码" ;;
  *)   DONE_N=-1 ;;
esac
if [ "$DONE_N" = "0" ] && [ "$CUR_escape" -eq 0 ]; then
  echo "  ⑦ 两条判据均恒零 ✓（按 include_tests=$INCTESTS 的 $DONE_W 口径；甲组只证收口证不了隔离 —— 乙组四项另验）"
elif [ "$INCTESTS" = "undecided" ] && [ "$CUR_open_prod" -eq 0 ] && [ "$CUR_open_all" -eq 0 ] && [ "$CUR_escape" -eq 0 ]; then
  echo "  ⑦ 三个计数当前均为 0 ✓，但 include_tests 未裁 —— 【不宣布达标】，待业主口径落定后再判。"
else
  echo "  ⑦ 持平（生产码 $CUR_open_prod · 含测试 $CUR_open_all · 逃逸 $CUR_escape）✓ 未回升；终值均为 0，尚未达标"
fi
exit 0
