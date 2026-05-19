#!/usr/bin/env sh
set -eu

json_false() {
  reason=$1
  jq -nc \
    --arg repo "${GITHUB_REPOSITORY:-}" \
    --arg reason "$reason" \
    '{run:false,reason:$reason,payload:{repository:$repo}}'
}

if ! command -v jq >/dev/null 2>&1; then
  printf '{"run":false,"reason":"jq is not installed","payload":{"repository":"%s"}}\n' "${GITHUB_REPOSITORY:-}"
  exit 0
fi

if ! command -v gh >/dev/null 2>&1; then
  json_false "gh is not installed"
  exit 0
fi

if [ -z "${GITHUB_REPOSITORY:-}" ]; then
  json_false "GITHUB_REPOSITORY is not set"
  exit 0
fi

limit="${GITHUB_ISSUE_LIMIT:-50}"
err_file="${TICKLE_RUNS_DIR:-.}/github-issues-ops-gh.err"
issues="$(gh issue list \
  --repo "$GITHUB_REPOSITORY" \
  --state open \
  --limit "$limit" \
  --json number,title,url,updatedAt,labels \
  2>"$err_file" || true)"

if [ -z "$issues" ]; then
  reason="$(sed -n '1p' "$err_file" 2>/dev/null || true)"
  if [ -z "$reason" ]; then
    reason="gh issue list returned no output"
  fi
  jq -nc \
    --arg repo "$GITHUB_REPOSITORY" \
    --arg reason "$reason" \
    '{run:false,reason:$reason,payload:{repository:$repo}}'
  exit 0
fi

count="$(printf '%s' "$issues" | jq 'length')"

if [ "$count" -eq 0 ]; then
  jq -nc \
    --arg repo "$GITHUB_REPOSITORY" \
    '{run:false,reason:"no open GitHub issues",payload:{repository:$repo,count:0}}'
  exit 0
fi

latest_updated="$(printf '%s' "$issues" | jq -r '[.[].updatedAt] | max // ""')"
event_id="github-issues:${GITHUB_REPOSITORY}:${count}:${latest_updated}"

last_run_succeeded=false
if [ -n "${TICKLE_LAST_RUN_AT:-}" ] && [ "${TICKLE_LAST_SUCCESS_AT:-}" = "${TICKLE_LAST_RUN_AT:-}" ]; then
  last_run_succeeded=true
fi

if [ "${TICKLE_LAST_EVENT_ID:-}" = "$event_id" ] && [ "$last_run_succeeded" = true ]; then
  jq -nc \
    --arg repo "$GITHUB_REPOSITORY" \
    --arg event_id "$event_id" \
    --argjson count "$count" \
    '{run:false,reason:($count|tostring + " open GitHub issues, unchanged since last successful triage"),event_id:$event_id,payload:{repository:$repo,count:$count}}'
  exit 0
fi

jq -nc \
  --arg repo "$GITHUB_REPOSITORY" \
  --arg event_id "$event_id" \
  --argjson count "$count" \
  --argjson issues "$issues" \
  '{run:true,reason:($count|tostring + " open GitHub issues found"),event_id:$event_id,payload:{repository:$repo,count:$count,issues:$issues}}'
