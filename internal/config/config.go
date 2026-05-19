package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"

	"github.com/callumalpass/tickle/internal/paths"
)

var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

type Job struct {
	Version  int               `yaml:"version"`
	ID       string            `yaml:"id"`
	Name     string            `yaml:"name"`
	Status   string            `yaml:"status"`
	Trigger  *Trigger          `yaml:"trigger"`
	Triggers []Trigger         `yaml:"triggers"`
	Run      CommandSpec       `yaml:"run"`
	Env      map[string]string `yaml:"env"`

	Path string `yaml:"-"`
	Dir  string `yaml:"-"`
}

type Trigger struct {
	Type     string            `yaml:"type"`
	Schedule string            `yaml:"schedule"`
	Every    string            `yaml:"every"`
	Command  []string          `yaml:"command"`
	CWD      string            `yaml:"cwd"`
	Timeout  string            `yaml:"timeout"`
	Env      map[string]string `yaml:"env"`
}

type CommandSpec struct {
	CWD     string            `yaml:"cwd"`
	Command []string          `yaml:"command"`
	Timeout string            `yaml:"timeout"`
	Env     map[string]string `yaml:"env"`
}

func LoadJob(path string) (*Job, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	dec := yaml.NewDecoder(file)
	dec.KnownFields(true)

	var job Job
	if err := dec.Decode(&job); err != nil {
		return nil, err
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	job.Path = abs
	job.Dir = filepath.Dir(abs)

	if err := job.NormalizeAndValidate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &job, nil
}

func LoadJobs(dir string) ([]*Job, error) {
	patterns := []string{filepath.Join(dir, "*.yaml"), filepath.Join(dir, "*.yml")}
	var files []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		files = append(files, matches...)
	}
	slices.Sort(files)

	jobs := make([]*Job, 0, len(files))
	for _, file := range files {
		job, err := LoadJob(file)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func ResolveJob(target string) (*Job, error) {
	if target == "" {
		return nil, errors.New("missing job id or path")
	}
	if ok, err := paths.ExistingFile(target); err != nil {
		return nil, err
	} else if ok {
		return LoadJob(target)
	}
	jobsDir, err := paths.JobsDir()
	if err != nil {
		return nil, err
	}
	for _, ext := range []string{".yaml", ".yml"} {
		candidate := filepath.Join(jobsDir, target+ext)
		if ok, err := paths.ExistingFile(candidate); err != nil {
			return nil, err
		} else if ok {
			return LoadJob(candidate)
		}
	}
	return nil, fmt.Errorf("job %q not found as a file or in %s", target, jobsDir)
}

func (j *Job) NormalizeAndValidate() error {
	if j.Version == 0 {
		j.Version = 1
	}
	if j.Version != 1 {
		return fmt.Errorf("unsupported version %d", j.Version)
	}
	j.ID = strings.TrimSpace(j.ID)
	if j.ID == "" {
		return errors.New("id is required")
	}
	if !validID.MatchString(j.ID) {
		return fmt.Errorf("id %q must match %s", j.ID, validID.String())
	}
	if j.Name == "" {
		j.Name = j.ID
	}
	j.Status = strings.ToLower(strings.TrimSpace(j.Status))
	if j.Status == "" {
		j.Status = "active"
	}
	if j.Status != "active" && j.Status != "disabled" {
		return fmt.Errorf("status must be active or disabled, got %q", j.Status)
	}
	if j.Trigger != nil && len(j.Triggers) > 0 {
		return errors.New("use either trigger or triggers, not both")
	}
	if j.Trigger != nil {
		j.Triggers = []Trigger{*j.Trigger}
		j.Trigger = nil
	}
	if len(j.Triggers) == 0 {
		j.Triggers = []Trigger{{Type: "manual"}}
	}
	if len(j.Run.Command) == 0 {
		return errors.New("run.command is required")
	}
	if j.Env == nil {
		j.Env = map[string]string{}
	}
	if err := validateCommandSpec("run", j.Run); err != nil {
		return err
	}
	for i := range j.Triggers {
		if err := validateTrigger(i, &j.Triggers[i]); err != nil {
			return err
		}
	}
	return nil
}

func validateCommandSpec(prefix string, spec CommandSpec) error {
	if len(spec.Command) == 0 {
		return fmt.Errorf("%s.command is required", prefix)
	}
	if strings.TrimSpace(spec.Command[0]) == "" {
		return fmt.Errorf("%s.command[0] is empty", prefix)
	}
	if spec.Timeout != "" {
		if _, err := time.ParseDuration(spec.Timeout); err != nil {
			return fmt.Errorf("%s.timeout: %w", prefix, err)
		}
	}
	return nil
}

func validateTrigger(i int, trigger *Trigger) error {
	prefix := fmt.Sprintf("triggers[%d]", i)
	trigger.Type = strings.ToLower(strings.TrimSpace(trigger.Type))
	if trigger.Type == "" {
		trigger.Type = "manual"
	}
	switch trigger.Type {
	case "manual":
		return nil
	case "cron":
		if trigger.Schedule == "" {
			return fmt.Errorf("%s.schedule is required for cron triggers", prefix)
		}
		if _, err := CronParser().Parse(trigger.Schedule); err != nil {
			return fmt.Errorf("%s.schedule: %w", prefix, err)
		}
	case "interval":
		if trigger.Every == "" {
			return fmt.Errorf("%s.every is required for interval triggers", prefix)
		}
		if _, err := time.ParseDuration(trigger.Every); err != nil {
			return fmt.Errorf("%s.every: %w", prefix, err)
		}
	case "script":
		if len(trigger.Command) == 0 {
			return fmt.Errorf("%s.command is required for script triggers", prefix)
		}
		if strings.TrimSpace(trigger.Command[0]) == "" {
			return fmt.Errorf("%s.command[0] is empty", prefix)
		}
		if trigger.Schedule == "" && trigger.Every == "" {
			return fmt.Errorf("%s.schedule or %s.every is required for script triggers", prefix, prefix)
		}
		if trigger.Schedule != "" {
			if _, err := CronParser().Parse(trigger.Schedule); err != nil {
				return fmt.Errorf("%s.schedule: %w", prefix, err)
			}
		}
		if trigger.Every != "" {
			if _, err := time.ParseDuration(trigger.Every); err != nil {
				return fmt.Errorf("%s.every: %w", prefix, err)
			}
		}
	default:
		return fmt.Errorf("%s.type must be cron, interval, script, or manual, got %q", prefix, trigger.Type)
	}
	if trigger.Timeout != "" {
		if _, err := time.ParseDuration(trigger.Timeout); err != nil {
			return fmt.Errorf("%s.timeout: %w", prefix, err)
		}
	}
	return nil
}

func (j *Job) Active() bool {
	return j.Status == "active"
}

func (j *Job) RunCWD() string {
	if j.Run.CWD != "" {
		return resolveRelative(j.Dir, j.Run.CWD)
	}
	return j.Dir
}

func (j *Job) TriggerCWD(trigger Trigger) string {
	if trigger.CWD != "" {
		return resolveRelative(j.Dir, trigger.CWD)
	}
	if j.Run.CWD != "" {
		return j.RunCWD()
	}
	return j.Dir
}

func resolveRelative(base, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(base, value))
}

func CronParser() cron.Parser {
	return cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
}
