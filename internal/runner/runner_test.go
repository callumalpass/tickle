package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/callumalpass/tickle/internal/config"
)

func TestScriptTriggerAndRunJobWriteHistory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TICKLE_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("TICKLE_DATA_HOME", filepath.Join(root, "data"))

	jobPath := filepath.Join(root, "job.yaml")
	if err := os.WriteFile(jobPath, []byte(`version: 1
id: demo
trigger:
  type: script
  every: 1m
  command: ["sh", "-c", "printf '{\"run\":true,\"reason\":\"ready\",\"event_id\":\"evt-1\",\"payload\":{\"n\":1}}'"]
run:
  command: ["sh", "-c", "echo hello; echo problem >&2"]
  timeout: 1m
`), 0o644); err != nil {
		t.Fatal(err)
	}

	job, err := config.LoadJob(jobPath)
	if err != nil {
		t.Fatal(err)
	}

	check, err := EvaluateScriptTrigger(context.Background(), job, job.Triggers[0])
	if err != nil {
		t.Fatal(err)
	}
	if !check.ShouldRun || check.Status != "matched" || check.EventID != "evt-1" {
		t.Fatalf("unexpected check result: %#v", check)
	}

	run, err := RunJob(context.Background(), job, TriggerEvent{
		Type:    "script",
		Reason:  check.Reason,
		EventID: check.EventID,
		Payload: check.Payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "success" {
		t.Fatalf("run status = %s: %s", run.Status, run.Error)
	}
	if _, err := os.Stat(filepath.Join(run.RunDir, "trigger.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(run.Stdout); err != nil {
		t.Fatal(err)
	}

	history, err := ReadHistory(job.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2", len(history))
	}
	if history[0].Type != "check" || history[1].Type != "run" {
		t.Fatalf("unexpected history: %#v", history)
	}
}

func TestConfigTokenCommands(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, "config")
	dataHome := filepath.Join(root, "data")
	t.Setenv("TICKLE_CONFIG_HOME", configHome)
	t.Setenv("TICKLE_DATA_HOME", dataHome)

	scriptDir := filepath.Join(configHome, "scripts", "demo")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	checkScript := filepath.Join(scriptDir, "has-work.sh")
	if err := os.WriteFile(checkScript, []byte("#!/usr/bin/env sh\nprintf '{\"run\":true,\"reason\":\"token ok\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runScript := filepath.Join(scriptDir, "run.sh")
	if err := os.WriteFile(runScript, []byte("#!/usr/bin/env sh\necho \"$TICKLE_SCRIPTS_DIR\"\necho \"$AGENT_PROMPT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	jobPath := filepath.Join(configHome, "jobs", "demo.yaml")
	if err := os.MkdirAll(filepath.Dir(jobPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobPath, []byte(`version: 1
id: demo-token
trigger:
  type: script
  every: 1m
  command: ["@config/scripts/demo/has-work.sh"]
run:
  command: ["@config/scripts/demo/run.sh"]
  timeout: 1m
env:
  AGENT_PROMPT: "@config/scripts/demo/prompt.md"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	job, err := config.LoadJob(jobPath)
	if err != nil {
		t.Fatal(err)
	}
	check, err := EvaluateScriptTrigger(context.Background(), job, job.Triggers[0])
	if err != nil {
		t.Fatal(err)
	}
	if !check.ShouldRun || check.Reason != "token ok" {
		t.Fatalf("unexpected check result: %#v", check)
	}

	run, err := RunJob(context.Background(), job, TriggerEvent{Type: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "success" {
		t.Fatalf("run failed: %#v", run)
	}
	stdout, err := os.ReadFile(run.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stdout), filepath.Join(configHome, "scripts", "demo")) {
		t.Fatalf("stdout did not contain expanded config script path: %s", stdout)
	}
}
