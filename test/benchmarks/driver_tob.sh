#!/bin/bash
# 6 toB daily-task scenarios — READ-ONLY / draft-free.
set -u
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$REPO_ROOT"
RESULTS="${BENCHMARK_RESULTS_DIR:-/tmp/maxiter_tests/results_tob}"
mkdir -p "$RESULTS"

get_latest_session() {
  ls -t ~/.shannon/sessions/*.json 2>/dev/null | head -1 | xargs -I{} basename {} .json
}

run_task() {
  local num=$1; local desc=$2; local prompt=$3
  echo "=== Task $num: $desc ===" | tee -a "$RESULTS/driver.log"
  date +"%Y-%m-%d %H:%M:%S" | tee -a "$RESULTS/driver.log"

  local stdout_file="$RESULTS/task${num}.stdout"
  perl -e 'alarm shift; exec @ARGV' 480 shan -y "$prompt" >"$stdout_file" 2>&1
  local rc=$?

  local sid=$(get_latest_session)
  echo "session_id=$sid (exit=$rc)" | tee -a "$RESULTS/driver.log"
  echo "$sid" > "$RESULTS/task${num}.session_id"
  tail -5 "$stdout_file" | sed 's/^/  /' | tee -a "$RESULTS/driver.log"
  echo "" | tee -a "$RESULTS/driver.log"
  sleep 2
}

run_task 1 "meeting prep" \
  "我明天（2026-04-21）日历上第一个会议是什么？看会议描述、参会人、相关邀请邮件，告诉我应该准备什么材料。只分析，不要创建或回复任何东西"

run_task 2 "inbox triage" \
  "过去 3 天（2026-04-17 到 2026-04-20）Gmail 收件箱里，哪些邮件需要我本周内回复？按优先级排序，每封 3 句：发件人、主题、我该做什么。不要起草回复，不要发送任何邮件"

run_task 3 "meetings audit" \
  "看我未来 7 天（2026-04-20 到 2026-04-27）的日历，哪些会议从标题和参会人数判断，可能是'可删除'或'改为异步'的？给一个表，每条标明判断依据（例如：纯 standup、1:1 且无固定议程、参会人超过 15 人等）。不要修改日历"

run_task 4 "drive doc dedupe" \
  "我 Drive 上有没有文件名含'年度计划' '年度规划' 'annual plan' 'yearly plan' 之类的文档？有多个版本就告诉我最新那个是哪个、最后修改时间、最后编辑人。只列出，不要打开或修改"

run_task 5 "calendar ↔ notion matching" \
  "看我未来 7 天的日历，每个会议在 Notion 里有没有对应的项目页面或客户页面（标题近似或关键词匹配）？匹配到的给 Notion 链接，没匹配的也明确指出。不要在 Notion 创建任何新页面"

run_task 6 "notion database health" \
  "我的 Notion workspace 里有哪些数据库？每个数据库用一句话说用途、大致多少行、最近更新时间。按'活跃度'（最近更新+条目数）排序。只读取"

echo "=== ALL DONE ==="
date +"%Y-%m-%d %H:%M:%S" | tee -a "$RESULTS/driver.log"
