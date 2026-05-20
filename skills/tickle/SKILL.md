---
name: tickle
description: Use when creating, validating, installing, running, or maintaining Tickle YAML jobs and the local Tickle daemon. Tickle runs command jobs from YAML, including cron, interval, manual, and script-gated triggers, with JSONL run history and agent-friendly prompt/memory conventions.
---

# Tickle

Use Tickle for local command automations that should be easy for agents and humans to inspect. Tickle jobs are YAML files, and run history is append-only JSONL plus per-run artifacts.

## Locate the CLI

Prefer the bundled skill wrapper from the installed skill folder:

```bash
<skills-dir>/tickle/scripts/tickle
```

If that path is not present, use `tickle` from `PATH`, or build it from the repo:

```bash
go build -o tickle ./cmd/tickle
```

## Basic Workflow

1. Run `tickle init` if the config/data directories do not exist.
2. Create or edit a job YAML file under the user's Tickle jobs directory.
3. Put user-owned job scripts under `@config/scripts/<job-id>/`.
4. Run `tickle validate <job-file-or-id>`.
5. For script-gated jobs, run `tickle check <job-file-or-id>`.
6. Run `tickle run <job-file-or-id>` for a manual smoke test when safe.
7. Use `tickle service install` and `tickle service start` when the user wants a background daemon.
8. Inspect `tickle status` and `tickle logs <job-id>` after setup or failures.

Default job directories:

- Linux: `~/.config/tickle/jobs/*.yaml`
- macOS: `~/Library/Application Support/tickle/jobs/*.yaml`
- Windows: `%APPDATA%\Tickle\jobs\*.yaml`

Default data directories:

- Linux: `~/.local/share/tickle`
- macOS: `~/Library/Application Support/tickle`
- Windows: `%LOCALAPPDATA%\Tickle`

`TICKLE_CONFIG_HOME` and `TICKLE_DATA_HOME` override these locations.

Default user-level layout:

```text
<config>/
  jobs/
  scripts/
    <job-id>/
  templates/

<data>/
  state/
  runs/
  logs/
  bin/
```

Use the config directory for job definitions, scripts, prompts, memory files,
and other hand-authored inputs. Use the data directory only for generated state,
history, logs, binaries, and run artifacts.

## Job Patterns

Use array commands by default. Avoid shell strings unless shell features are required.

```yaml
run:
  command: ["@config/scripts/my-job/run.sh"]
```

Tickle expands `@config/` and `@data/` in command arguments, working
directories, and environment values. Prefer `@config/scripts/<job-id>/...` for
user-owned automations. Use repo-relative paths only when the automation is
owned by that repository and the scripts should be committed there.

Use `script` triggers when a condition should be checked on a schedule before running the job:

```yaml
triggers:
  - type: script
    schedule: "*/15 * * * *"
    command: ["@config/scripts/my-job/should-run.sh"]
    timeout: 30s
```

Script trigger contract:

- exit `0`: run the job.
- exit `1`: skip the job.
- any other exit code: check failed.
- optional JSON stdout can include `run`, `reason`, `event_id`, and `payload`.

Example JSON stdout:

```json
{"run":true,"reason":"new work found","event_id":"github:repo:issue-123","payload":{"issue":123}}
```

The job command receives `TICKLE_TRIGGER_FILE`, pointing to the saved trigger payload for the run.

## Templates

Copy from `templates/` when creating new jobs:

- `command.yaml`: ordinary scheduled command job.
- `script-gated.yaml`: check script controls whether the job runs.
- `agent-job.yaml`: agent job using prompt and memory files.

After copying a template, update `id`, `name`, paths, commands, schedule, and timeout before validation.

## Examples

Use `examples/github-issues-ops/` for a complete agent automation that checks a
GitHub repository for open issues and asks a coding agent to triage them into an
ops registry at `~/projects/ops`. The ops registry project is
https://github.com/callumalpass/ops.

To install that example:

1. Copy `examples/github-issues-ops/job.yaml` to the user's Tickle jobs directory as `github-issues-ops.yaml`.
2. Copy `examples/github-issues-ops/scripts/*`, `prompt.md`, and `memory.md` to `@config/scripts/github-issues-ops/`.
3. Edit `GITHUB_REPOSITORY` and, if needed, `TICKLE_AGENT_COMMAND`.
4. Change `status` from `disabled` to `active`.
5. Run `tickle validate github-issues-ops` and `tickle check github-issues-ops`.

## Service Commands

Use:

```bash
tickle service install
tickle service start
tickle service status
tickle service logs
```

`service install` copies the current binary to a stable per-user runtime path before registering the native user-level background runner.

The running daemon hot-reloads valid changes to job YAML files in the jobs
directory. If a job file is invalid, the daemon logs the reload error and keeps
the last good schedule.
