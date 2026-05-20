# Tickle

Tickle is a tiny local job daemon for running commands from YAML, with
script-gated triggers and JSONL run history.

It runs scheduled jobs, interval jobs, manual jobs, and jobs guarded by small
check scripts. It can run ordinary shell commands, maintenance tasks, or agent
workflows that use prompt files, memory files, structured trigger payloads, and
a run journal that is easy to inspect with normal shell tools.

## Install

Tickle is intended to be skill/agent-first. The easiest path is usually to ask
your coding agent to install it from this repository, rather than installing Go
and building it yourself.

Example agent prompt:

```text
Install Tickle from https://github.com/callumalpass/tickle.
Use the repo's skills/tickle skill, install the matching Tickle binary, run
tickle init, and install/start the user service.
```

The repo includes a coding-agent skill at `skills/tickle`. The skill teaches
agents how to install Tickle, create jobs, validate them, run checks, inspect
history, and manage the daemon.

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

## Manual Source Build

You only need Go if you want to build Tickle from source. Install Go 1.24 or
newer from <https://go.dev/doc/install>, then run:

```bash
go build -o tickle ./cmd/tickle
./tickle init
./tickle validate example
./tickle run example
./tickle daemon
```

For a persistent user-level daemon, use:

```bash
./tickle service install
./tickle service start
./tickle service status
```

Check whether a newer Tickle release is available:

```bash
./tickle update --check
```

This only checks for updates; it does not replace the installed binary.

By default, Tickle reads jobs from:

- Linux: `~/.config/tickle/jobs/*.yaml`
- macOS: `~/Library/Application Support/tickle/jobs/*.yaml`
- Windows: `%APPDATA%\Tickle\jobs\*.yaml`

Run data is written as JSON and JSONL under the platform data directory:

- Linux: `~/.local/share/tickle`
- macOS: `~/Library/Application Support/tickle`
- Windows: `%LOCALAPPDATA%\Tickle`

You can override these paths with `TICKLE_CONFIG_HOME` and `TICKLE_DATA_HOME`.

`tickle init` creates this user-level layout:

```text
<config>/
  jobs/
  scripts/
  templates/

<data>/
  state/
  runs/
  logs/
  bin/
```

For user-owned automations, put scripts under
`@config/scripts/<job-id>/` and reference them from job files with the
`@config/` token. Tickle expands `@config/` and `@data/` in command
arguments, working directories, and environment values before starting a
process.

Repo-local scripts still work when the automation belongs to a project and the
scripts should be committed with that project.

## Job Format

```yaml
version: 1
id: repo-maintenance
name: Repo Maintenance
status: active

triggers:
  - type: script
    schedule: "*/15 * * * *"
    command: ["@config/scripts/repo-maintenance/has-work.sh"]
    timeout: 30s

run:
  cwd: /path/to/project
  command: ["@config/scripts/repo-maintenance/run-agent-job.sh"]
  timeout: 2h

env:
  AGENT_MEMORY: "@config/scripts/repo-maintenance/memory.md"
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
{"run":true,"reason":"3 new issues","event_id":"github:repo:1898","payload":{"issues":[1898]}}
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
tickle update --check
```

`tickle daemon` runs in the foreground. `tickle service install` copies the
current binary to a stable per-user location and registers it with the native
user-level service mechanism:

- Linux: `systemd --user`
- macOS: `launchd` LaunchAgent
- Windows: Task Scheduler logon task

While the daemon is running, it polls the jobs directory and hot-reloads valid
changes to `*.yaml` and `*.yml` job files. Invalid edits are logged and the last
good schedule keeps running.

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
