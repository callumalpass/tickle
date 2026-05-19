package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadJobNormalizesSingularTrigger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.yaml")
	writeFile(t, path, `version: 1
id: demo
run:
  command: ["echo", "hello"]
trigger:
  type: interval
  every: 5m
`)

	job, err := LoadJob(path)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "demo" {
		t.Fatalf("ID = %q", job.ID)
	}
	if job.Status != "active" {
		t.Fatalf("Status = %q", job.Status)
	}
	if len(job.Triggers) != 1 {
		t.Fatalf("len(Triggers) = %d", len(job.Triggers))
	}
	if job.Triggers[0].Type != "interval" {
		t.Fatalf("trigger type = %q", job.Triggers[0].Type)
	}
}

func TestLoadJobRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.yaml")
	writeFile(t, path, `version: 1
id: demo
surprise: true
run:
  command: ["echo", "hello"]
`)

	if _, err := LoadJob(path); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestLoadJobDefaultsToManualTrigger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.yaml")
	writeFile(t, path, `version: 1
id: demo
run:
  command: ["echo", "hello"]
`)

	job, err := LoadJob(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Triggers) != 1 || job.Triggers[0].Type != "manual" {
		t.Fatalf("expected manual trigger, got %#v", job.Triggers)
	}
}

func TestTokenCWDIsNotResolvedRelativeToJobFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.yaml")
	writeFile(t, path, `version: 1
id: demo
run:
  cwd: "@config/work"
  command: ["echo", "hello"]
trigger:
  type: script
  every: 1m
  cwd: "@data/checks"
  command: ["echo", "check"]
`)

	job, err := LoadJob(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := job.RunCWD(); got != "@config/work" {
		t.Fatalf("RunCWD = %q", got)
	}
	if got := job.TriggerCWD(job.Triggers[0]); got != "@data/checks" {
		t.Fatalf("TriggerCWD = %q", got)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
