#!/usr/bin/env bash
# N-A3 真 PG 证据跑批(dev56 R2-7/addendum 部署生效门)。
# 用法: sudo -u postgres 可达的机器上: bash scripts/na3-pg-evidence.sh <输出目录>
# 产物: <输出目录>/na3-evidence.log(全程命令+输出)+ acl-dp.txt(\dp 封存)+ go-test-pg.log
# ⚠只对独立测试库 dingwei_r3 操作,任何生产 DSN 禁入(08-04 T-ITEST 铁律)。
set -euo pipefail
OUT="${1:?输出目录必填}"
mkdir -p "$OUT"
LOG="$OUT/na3-evidence.log"
DB=dingwei_r3
PW_APP=r3app PW_AS=r3assign PW_GC=r3gc PW_SUP=r3sup
exec > >(tee "$LOG") 2>&1

echo "== N-A3 evidence run @$(date -u +%FT%TZ) host=$(hostname) =="
echo "== git anchor: $(git rev-parse HEAD) =="
sudo -u postgres psql -Atc 'select version();'

echo '== 0) 重置测试库与角色(幂等) =='
sudo -u postgres psql -v ON_ERROR_STOP=1 <<SQL
DROP DATABASE IF EXISTS ${DB};
DO \$\$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='datad_user') THEN CREATE ROLE datad_user LOGIN; END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='datad_assign') THEN CREATE ROLE datad_assign LOGIN; END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='datad_assign_gc') THEN CREATE ROLE datad_assign_gc LOGIN; END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='r3super') THEN CREATE ROLE r3super LOGIN SUPERUSER; END IF;
END \$\$;
ALTER ROLE datad_user PASSWORD '${PW_APP}';
ALTER ROLE datad_assign PASSWORD '${PW_AS}';
ALTER ROLE datad_assign_gc PASSWORD '${PW_GC}';
ALTER ROLE r3super PASSWORD '${PW_SUP}';
CREATE DATABASE ${DB} OWNER datad_user;
SQL

echo '== 1) SchemaDDL 迁移(datad_user 生产同路径:OpenPG 幂等迁移,TestPGMigrateOnly 触发)=='
DATAD_TEST_PG_DSN="postgres://datad_user:${PW_APP}@127.0.0.1:5432/${DB}" \
  go test -count=1 -run 'TestPGMigrateOnly$' ./internal/datad/ | tail -1

echo '== 2) 授权(deploy/datad-grants.sql,超级用户执行)——先迁后授,与生产序一致 =='
sudo -u postgres psql -d ${DB} -v ON_ERROR_STOP=1 -f deploy/datad-grants.sql

echo '== 3) ACL 封存(\dp 四表)=='
sudo -u postgres psql -d ${DB} -c '\dp assignment' -c '\dp assign_first_result' -c '\dp task' -c '\dp audit' | tee "$OUT/acl-dp.txt"

echo '== 4) PG 集成全套(-race,四 DSN 门控全开;退出码显式捕获不经管道=防掩码)=='
set +e
DATAD_TEST_PG_DSN="postgres://datad_user:${PW_APP}@127.0.0.1:5432/${DB}" \
DATAD_TEST_PG_ASSIGN_DSN="postgres://datad_assign:${PW_AS}@127.0.0.1:5432/${DB}" \
DATAD_TEST_PG_GC_DSN="postgres://datad_assign_gc:${PW_GC}@127.0.0.1:5432/${DB}" \
DATAD_TEST_PG_SETUP_DSN="postgres://r3super:${PW_SUP}@127.0.0.1:5432/${DB}" \
  go test -race -count=1 -v -run 'TestPG' ./internal/datad/ >"$OUT/go-test-pg.log" 2>&1
TEST_EXIT=$?
set -e
grep -E '^(--- (PASS|FAIL|SKIP)|PASS|FAIL|ok)' "$OUT/go-test-pg.log" || true
echo "go test exit=${TEST_EXIT}"
[ "$TEST_EXIT" -eq 0 ]

echo '== 5) 负向裸证(psql 直连,测试之外的第二证面)=='
set +e
PGPASSWORD=$PW_APP psql -h 127.0.0.1 -U datad_user -d ${DB} -c "INSERT INTO assignment(task_id,role,assignee,generation,active,request_id) VALUES('x','executor','x',1,false,'x');" 2>&1 | head -2
PGPASSWORD=$PW_APP psql -h 127.0.0.1 -U datad_user -d ${DB} -c "UPDATE assign_first_result SET outcome='t';" 2>&1 | head -2
PGPASSWORD=$PW_GC psql -h 127.0.0.1 -U datad_assign_gc -d ${DB} -c "UPDATE assignment SET active=false;" 2>&1 | head -2
PGPASSWORD=$PW_AS psql -h 127.0.0.1 -U datad_assign -d ${DB} -c "DELETE FROM assign_first_result;" 2>&1 | head -2
PGPASSWORD=$PW_AS psql -h 127.0.0.1 -U datad_assign -d ${DB} -c "SELECT current_user, session_user;" 2>&1 | head -4
set -e
echo '== done =='
