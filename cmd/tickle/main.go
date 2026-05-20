package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/callumalpass/tickle/internal/config"
	"github.com/callumalpass/tickle/internal/paths"
	"github.com/callumalpass/tickle/internal/runner"
	"github.com/callumalpass/tickle/internal/scheduler"
	"github.com/callumalpass/tickle/internal/service"
	updatecheck "github.com/callumalpass/tickle/internal/update"
	"github.com/callumalpass/tickle/internal/version"
)

type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		var ee exitError
		if errors.As(err, &ee) {
			if ee.err != nil {
				fmt.Fprintln(os.Stderr, ee.err)
			}
			os.Exit(ee.code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "help", "-h", "--help":
		usage()
		return nil
	case "init":
		return cmdInit(args[1:])
	case "list":
		return cmdList()
	case "validate":
		return cmdValidate(args[1:])
	case "check":
		return cmdCheck(args[1:])
	case "run":
		return cmdRun(args[1:])
	case "daemon":
		return scheduler.RunForeground()
	case "status":
		return cmdStatus()
	case "logs":
		return cmdLogs(args[1:])
	case "service":
		return cmdService(args[1:])
	case "update":
		return cmdUpdate(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Print(`tickle - tiny YAML job daemon with script-gated triggers

Usage:
  tickle init
  tickle list
  tickle validate [job-file-or-id]
  tickle check <job-file-or-id>
  tickle run <job-file-or-id>
  tickle daemon
  tickle status
  tickle logs [job-file-or-id]
  tickle service <install|start|stop|restart|status|logs|uninstall>
  tickle update --check

Environment:
  TICKLE_CONFIG_HOME  override config directory
  TICKLE_DATA_HOME    override data directory

`)
}

func cmdInit(args []string) error {
	if err := paths.EnsureBaseDirs(); err != nil {
		return err
	}
	jobsDir, err := paths.JobsDir()
	if err != nil {
		return err
	}
	examplePath := filepath.Join(jobsDir, "example.yaml")
	if _, err := os.Stat(examplePath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(examplePath, []byte(exampleJobYAML), 0o644); err != nil {
			return err
		}
		fmt.Println("created", examplePath)
	}
	scriptsDir, err := paths.ScriptsDir()
	if err != nil {
		return err
	}
	exampleScript := filepath.Join(scriptsDir, "example", "run.sh")
	if _, err := os.Stat(exampleScript); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(exampleScript), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(exampleScript, []byte(exampleScriptSH), 0o755); err != nil {
			return err
		}
		fmt.Println("created", exampleScript)
	}
	configHome, _ := paths.ConfigHome()
	dataHome, _ := paths.DataHome()
	fmt.Println("config:", configHome)
	fmt.Println("data:", dataHome)
	return nil
}

func cmdList() error {
	jobs, err := loadDefaultJobs()
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		jobsDir, _ := paths.JobsDir()
		fmt.Println("no jobs found in", jobsDir)
		return nil
	}
	for _, job := range jobs {
		fmt.Printf("%-24s %-8s %-18s %s\n", job.ID, job.Status, triggerSummary(job), job.Name)
	}
	return nil
}

func cmdValidate(args []string) error {
	if len(args) == 0 {
		jobs, err := loadDefaultJobs()
		if err != nil {
			return err
		}
		for _, job := range jobs {
			fmt.Println("ok", job.Path)
		}
		if len(jobs) == 0 {
			fmt.Println("ok: no jobs configured")
		}
		return nil
	}
	for _, target := range args {
		job, err := config.ResolveJob(target)
		if err != nil {
			return err
		}
		fmt.Println("ok", job.Path)
	}
	return nil
}

func cmdCheck(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: tickle check <job-file-or-id>")
	}
	job, err := config.ResolveJob(args[0])
	if err != nil {
		return err
	}
	matched := false
	failed := false
	checked := false
	for _, trigger := range job.Triggers {
		if trigger.Type != "script" {
			continue
		}
		checked = true
		result, _ := runner.EvaluateScriptTrigger(context.Background(), job, trigger)
		fmt.Printf("%s: %s", job.ID, result.Status)
		if result.Reason != "" {
			fmt.Printf(" - %s", result.Reason)
		}
		if result.EventID != "" {
			fmt.Printf(" [%s]", result.EventID)
		}
		fmt.Println()
		if result.Status == "failed" {
			failed = true
		}
		if result.ShouldRun {
			matched = true
		}
	}
	if !checked {
		fmt.Println("no script triggers to check")
		return nil
	}
	if failed {
		return exitError{code: 2}
	}
	if !matched {
		return exitError{code: 1}
	}
	return nil
}

func cmdRun(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: tickle run <job-file-or-id>")
	}
	job, err := config.ResolveJob(args[0])
	if err != nil {
		return err
	}
	result, _ := runner.RunJob(context.Background(), job, runner.TriggerEvent{
		Type:   "manual",
		Reason: "manual CLI run",
	})
	fmt.Printf("%s: %s in %s\n", job.ID, result.Status, result.Duration.Round(time.Millisecond))
	fmt.Println("run:", result.RunDir)
	if result.Status != "success" {
		if result.Error != "" {
			fmt.Println("error:", result.Error)
		}
		return exitError{code: 1}
	}
	return nil
}

func cmdStatus() error {
	configHome, _ := paths.ConfigHome()
	dataHome, _ := paths.DataHome()
	fmt.Println("config:", configHome)
	fmt.Println("data:", dataHome)
	if out, err := service.Status(); err == nil {
		fmt.Println("service: available")
		printFirstLines(out, 8)
	} else {
		fmt.Println("service:", err)
	}
	fmt.Println()

	jobs, err := loadDefaultJobs()
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		fmt.Println("jobs: none")
		return nil
	}
	fmt.Println("jobs:")
	for _, job := range jobs {
		fmt.Printf("  %-24s %-8s %-18s", job.ID, job.Status, triggerSummary(job))
		records, _ := runner.ReadHistory(job.ID, 1)
		if len(records) > 0 {
			last := records[len(records)-1]
			fmt.Printf(" last %s/%s at %s", last.Type, last.Status, last.TS)
		}
		fmt.Println()
	}
	return nil
}

func cmdLogs(args []string) error {
	if len(args) > 1 {
		return errors.New("usage: tickle logs [job-file-or-id]")
	}
	if len(args) == 1 {
		job, err := config.ResolveJob(args[0])
		if err != nil {
			return err
		}
		return printHistory(job.ID, 30)
	}
	jobs, err := loadDefaultJobs()
	if err != nil {
		return err
	}
	for i, job := range jobs {
		if i > 0 {
			fmt.Println()
		}
		fmt.Println(job.ID + ":")
		if err := printHistory(job.ID, 10); err != nil {
			return err
		}
	}
	return nil
}

func cmdService(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: tickle service <install|start|stop|restart|status|logs|uninstall>")
	}
	var out string
	var err error
	switch args[0] {
	case "install":
		out, err = service.Install()
	case "start":
		out, err = service.Start()
	case "stop":
		out, err = service.Stop()
	case "restart":
		out, err = service.Restart()
	case "status":
		out, err = service.Status()
	case "logs":
		out, err = service.Logs()
	case "uninstall":
		out, err = service.Uninstall()
	default:
		return fmt.Errorf("unknown service command %q", args[0])
	}
	if out != "" {
		fmt.Print(out)
	}
	return err
}

func cmdUpdate(args []string) error {
	if len(args) != 1 || (args[0] != "--check" && args[0] != "check") {
		return errors.New("usage: tickle update --check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := updatecheck.Check(ctx, updatecheck.Options{
		CurrentVersion: version.Version,
	})
	if err != nil {
		return err
	}
	fmt.Println("current:", result.CurrentVersion)
	if result.LatestTag != "" {
		fmt.Println("latest:", result.LatestTag)
	} else {
		fmt.Println("latest:", result.LatestVersion)
	}
	if result.CurrentComparable {
		if result.UpdateAvailable {
			fmt.Println("status: update available")
		} else {
			fmt.Println("status: up to date")
		}
	} else {
		fmt.Println("status: latest release found; current version is not comparable")
	}
	fmt.Println("platform:", result.Platform)
	if result.BinaryAsset != nil {
		fmt.Println("binary:", result.BinaryAsset.Name)
		fmt.Println("binary_url:", result.BinaryAsset.URL)
	} else {
		fmt.Println("binary: no matching platform asset found")
	}
	if result.SkillAsset != nil {
		fmt.Println("skill:", result.SkillAsset.Name)
		fmt.Println("skill_url:", result.SkillAsset.URL)
	} else {
		fmt.Println("skill: no matching platform skill asset found")
	}
	if result.ReleaseURL != "" {
		fmt.Println("release:", result.ReleaseURL)
	}
	return nil
}

func loadDefaultJobs() ([]*config.Job, error) {
	jobsDir, err := paths.JobsDir()
	if err != nil {
		return nil, err
	}
	return config.LoadJobs(jobsDir)
}

func triggerSummary(job *config.Job) string {
	parts := make([]string, 0, len(job.Triggers))
	for _, trigger := range job.Triggers {
		switch trigger.Type {
		case "cron":
			parts = append(parts, "cron:"+trigger.Schedule)
		case "interval":
			parts = append(parts, "every:"+trigger.Every)
		case "script":
			if trigger.Schedule != "" {
				parts = append(parts, "script:"+trigger.Schedule)
			}
			if trigger.Every != "" {
				parts = append(parts, "script:"+trigger.Every)
			}
		default:
			parts = append(parts, trigger.Type)
		}
	}
	return strings.Join(parts, ",")
}

func printHistory(jobID string, limit int) error {
	records, err := runner.ReadHistory(jobID, limit)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Println("  no history")
		return nil
	}
	for _, record := range records {
		fmt.Printf("  %s %-5s %-8s", record.TS, record.Type, record.Status)
		if record.Trigger != "" {
			fmt.Printf(" trigger=%s", record.Trigger)
		}
		if record.Reason != "" {
			fmt.Printf(" reason=%q", record.Reason)
		}
		if record.EventID != "" {
			fmt.Printf(" event=%s", record.EventID)
		}
		if record.RunDir != "" {
			fmt.Printf(" run=%s", record.RunDir)
		}
		if record.Error != "" {
			fmt.Printf(" error=%q", record.Error)
		}
		fmt.Println()
	}
	return nil
}

func printFirstLines(text string, limit int) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > limit {
		lines = lines[:limit]
	}
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			fmt.Println("  " + line)
		}
	}
}

const exampleJobYAML = `version: 1
id: example
name: Example
status: disabled

trigger:
  type: interval
  every: 10m

run:
  command: ["@config/scripts/example/run.sh"]
  timeout: 1m
`

const exampleScriptSH = `#!/usr/bin/env sh
set -eu

echo "tickle example"
`
