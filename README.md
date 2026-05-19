# Tickle

Tickle is a tiny local job daemon for running commands from YAML, with
script-gated triggers and JSONL run history.

It runs scheduled jobs, interval jobs, manual jobs, and jobs guarded by small
check scripts. It can run ordinary shell commands, maintenance tasks, or agent
workflows that use prompt files, memory files, structured trigger payloads, and
a run journal that is easy to inspect with normal shell tools.

## Quick Start

```bash
go build -o tickle ./cmd/tickle
./tickle init
./tickle validate examples/script-gated.yaml
./tickle run examples/script-gated.yaml
./tickle daemon
```

By default, Tickle reads jobs from:

- Linux: `~/.config/tickle/jobs/*.yaml`
- macOS: `~/Library/Application Support/tickle/jobs/*.yaml`
- Windows: `%APPDATA%\Tickle\jobs\*.yaml`

Run data is written as JSON and JSONL under the platform data directory:

- Linux: `~/.local/share/tickle`
- macOS: `~/Library/Application Support/tickle`
- Windows: `%LOCALAPPDATA%\Tickle`

You can override these paths with `TICKLE_CONFIG_HOME` and `TICKLE_DATA_HOME`.

## Skill

The repo includes a Codex skill at `skills/tickle`. The skill teaches agents how
to create, validate, install, run, and inspect Tickle jobs.

Release builds publish platform-specific skill zips:

```text
tickle-skill-linux-amd64.zip
tickle-skill-linux-arm64.zip
tickle-skill-darwin-amd64.zip
tickle-skill-darwin-arm64.zip
tickle-skill-windows-amd64.zip
```

Each zip contains a `tickle/` skill folder with `SKILL.md`, templates, wrapper
scripts, and the matching platform binary under `scripts/bin/`.

## Job Format

```yaml
version: 1
id: tasknotes-ops
name: TaskNotes Ops
status: active

triggers:
  - type: script
    schedule: "*/15 * * * *"
    command: ["./checks/has-new-issues.sh"]
    timeout: 30s

run:
  cwd: /home/calluma/projects/tasknotes
  command: ["codex", "exec", "--prompt-file", "./prompt.md"]
  timeout: 2h

env:
  AGENT_MEMORY: ./memory.md
```

The native trigger types are:

- `cron`: run on a cron schedule.
- `interval`: run every duration, for example `10m`.
- `script`: run a check command on a cron schedule or interval; run the job only
  when the check says yes.
- `manual`: never scheduled by the daemon.

Script trigger contract:

- exit `0`: run the job.
- exit `1`: skip the job.
- any other exit code: the check failed.
- optional JSON stdout can include `run`, `reason`, `event_id`, and `payload`.

Example stdout:

```json
{"run":true,"reason":"3 new issues","event_id":"github:tasknotes:1898","payload":{"issues":[1898]}}
```

## CLI

```bash
tickle init
tickle list
tickle validate <job-file-or-id>
tickle check <job-file-or-id>
tickle run <job-file-or-id>
tickle daemon
tickle status
tickle logs [job-file-or-id]
tickle service install
tickle service start
tickle service status
tickle service logs
```

`tickle daemon` runs in the foreground. `tickle service install` copies the
current binary to a stable per-user location and registers it with the native
user-level service mechanism:

- Linux: `systemd --user`
- macOS: `launchd` LaunchAgent
- Windows: Task Scheduler logon task

## History

Each job has append-only JSONL history:

```text
<data>/runs/<job-id>/history.jsonl
```

Each actual command run also gets an artifact directory:

```text
<data>/runs/<job-id>/<timestamp>/
  trigger.json
  stdout.log
  stderr.log
  result.json
```

Mutable state lives in:

```text
<data>/state/<job-id>.json
```

## Release Builds

Build all standalone binaries and skill zips locally:

```bash
scripts/build-release.sh
```

Artifacts are written to `dist/`, including `SHA256SUMS`.

GitHub Actions runs tests on pushes and pull requests. Pushing a tag like
`v0.1.0` builds the release artifacts and attaches them to a GitHub Release.
