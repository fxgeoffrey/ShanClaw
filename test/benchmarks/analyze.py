#!/usr/bin/env python3
"""Parse a shan session + audit log for one test run and emit a report."""
import json, sys, re
from pathlib import Path
from collections import Counter

def main(session_id, task_num, task_name):
    home = Path.home()
    sess_file = home / ".shannon/sessions" / f"{session_id}.json"
    audit = home / ".shannon/logs/audit.log"
    if not sess_file.exists():
        print(f"ERROR: session file not found: {sess_file}")
        return
    sess = json.loads(sess_file.read_text())

    # tool calls for this session, in time order
    tool_calls = []
    with audit.open() as f:
        for line in f:
            try:
                r = json.loads(line)
            except json.JSONDecodeError:
                continue
            if r.get("session_id") == session_id:
                tool_calls.append(r)

    tools = [t.get("tool_name", "?") for t in tool_calls]
    tool_counts = Counter(tools)
    # consecutive-same-tool streaks
    streaks = []
    cur, cur_n = None, 0
    for name in tools:
        if name == cur:
            cur_n += 1
        else:
            if cur_n >= 3:
                streaks.append((cur, cur_n))
            cur, cur_n = name, 1
    if cur_n >= 3:
        streaks.append((cur, cur_n))

    # error heuristic: output_summary contains "error" or starts with "fail"
    failed = 0
    for t in tool_calls:
        out = (t.get("output_summary") or "").lower()
        if ("error" in out[:200]) or out.startswith("fail") or "no such file" in out:
            failed += 1

    # read-before-edit violation: file_edit without prior file_read of same path
    reads, violations = set(), []
    for t in tool_calls:
        name = t.get("tool_name")
        inp = t.get("input_summary") or ""
        m = re.search(r'"(?:file_path|path)"\s*:\s*"([^"]+)"', inp)
        path = m.group(1) if m else None
        if name == "file_read" and path:
            reads.add(path)
        if name == "file_edit" and path and path not in reads:
            violations.append(path)

    usage = sess.get("usage", {})
    llm_calls = usage.get("llm_calls", 0)
    msgs = sess.get("messages", [])
    last_assistant = ""
    for m in reversed(msgs):
        if m.get("role") == "assistant":
            c = m.get("content")
            if isinstance(c, str):
                last_assistant = c
            elif isinstance(c, list):
                for part in c:
                    if isinstance(part, dict) and part.get("type") == "text":
                        last_assistant = part.get("text", "")
                        break
            break

    # Detect maxIter synthesis by the structured report format in the final message.
    looks_synthesized = "**Task**" in last_assistant and "**Done**" in last_assistant

    report = {
        "task": task_num,
        "name": task_name,
        "session_id": session_id,
        "llm_calls": llm_calls,
        "tool_calls": len(tool_calls),
        "tool_dist": dict(tool_counts.most_common()),
        "consecutive_streaks_3plus": streaks,
        "failures_detected": failed,
        "read_before_edit_violations": violations,
        "total_tokens": usage.get("total_tokens", 0),
        "cost_usd": usage.get("cost_usd", 0),
        "cache_read_tokens": usage.get("cache_read_tokens", 0),
        "maxiter_synthesis_detected": looks_synthesized,
        "last_assistant_preview": last_assistant[:400],
        "tool_sequence": [f"{i+1}. {t['tool_name']}: {(t.get('input_summary') or '')[:80]}" for i, t in enumerate(tool_calls)][:40],
    }
    print(json.dumps(report, ensure_ascii=False, indent=2))

if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2], sys.argv[3])
