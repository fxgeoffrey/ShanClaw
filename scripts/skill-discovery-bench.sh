#!/bin/bash
# Skill discovery benchmark: measures use_skill trigger rate.
# Usage: ./scripts/skill-discovery-bench.sh [before|after]
set -euo pipefail

LABEL=${1:-"before"}
LOG=~/.shannon/logs/audit.log
MARKER=$(wc -l < "$LOG")

PROMPTS=(
  "帮我创建一个 agent"
  "分析这个 PDF"
  "写一个 MCP server"
  "做一个 landing page"
  "帮我写项目状态更新"
  "我想让 AI 能查数据库"
  "这个 Go 函数有 bug，帮我看看"
  "帮我写个单元测试"
  "给这个报告换套主题"
  "把这套流程封装成可复用的模板"
)

for i in "${!PROMPTS[@]}"; do
  echo "[$((i+1))/10] ${PROMPTS[$i]}"
  shan -y "${PROMPTS[$i]}" </dev/null > /dev/null 2>"$LOG.bench_stderr" &
  BGPID=$!
  for tick in $(seq 1 60); do
    kill -0 $BGPID 2>/dev/null || break
    sleep 1
  done
  kill $BGPID 2>/dev/null; wait $BGPID 2>/dev/null
  cat "$LOG.bench_stderr" | grep "^\[skill-discovery\]" || true
  sleep 2
done

NEW_ENTRIES=$(tail -n +$((MARKER+1)) "$LOG")
SKILL_CALLS=$(echo "$NEW_ENTRIES" | grep -c '"use_skill"' || true)
TOTAL_CALLS=$(echo "$NEW_ENTRIES" | wc -l | tr -d ' ')

echo ""
echo "=== Results ($LABEL) ==="
echo "use_skill calls: $SKILL_CALLS / 10 prompts"
echo "Total tool calls: $TOTAL_CALLS"
echo "$NEW_ENTRIES" | grep '"use_skill"' | python3 -c "
import sys, json
for line in sys.stdin:
    d = json.loads(line.strip())
    s = d.get('input_summary','')
    try:
        inp = json.loads(s)
        print(f'  -> {inp.get(\"skill_name\",\"?\")}')" 2>/dev/null || true
