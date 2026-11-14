#!/usr/bin/env bash
# PortLedger 端到端冒烟：以真实扫描器（proc / compose 夹具）驱动完整业务流。
#
# 用法：bash scripts/e2e-smoke.sh [二进制路径]
# 默认读取 ./bin/port-ledger，临时目录自动创建并在结束时清理。
set -u

BIN="${1:-./bin/port-ledger}"
PORT="${ADMIN_PORT_E2E:-18081}"
BASE="http://127.0.0.1:${PORT}"
WORK="$(mktemp -d)"
DATA="$WORK/data"
PASS=0; FAIL=0

ok()   { PASS=$((PASS+1)); echo "  ✓ $1"; }
bad()  { FAIL=$((FAIL+1)); echo "  ✗ $1"; }
has()  { if echo "$2" | grep -q -- "$3"; then ok "$1"; else bad "$1（未匹配: $3）"; fi; }
lacks(){ if echo "$2" | grep -q -- "$3"; then bad "$1（不应包含: $3）"; else ok "$1"; fi; }

echo "== 准备夹具与数据目录 =="
mkdir -p "$DATA"
cp -r "$(dirname "$0")/../internal/scanner/testdata/proc" "$WORK/proc"
cp -r "$(dirname "$0")/../internal/scanner/testdata/compose" "$WORK/compose"

echo "== 启动 PortLedger =="
DATA_DIR="$DATA" ADMIN_PORT="$PORT" \
PROC_ROOT="$WORK/proc" \
SCAN_COMPOSE_DIR="$WORK/compose" \
SCAN_HOST_PROC=true SCAN_DOCKER_SOCKET=false \
SCAN_ON_START=true SCAN_INTERVAL=0 LOG_LEVEL=info \
"$BIN" > "$WORK/server.log" 2>&1 &
SRV=$!
trap 'kill $SRV 2>/dev/null; rm -rf "$WORK"' EXIT

for i in $(seq 1 40); do
  if curl -s -o /dev/null --max-time 2 "$BASE/api/health"; then break; fi
  sleep 0.5
done

echo "== T1 健康检查 =="
H=$(curl -s "$BASE/api/health")
has "status=ok" "$H" '"status":"ok"'
has "版本号呈现" "$H" '"version":"1.0.1"'
has "首启已自动扫描" "$H" '"scan_count":1'
has "数据源健康非空" "$H" 'source_health'

echo "== T2 真实扫描结果（proc + compose 夹具）=="
P=$(curl -s "$BASE/api/ports?page_size=1000")
has "列表返回分页总数（>1000）" "$P" '"total":185'
has "宿主进程端口 3300 判为 in-use(host)" "$(curl -s "$BASE/api/ports/3300")" '"state":"in-use(host)"'
has "8080 命中字典（系统）" "$(curl -s "$BASE/api/ports/8080")" '"state":"system"'
has "8080 同时被进程/compose 占用 → 冲突" "$(curl -s "$BASE/api/ports/8080")" '"conflict":true'
has "6690 系统端口自动命名" "$(curl -s "$BASE/api/ports/6690")" 'Synology Drive 文件同步与备份'
has "6690 未冲突" "$(curl -s "$BASE/api/ports/6690")" '"conflict":false'
has "5000 系统端口 + 进程占用 → 冲突" "$(curl -s "$BASE/api/ports/5000")" '"conflict":true'
has "compose 声明 15432 判为 configured" "$(curl -s "$BASE/api/ports/15432")" '"state":"configured"'
has "compose 长语法 host 模式 6060" "$(curl -s "$BASE/api/ports/6060")" '"state":"configured"'
has "compose 区间声明 7001" "$(curl -s "$BASE/api/ports/7001")" '"state":"configured"'
has "udp 端口 5353 被采集" "$P" '"port":5353,"protocol":"udp"'
has "未发布的容器端口 9000 无观测（404）" "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/ports/9000")" '404'

echo "== T3 端口命名 / 回退 =="
Q=$(curl -s -X PUT "$BASE/api/ports/3300" -H 'Content-Type: application/json' \
  -d '{"protocol":"tcp","name":"自建服务","note":"e2e 测试","tags":"测试,临时","project":"e2e"}')
has "命名生效" "$Q" '"name":"自建服务"'
has "名称来源标记为 user" "$Q" '"name_source":"user"'
has "标签与项目落库" "$Q" '"project":"e2e"'
A=$(curl -s "$BASE/api/audit?action=rename")
has "命名写入审计" "$A" '自建服务'
Q2=$(curl -s -X PUT "$BASE/api/ports/3300" -H 'Content-Type: application/json' -d '{"protocol":"tcp"}')
has "全空提交即回退（删除台账记录）" "$Q2" '"name":""'
has "回退后名称来源清空" "$Q2" '"name_source":""'
lacks "回退后不应残留备注" "$Q2" 'e2e 测试'
B=$(curl -s -X PUT "$BASE/api/ports/3300" -H 'Content-Type: application/json' -d '{"protocol":"tcp","name":"'"$(printf 'x%.0s' $(seq 1 61))"'"}')
has "名称超长被拒绝" "$B" '不得超过 60'

echo "== T4 空闲端口查询 =="
F=$(curl -s "$BASE/api/free?from=26000&to=26010&count=5&protocol=tcp")
has "空闲端口返回 5 个" "$F" '"count":5'
has "连续区间折叠为 1 段" "$F" '"from":26000,"to":26010,"count":11'
has "带上次扫描时间基准" "$F" '"based_on"'

echo "== T5 保留管理 =="
R=$(curl -s -X POST "$BASE/api/reservations" -H 'Content-Type: application/json' \
  -d '{"port":26001,"protocol":"tcp","owner":"kyson","project":"e2e","reason":"待部署","expires_at":"2030-01-01"}')
has "保留创建成功" "$R" '"id"'
RID=$(echo "$R" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
C=$(curl -s -X POST "$BASE/api/reservations" -H 'Content-Type: application/json' -d '{"port":3300,"protocol":"tcp","owner":"x"}')
has "占用端口保留被拒（409）" "$C" '已被占用'
L=$(curl -s "$BASE/api/reservations")
has "保留列表可见" "$L" '"owner":"kyson"'
has "保留未过期" "$L" '"expired":false'
P2=$(curl -s "$BASE/api/ports?q=26001")
has "保留即时反映到端口列表" "$P2" '"state":"reserved"'
U=$(curl -s -X PUT "$BASE/api/reservations" -H 'Content-Type: application/json' -d "{\"id\":$RID,\"owner\":\"kyson2\",\"expires_at\":\"2031-01-01\"}")
has "续期成功" "$U" '"ok":true'
D=$(curl -s -X DELETE "$BASE/api/reservations?id=$RID")
has "释放成功" "$D" '"ok":true'
N=$(curl -s "$BASE/api/reservations")
has "释放后列表为空" "$N" '"total":0'

echo "== T6 系统端口字典 =="
DG=$(curl -s "$BASE/api/system-dict")
has "内置条目数（>=100）" "$DG" '"builtin_count":109'
has "类别树带计数" "$DG" '"categories"'
O=$(curl -s -X POST "$BASE/api/system-dict" -H 'Content-Type: application/json' \
  -d '{"action":"override","port":6690,"protocol":"tcp","display_name":"Drive 同步（已改名）"}')
has "覆盖内置名称成功" "$O" '"ok":true'
DG2=$(curl -s "$BASE/api/system-dict?q=Drive")
has "覆盖后条目类型为 overridden" "$DG2" '"type":"overridden"'
CR=$(curl -s -X POST "$BASE/api/system-dict" -H 'Content-Type: application/json' \
  -d '{"action":"create","port":26050,"port_end":26060,"protocol":"tcp","category":"e2e","display_name":"e2e 条目"}')
has "新增自定义条目成功" "$CR" '"id"'
CID=$(echo "$CR" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
RS=$(curl -s -X POST "$BASE/api/system-dict" -H 'Content-Type: application/json' -d '{"action":"restore","port":6690,"protocol":"tcp"}')
has "恢复内置成功" "$RS" '"ok":true'
DL=$(curl -s -X POST "$BASE/api/system-dict" -H 'Content-Type: application/json' -d "{\"action\":\"delete\",\"id\":$CID}")
has "删除自定义条目成功" "$DL" '"ok":true'

echo "== T7 设置 =="
SG=$(curl -s "$BASE/api/settings")
has "设置含 values 与 env_defaults" "$SG" '"env_defaults"'
SP=$(curl -s -X PUT "$BASE/api/settings" -H 'Content-Type: application/json' -d '{"scan_interval":600}')
has "设置保存成功" "$SP" '"ok":true'
has "变更项被列出" "$SP" '"scan_interval"'
SB=$(curl -s -X PUT "$BASE/api/settings" -H 'Content-Type: application/json' -d '{"scan_ranges":"9000-8000"}')
has "非法范围被拒" "$SB" '端口范围越界或倒置'
ST=$(curl -s -X PUT "$BASE/api/settings" -H 'Content-Type: application/json' -d '{"tls_self_signed":true}')
has "需重启项被标注" "$ST" 'restart_required'

echo "== T8 备份 =="
curl -s -X PUT "$BASE/api/ports/3300" -H 'Content-Type: application/json' -d '{"protocol":"tcp","name":"保留台账项","project":"e2e"}' > /dev/null
BE=$(curl -s "$BASE/api/backup/export")
has "导出台账含 ports" "$BE" '"ports"'
has "导出内容含台账命名" "$BE" '保留台账项'
has "导出落盘到 /data/backups" "$(ls "$DATA/backups" 2>/dev/null)" 'port-ledger-'
BP=$(curl -s -X POST "$BASE/api/backup/import?preview=1" -H 'Content-Type: application/json' -d "$(printf '%s' "$BE" | sed 's/^{/{"mode":"merge",/')")
has "导入预览返回 preview" "$BP" '"preview":true'
has "导入预览统计命名条数" "$BP" '"ports":1'
BUX=$(curl -s -X POST "$BASE/api/backup/import" -H 'Content-Type: application/json' -d "$(printf '%s' "$BE" | sed 's/^{/{"mode":"upsert",/')")
has "未知 mode 被拒" "$BUX" 'mode 仅支持'
BI=$(curl -s -X POST "$BASE/api/backup/import" -H 'Content-Type: application/json' -d "$(printf '%s' "$BE" | sed 's/^{/{"mode":"merge",/')")
has "按 merge 模式导入成功" "$BI" '"imported"'
has "导入写入审计" "$(curl -s "$BASE/api/audit?action=backup_import")" 'backup_import'

echo "== T9 日志与静态资源 =="
LG=$(curl -s "$BASE/api/logs")
has "日志含扫描完成记录" "$LG" '扫描完成'
has "扫描明细日期已产生" "$LG" '"dates"'
SC=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/")
has "首页 200" "$SC" '200'
SJ=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/app.js")
has "app.js 200" "$SJ" '200'
SD=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/ports")
has "SPA 回退 200" "$SD" '200'

echo "== T10 健康自检子命令 =="
OUT=$(DATA_DIR="$DATA" ADMIN_PORT="$PORT" "$BIN" -health 2>&1)
has "-health 子命令返回 healthy" "$OUT" 'healthy'

echo
echo "===== E2E 结果：通过 $PASS 项，失败 $FAIL 项 ====="
if [ "$FAIL" -gt 0 ]; then
  echo "--- 服务端日志尾部 ---"; tail -20 "$WORK/server.log"
  exit 1
fi
exit 0
