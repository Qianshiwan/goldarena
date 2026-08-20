#!/usr/bin/env bash
# 本地 PostgreSQL 一键初始化（Windows + Git Bash，EDB 便携二进制）
# 用法: bash scripts/setup_postgres.sh
set -u

PGHOME="/d/tools/Jinguizigoldtrader/pg16"
ZIP="/d/tools/Jinguizigoldtrader/pg16_bin.zip"
PGBIN="$PGHOME/bin"
PGDATA="/d/tools/Jinguizigoldtrader/pgdata"
SCHEMA="/d/tools/Jinguizigoldtrader/goldarena/data/init/001_schema.sql"
PGUSER="goldarena"
PGDB="goldarena"
PGPORT=5432

echo "==> [1/5] 解压便携二进制"
if [ ! -x "$PGBIN/initdb.exe" ]; then
  if [ ! -f "$ZIP" ]; then
    echo "ERROR: 未找到 $ZIP，请先下载 PostgreSQL 便携二进制"; exit 1
  fi
  echo "    解压 $ZIP -> $PGHOME"
  unzip -o -q "$ZIP" -d "$PGHOME" || { echo "ERROR: 解压失败"; exit 1; }
  # EDB 包可能嵌套一层 pgsql/ 目录，统一归到 PGHOME/bin
  if [ ! -x "$PGBIN/initdb.exe" ] && [ -x "$PGHOME/pgsql/bin/initdb.exe" ]; then
    cp -r "$PGHOME/pgsql/"* "$PGHOME/" 2>/dev/null || true
  fi
fi
[ -x "$PGBIN/initdb.exe" ] || { echo "ERROR: initdb.exe 仍缺失"; exit 1; }
echo "    initdb 就绪: $PGBIN/initdb.exe"

echo "==> [2/5] initdb (仅首次)"
if [ ! -f "$PGDATA/PG_VERSION" ]; then
  rm -rf "$PGDATA"
  "$PGBIN/initdb.exe" -D "$PGDATA" -U "$PGUSER" -A trust -E UTF8 --locale=C \
    || { echo "ERROR: initdb 失败"; exit 1; }
  echo "    initdb 完成 -> $PGDATA"
else
  echo "    已存在，跳过"
fi

echo "==> [3/5] 启动 postgres"
if "$PGBIN/pg_ctl.exe" status -D "$PGDATA" >/dev/null 2>&1; then
  echo "    已在运行"
else
  "$PGBIN/pg_ctl.exe" -D "$PGDATA" -l "$PGDATA/server.log" \
    -o "-p $PGPORT -c listen_addresses=localhost" start \
    || { echo "ERROR: 启动失败，详见 $PGDATA/server.log"; exit 1; }
  # 等待就绪
  for i in $(seq 1 20); do
    if "$PGBIN/pg_isready.exe" -h localhost -p "$PGPORT" >/dev/null 2>&1; then break; fi
    sleep 1
  done
fi
"$PGBIN/pg_isready.exe" -h localhost -p "$PGPORT" && echo "    postgres 就绪"

echo "==> [4/5] 建库 $PGDB (若不存在)"
"$PGBIN/psql.exe" -U "$PGUSER" -h localhost -p "$PGPORT" -d postgres -tc \
  "SELECT 1 FROM pg_database WHERE datname='$PGDB'" | grep -q 1 \
  || "$PGBIN/createdb.exe" -U "$PGUSER" -p "$PGPORT" "$PGDB"
echo "    库检查完成"

echo "==> [5/5] 执行 schema"
"$PGBIN/psql.exe" -U "$PGUSER" -h localhost -p "$PGPORT" -d "$PGDB" -f "$SCHEMA" \
  && echo "    schema 执行成功 (users/wallets/orders/positions/contests/klines)"

echo ""
echo "PostgreSQL 已就绪: host=localhost port=$PGPORT user=$PGUSER dbname=$PGDB (trust 认证, 无密码)"
echo "启动 gateway 时会自动连上 (config.yaml database.postgres 已配好)。"
