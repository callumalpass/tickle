package runner

import (
	"context"
	"os"
	"path/filepath"
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
