package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/callumalpass/tickle/internal/config"
)

func TestReloadIfChangedReplacesScheduleJobs(t *testing.T) {
	jobsDir := makeJobsDir(t)
	writeJob(t, jobsDir, "demo.yaml", `version: 1
id: demo
name: first
trigger:
  type: interval
  every: 1h
run:
  command: ["echo", "first"]
`)

	daemon, ctx, stop := testDaemon(t, jobsDir)
	defer stop()

	writeJob(t, jobsDir, "demo.yaml", `version: 1
id: demo
name: second
status: disabled
trigger:
  type: interval
  every: 1h
run:
  command: ["echo", "second"]
`)
	daemon.reloadIfChanged(ctx)

	jobs := daemonJobs(daemon)
	if len(jobs) != 1 {
		t.Fatalf("len(jobs) = %d, want 1", len(jobs))
	}
	if jobs[0].Name != "second" || jobs[0].Status != "disabled" {
		t.Fatalf("job was not reloaded: %#v", jobs[0])
	}
}

func TestReloadIfChangedKeepsLastGoodConfigOnInvalidYAML(t *testing.T) {
	jobsDir := makeJobsDir(t)
	writeJob(t, jobsDir, "demo.yaml", `version: 1
id: demo
name: good
trigger:
  type: interval
  every: 1h
run:
  command: ["echo", "good"]
`)

	daemon, ctx, stop := testDaemon(t, jobsDir)
	defer stop()

	writeJob(t, jobsDir, "demo.yaml", `version: 1
id: demo
surprise: true
run:
  command: ["echo", "bad"]
`)
	daemon.reloadIfChanged(ctx)

	jobs := daemonJobs(daemon)
	if len(jobs) != 1 {
		t.Fatalf("len(jobs) = %d, want 1", len(jobs))
	}
	if jobs[0].Name != "good" {
		t.Fatalf("invalid reload replaced last good job: %#v", jobs[0])
	}
	badFingerprint, err := fingerprintJobsDir(jobsDir)
	if err != nil {
		t.Fatal(err)
	}
	if daemon.sameFingerprint(badFingerprint) {
		t.Fatal("invalid reload should not accept the new fingerprint")
	}
}

func TestReloadIfChangedPreservesRunningGuard(t *testing.T) {
	jobsDir := makeJobsDir(t)
	writeJob(t, jobsDir, "demo.yaml", `version: 1
id: demo
name: first
trigger:
  type: interval
  every: 1h
run:
  command: ["echo", "first"]
`)

	daemon, ctx, stop := testDaemon(t, jobsDir)
	defer stop()
	if !daemon.tryStart("demo") {
		t.Fatal("expected running guard to start empty")
	}
	defer daemon.finish("demo")

	writeJob(t, jobsDir, "demo.yaml", `version: 1
id: demo
name: second
trigger:
  type: interval
  every: 1h
run:
  command: ["echo", "second"]
`)
	daemon.reloadIfChanged(ctx)

	if daemon.tryStart("demo") {
		daemon.finish("demo")
		t.Fatal("reload cleared the running guard")
	}
}

func testDaemon(t *testing.T, jobsDir string) (*Daemon, context.Context, func()) {
	t.Helper()
	jobs, err := config.LoadJobs(jobsDir)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := fingerprintJobsDir(jobsDir)
	if err != nil {
		t.Fatal(err)
	}
	daemon := New(jobs)
	daemon.jobsDir = jobsDir
	daemon.fingerprint = fingerprint
	ctx, cancel := context.WithCancel(context.Background())
	if err := daemon.replaceSchedule(ctx, jobs, "loaded"); err != nil {
		cancel()
		t.Fatal(err)
	}
	return daemon, ctx, func() {
		cancel()
		daemon.stopSchedule()
	}
}

func daemonJobs(daemon *Daemon) []*config.Job {
	daemon.scheduleMu.Lock()
	defer daemon.scheduleMu.Unlock()
	return append([]*config.Job(nil), daemon.jobs...)
}

func makeJobsDir(t *testing.T) string {
	t.Helper()
	jobsDir := filepath.Join(t.TempDir(), "jobs")
	if err := os.MkdirAll(jobsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return jobsDir
}

func writeJob(t *testing.T, jobsDir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(jobsDir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
