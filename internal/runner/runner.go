package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/callumalpass/tickle/internal/config"
	"github.com/callumalpass/tickle/internal/paths"
)

type State struct {
	LastCheckAt   string `json:"last_check_at,omitempty"`
	LastRunAt     string `json:"last_run_at,omitempty"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
	LastFailureAt string `json:"last_failure_at,omitempty"`
	LastEventID   string `json:"last_event_id,omitempty"`
}

type TriggerEvent struct {
	Type    string          `json:"type"`
	Source  string          `json:"source,omitempty"`
	Reason  string          `json:"reason,omitempty"`
	EventID string          `json:"event_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type CheckResult struct {
	JobID     string          `json:"job_id"`
	Trigger   string          `json:"trigger"`
	ShouldRun bool            `json:"should_run"`
	Status    string          `json:"status"`
	Reason    string          `json:"reason,omitempty"`
	EventID   string          `json:"event_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	ExitCode  int             `json:"exit_code"`
	Duration  time.Duration   `json:"-"`
	Error     string          `json:"error,omitempty"`
}

type RunResult struct {
	JobID     string        `json:"job_id"`
	Status    string        `json:"status"`
	RunDir    string        `json:"run_dir"`
	Stdout    string        `json:"stdout"`
	Stderr    string        `json:"stderr"`
	ExitCode  int           `json:"exit_code"`
	Duration  time.Duration `json:"-"`
	Error     string        `json:"error,omitempty"`
	StartedAt string        `json:"started_at"`
	EndedAt   string        `json:"ended_at"`
}

type HistoryRecord struct {
	TS         string `json:"ts"`
	JobID      string `json:"job_id"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Trigger    string `json:"trigger,omitempty"`
	Reason     string `json:"reason,omitempty"`
	EventID    string `json:"event_id,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	RunDir     string `json:"run_dir,omitempty"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Error      string `json:"error,omitempty"`
}

type scriptJSON struct {
	Run     *bool           `json:"run"`
	Reason  string          `json:"reason"`
	EventID string          `json:"event_id"`
	Payload json.RawMessage `json:"payload"`
}

func EvaluateScriptTrigger(ctx context.Context, job *config.Job, trigger config.Trigger) (CheckResult, error) {
	start := time.Now()
	state, _ := LoadState(job.ID)
	env, err := commandEnv(job, state, map[string]string{
		"TICKLE_TRIGGER_TYPE": "script",
	})
	if err != nil {
		return CheckResult{}, err
	}
	env = mergeEnv(env, trigger.Env)

	timeout := 30 * time.Second
	if trigger.Timeout != "" {
		timeout, err = time.ParseDuration(trigger.Timeout)
		if err != nil {
			return CheckResult{}, err
		}
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode, execErr := execute(ctx, trigger.Command, job.TriggerCWD(trigger), envList(env), &stdout, &stderr)
	result := CheckResult{
		JobID:     job.ID,
		Trigger:   "script",
		ExitCode:  exitCode,
		Duration:  time.Since(start),
		ShouldRun: exitCode == 0,
	}

	if ctx.Err() != nil {
		result.Status = "failed"
		result.Error = ctx.Err().Error()
		result.Reason = "check timed out"
	} else if execErr != nil && exitCode != 1 {
		result.Status = "failed"
		result.Error = execErr.Error()
		result.Reason = "check failed"
	} else if exitCode == 1 {
		result.Status = "skipped"
		result.Reason = "script exited 1"
	} else if exitCode == 0 {
		result.Status = "matched"
		result.Reason = "script exited 0"
	} else {
		result.Status = "failed"
		result.Reason = fmt.Sprintf("script exited %d", exitCode)
		if execErr != nil {
			result.Error = execErr.Error()
		}
	}

	if parsed, ok := parseScriptJSON(stdout.Bytes()); ok {
		if parsed.Run != nil {
			result.ShouldRun = *parsed.Run
			if !*parsed.Run && result.Status == "matched" {
				result.Status = "skipped"
			}
		}
		if parsed.Reason != "" {
			result.Reason = parsed.Reason
		}
		result.EventID = parsed.EventID
		result.Payload = parsed.Payload
	} else if line := firstLine(stdout.String()); line != "" && result.Reason == "" {
		result.Reason = line
	}

	if result.Error == "" && stderr.Len() > 0 && result.Status == "failed" {
		result.Error = firstLine(stderr.String())
	}

	now := time.Now()
	state.LastCheckAt = now.Format(time.RFC3339)
	if result.EventID != "" {
		state.LastEventID = result.EventID
	}
	_ = SaveState(job.ID, state)

	exit := result.ExitCode
	_ = AppendHistory(job.ID, HistoryRecord{
		TS:         now.Format(time.RFC3339),
		JobID:      job.ID,
		Type:       "check",
		Status:     result.Status,
		Trigger:    "script",
		Reason:     result.Reason,
		EventID:    result.EventID,
		DurationMS: result.Duration.Milliseconds(),
		ExitCode:   &exit,
		Error:      result.Error,
	})

	return result, execErr
}

func RunJob(ctx context.Context, job *config.Job, event TriggerEvent) (RunResult, error) {
	start := time.Now()
	startedAt := start.Format(time.RFC3339)
	runDir, err := newRunDir(job.ID, start)
	if err != nil {
		return RunResult{}, err
	}

	triggerPath := filepath.Join(runDir, "trigger.json")
	if err := writeJSON(triggerPath, event); err != nil {
		return RunResult{}, err
	}
	stdoutPath := filepath.Join(runDir, "stdout.log")
	stderrPath := filepath.Join(runDir, "stderr.log")
	stdout, err := os.Create(stdoutPath)
	if err != nil {
		return RunResult{}, err
	}
	defer stdout.Close()
	stderr, err := os.Create(stderrPath)
	if err != nil {
		return RunResult{}, err
	}
	defer stderr.Close()

	state, _ := LoadState(job.ID)
	env, err := commandEnv(job, state, map[string]string{
		"TICKLE_TRIGGER_TYPE": event.Type,
		"TICKLE_TRIGGER_FILE": triggerPath,
		"TICKLE_RUN_DIR":      runDir,
	})
	if err != nil {
		return RunResult{}, err
	}
	env = mergeEnv(env, job.Run.Env)

	if job.Run.Timeout != "" {
		timeout, err := time.ParseDuration(job.Run.Timeout)
		if err != nil {
			return RunResult{}, err
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	exitCode, execErr := execute(ctx, job.Run.Command, job.RunCWD(), envList(env), stdout, stderr)
	endedAt := time.Now().Format(time.RFC3339)

	result := RunResult{
		JobID:     job.ID,
		Status:    "success",
		RunDir:    runDir,
		Stdout:    stdoutPath,
		Stderr:    stderrPath,
		ExitCode:  exitCode,
		Duration:  time.Since(start),
		StartedAt: startedAt,
		EndedAt:   endedAt,
	}
	if ctx.Err() != nil {
		result.Status = "failed"
		result.Error = ctx.Err().Error()
	} else if execErr != nil {
		result.Status = "failed"
		result.Error = execErr.Error()
	}

	if err := writeJSON(filepath.Join(runDir, "result.json"), result); err != nil && result.Error == "" {
		result.Status = "failed"
		result.Error = err.Error()
	}

	now := time.Now()
	state.LastRunAt = now.Format(time.RFC3339)
	if event.EventID != "" {
		state.LastEventID = event.EventID
	}
	if result.Status == "success" {
		state.LastSuccessAt = state.LastRunAt
	} else {
		state.LastFailureAt = state.LastRunAt
	}
	_ = SaveState(job.ID, state)

	exit := result.ExitCode
	_ = AppendHistory(job.ID, HistoryRecord{
		TS:         now.Format(time.RFC3339),
		JobID:      job.ID,
		Type:       "run",
		Status:     result.Status,
		Trigger:    event.Type,
		Reason:     event.Reason,
		EventID:    event.EventID,
		DurationMS: result.Duration.Milliseconds(),
		ExitCode:   &exit,
		RunDir:     result.RunDir,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		Error:      result.Error,
	})

	return result, execErr
}

func LoadState(jobID string) (State, error) {
	path, err := statePath(jobID)
	if err != nil {
		return State{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, err
	}
	defer file.Close()
	var state State
	if err := json.NewDecoder(file).Decode(&state); err != nil {
		return State{}, err
	}
	return state, nil
}

func SaveState(jobID string, state State) error {
	path, err := statePath(jobID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := writeJSON(tmp, state); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func AppendHistory(jobID string, record HistoryRecord) error {
	path, err := historyPath(jobID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func ReadHistory(jobID string, limit int) ([]HistoryRecord, error) {
	path, err := historyPath(jobID)
	if err != nil {
		return nil, err
	}
	lines, err := tailLines(path, limit)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	records := make([]HistoryRecord, 0, len(lines))
	for _, line := range lines {
		var record HistoryRecord
		if err := json.Unmarshal([]byte(line), &record); err == nil {
			records = append(records, record)
		}
	}
	return records, nil
}

func statePath(jobID string) (string, error) {
	dir, err := paths.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, jobID+".json"), nil
}

func historyPath(jobID string) (string, error) {
	dir, err := paths.RunsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, jobID, "history.jsonl"), nil
}

func newRunDir(jobID string, t time.Time) (string, error) {
	base, err := paths.RunsDir()
	if err != nil {
		return "", err
	}
	stamp := t.UTC().Format("20060102T150405.000000000Z")
	dir := filepath.Join(base, jobID, stamp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func commandEnv(job *config.Job, state State, extra map[string]string) (map[string]string, error) {
	configHome, err := paths.ConfigHome()
	if err != nil {
		return nil, err
	}
	dataHome, err := paths.DataHome()
	if err != nil {
		return nil, err
	}
	stateDir, err := paths.StateDir()
	if err != nil {
		return nil, err
	}
	runsDir, err := paths.RunsDir()
	if err != nil {
		return nil, err
	}
	sp, err := statePath(job.ID)
	if err != nil {
		return nil, err
	}

	env := environMap(os.Environ())
	env = mergeEnv(env, job.Env)
	env["TICKLE_JOB_ID"] = job.ID
	env["TICKLE_JOB_NAME"] = job.Name
	env["TICKLE_JOB_FILE"] = job.Path
	env["TICKLE_CONFIG_HOME"] = configHome
	env["TICKLE_DATA_HOME"] = dataHome
	env["TICKLE_STATE_DIR"] = stateDir
	env["TICKLE_STATE_FILE"] = sp
	env["TICKLE_RUNS_DIR"] = runsDir
	env["TICKLE_LAST_CHECK_AT"] = state.LastCheckAt
	env["TICKLE_LAST_RUN_AT"] = state.LastRunAt
	env["TICKLE_LAST_SUCCESS_AT"] = state.LastSuccessAt
	env["TICKLE_LAST_FAILURE_AT"] = state.LastFailureAt
	env["TICKLE_LAST_EVENT_ID"] = state.LastEventID
	env = mergeEnv(env, extra)
	return env, nil
}

func execute(ctx context.Context, command []string, cwd string, env []string, stdout, stderr io.Writer) (int, error) {
	if len(command) == 0 {
		return -1, errors.New("empty command")
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), err
	}
	return -1, err
}

func parseScriptJSON(stdout []byte) (scriptJSON, bool) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return scriptJSON{}, false
	}
	var parsed scriptJSON
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return scriptJSON{}, false
	}
	return parsed, true
}

func firstLine(s string) string {
	scanner := bufio.NewScanner(strings.NewReader(s))
	if scanner.Scan() {
		return scanner.Text()
	}
	return ""
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func environMap(values []string) map[string]string {
	env := make(map[string]string, len(values))
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if ok {
			env[key] = val
		}
	}
	return env
}

func mergeEnv(base map[string]string, updates map[string]string) map[string]string {
	for key, value := range updates {
		base[key] = value
	}
	return base
}

func envList(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+"="+env[key])
	}
	return values
}

func tailLines(path string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 20
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	ring := make([]string, limit)
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		ring[count%limit] = scanner.Text()
		count++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	size := count
	if size > limit {
		size = limit
	}
	out := make([]string, 0, size)
	start := count - size
	for i := 0; i < size; i++ {
		out = append(out, ring[(start+i)%limit])
	}
	return out, nil
}
