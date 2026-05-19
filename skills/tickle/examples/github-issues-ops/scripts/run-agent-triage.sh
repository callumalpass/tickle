#!/usr/bin/env sh
set -eu

prompt_file="${TRIAGE_PROMPT:-$TICKLE_SCRIPTS_DIR/github-issues-ops/prompt.md}"
memory_file="${TRIAGE_MEMORY:-$TICKLE_SCRIPTS_DIR/github-issues-ops/memory.md}"
ops_root="${OPS_ROOT:-$HOME/projects/ops}"
ops_url="${OPS_GITHUB_URL:-https://github.com/callumalpass/ops}"

mkdir -p "$(dirname "$memory_file")"
touch "$memory_file"

effective_prompt="$TICKLE_RUN_DIR/agent-prompt.md"
{
  printf '# GitHub Issues Ops Triage\n\n'
  printf 'Repository: `%s`\n' "${GITHUB_REPOSITORY:-unset}"
  printf 'Trigger payload: `%s`\n' "${TICKLE_TRIGGER_FILE:-unset}"
  printf 'Ops root: `%s`\n' "$ops_root"
  printf 'Ops project: %s\n' "$ops_url"
  printf 'Memory file: `%s`\n\n' "$memory_file"
  printf '## Saved Memory\n\n'
  sed -n '1,220p' "$memory_file"
  printf '\n\n## Automation Instructions\n\n'
  sed -n '1,260p' "$prompt_file"
} > "$effective_prompt"

export OPS_ROOT="$ops_root"
export OPS_GITHUB_URL="$ops_url"
export TICKLE_EFFECTIVE_PROMPT="$effective_prompt"
export TICKLE_EFFECTIVE_MEMORY="$memory_file"

if [ -n "${TICKLE_AGENT_COMMAND:-}" ]; then
  exec sh -c "$TICKLE_AGENT_COMMAND"
fi

if command -v codex >/dev/null 2>&1; then
  exec codex exec --prompt-file "$effective_prompt"
fi

if command -v claude >/dev/null 2>&1; then
  exec claude -p "$(sed -n '1,2000p' "$effective_prompt")"
fi

printf '%s\n' "No agent command configured." >&2
printf '%s\n' 'Set TICKLE_AGENT_COMMAND in the job env, for example:' >&2
printf '%s\n' '  TICKLE_AGENT_COMMAND: "your-agent --prompt-file \"$TICKLE_EFFECTIVE_PROMPT\""' >&2
exit 2
