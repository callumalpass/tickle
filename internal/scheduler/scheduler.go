package scheduler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/callumalpass/tickle/internal/config"
	"github.com/callumalpass/tickle/internal/paths"
	"github.com/callumalpass/tickle/internal/runner"
)

const defaultReloadInterval = 5 * time.Second

type Daemon struct {
	scheduleMu     sync.Mutex
	jobs           []*config.Job
	jobsDir        string
	cron           *cron.Cron
	scheduleCancel context.CancelFunc
	fingerprint    jobsFingerprint
	reloadEvery    time.Duration
	loadJobs       func(string) ([]*config.Job, error)

	mu      sync.Mutex
	running map[string]bool
}

func New(jobs []*config.Job) *Daemon {
	return &Daemon{
		jobs:        jobs,
		reloadEvery: defaultReloadInterval,
		loadJobs:    config.LoadJobs,
		running:     map[string]bool{},
	}
}

func LoadDefault() (*Daemon, error) {
	if err := paths.EnsureBaseDirs(); err != nil {
		return nil, err
	}
	jobsDir, err := paths.JobsDir()
	if err != nil {
		return nil, err
	}
	jobs, err := config.LoadJobs(jobsDir)
	if err != nil {
		return nil, err
	}
	fingerprint, err := fingerprintJobsDir(jobsDir)
	if err != nil {
		return nil, err
	}
	daemon := New(jobs)
	daemon.jobsDir = jobsDir
	daemon.fingerprint = fingerprint
	return daemon, nil
}

func (d *Daemon) Run(ctx context.Context) error {
	if err := d.replaceSchedule(ctx, d.jobs, "loaded"); err != nil {
		return err
	}
	var reloadDone <-chan struct{}
	if d.jobsDir != "" && d.reloadEvery > 0 {
		done := make(chan struct{})
		reloadDone = done
		go func() {
			defer close(done)
			d.reloadLoop(ctx)
		}()
	}

	<-ctx.Done()
	if reloadDone != nil {
		<-reloadDone
	}
	log.Printf("tickle daemon stopping")
	d.stopSchedule()
	return nil
}

func RunForeground() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	daemon, err := LoadDefault()
	if err != nil {
		return err
	}
	return daemon.Run(ctx)
}

func (d *Daemon) replaceSchedule(runCtx context.Context, jobs []*config.Job, action string) error {
	if err := runCtx.Err(); err != nil {
		return err
	}

	nextCron := newCron()
	scheduleCtx, scheduleCancel := context.WithCancel(runCtx)
	var intervals []func()
	active := 0
	for _, job := range jobs {
		if !job.Active() {
			log.Printf("job %s is disabled", job.ID)
			continue
		}
		for _, trigger := range job.Triggers {
			if trigger.Type == "manual" {
				continue
			}
			active++
			if err := d.registerTrigger(scheduleCtx, runCtx, nextCron, &intervals, job, trigger); err != nil {
				scheduleCancel()
				return err
			}
		}
	}
	if err := runCtx.Err(); err != nil {
		scheduleCancel()
		return err
	}

	d.scheduleMu.Lock()
	oldCron := d.cron
	oldCancel := d.scheduleCancel
	d.jobs = jobs
	d.cron = nextCron
	d.scheduleCancel = scheduleCancel
	d.scheduleMu.Unlock()

	if oldCancel != nil {
		oldCancel()
	}
	if oldCron != nil {
		oldCron.Stop()
	}
	for _, startInterval := range intervals {
		go startInterval()
	}
	nextCron.Start()

	log.Printf("tickle daemon %s %d jobs, %d active scheduled triggers", action, len(jobs), active)
	return nil
}

func (d *Daemon) stopSchedule() {
	d.scheduleMu.Lock()
	currentCron := d.cron
	currentCancel := d.scheduleCancel
	d.cron = nil
	d.scheduleCancel = nil
	d.scheduleMu.Unlock()

	if currentCancel != nil {
		currentCancel()
	}
	if currentCron != nil {
		currentCron.Stop()
	}
}

func (d *Daemon) reloadLoop(ctx context.Context) {
	ticker := time.NewTicker(d.reloadEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.reloadIfChanged(ctx)
		}
	}
}

func (d *Daemon) reloadIfChanged(ctx context.Context) {
	if err := ctx.Err(); err != nil {
		return
	}
	before, err := fingerprintJobsDir(d.jobsDir)
	if err != nil {
		log.Printf("tickle daemon config reload skipped: %v", err)
		return
	}
	if d.sameFingerprint(before) {
		return
	}

	loadJobs := d.loadJobs
	if loadJobs == nil {
		loadJobs = config.LoadJobs
	}
	jobs, err := loadJobs(d.jobsDir)
	if err != nil {
		log.Printf("tickle daemon config reload skipped: %v", err)
		return
	}
	after, err := fingerprintJobsDir(d.jobsDir)
	if err != nil {
		log.Printf("tickle daemon config reload skipped: %v", err)
		return
	}
	if !before.Equal(after) {
		log.Printf("tickle daemon config reload deferred: job files changed while loading")
		return
	}
	if err := d.replaceSchedule(ctx, jobs, "reloaded"); err != nil {
		log.Printf("tickle daemon config reload skipped: %v", err)
		return
	}
	d.setFingerprint(after)
}

func (d *Daemon) registerTrigger(scheduleCtx, runCtx context.Context, scheduler *cron.Cron, intervals *[]func(), job *config.Job, trigger config.Trigger) error {
	switch trigger.Type {
	case "cron":
		cronJob := job
		cronTrigger := trigger
		_, err := scheduler.AddFunc(cronTrigger.Schedule, func() {
			d.runJob(runCtx, cronJob, runner.TriggerEvent{Type: "cron", Source: cronTrigger.Schedule, Reason: "cron schedule matched"})
		})
		return err
	case "interval":
		every, err := time.ParseDuration(trigger.Every)
		if err != nil {
			return err
		}
		intervalJob := job
		intervalTrigger := trigger
		*intervals = append(*intervals, func() {
			d.intervalLoop(scheduleCtx, every, func() {
				d.runJob(runCtx, intervalJob, runner.TriggerEvent{Type: "interval", Source: intervalTrigger.Every, Reason: "interval elapsed"})
			})
		})
		return nil
	case "script":
		if trigger.Schedule != "" {
			scriptJob := job
			scriptTrigger := trigger
			_, err := scheduler.AddFunc(scriptTrigger.Schedule, func() {
				d.checkScript(scheduleCtx, runCtx, scriptJob, scriptTrigger)
			})
			if err != nil {
				return err
			}
		}
		if trigger.Every != "" {
			every, err := time.ParseDuration(trigger.Every)
			if err != nil {
				return err
			}
			intervalJob := job
			intervalTrigger := trigger
			*intervals = append(*intervals, func() {
				d.intervalLoop(scheduleCtx, every, func() {
					d.checkScript(scheduleCtx, runCtx, intervalJob, intervalTrigger)
				})
			})
		}
		return nil
	default:
		return fmt.Errorf("unsupported trigger type %q", trigger.Type)
	}
}

func (d *Daemon) intervalLoop(ctx context.Context, every time.Duration, fn func()) {
	timer := time.NewTimer(every)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			fn()
			timer.Reset(every)
		}
	}
}

func (d *Daemon) checkScript(checkCtx, runCtx context.Context, job *config.Job, trigger config.Trigger) {
	result, _ := runner.EvaluateScriptTrigger(checkCtx, job, trigger)
	log.Printf("check %s: %s (%s)", job.ID, result.Status, result.Reason)
	if result.Status == "failed" {
		return
	}
	if !result.ShouldRun {
		return
	}
	d.runJob(runCtx, job, runner.TriggerEvent{
		Type:    "script",
		Source:  "script trigger",
		Reason:  result.Reason,
		EventID: result.EventID,
		Payload: result.Payload,
	})
}

func newCron() *cron.Cron {
	return cron.New(
		cron.WithParser(config.CronParser()),
		cron.WithChain(cron.Recover(cron.DefaultLogger)),
	)
}

type jobsFingerprint struct {
	files []jobFileFingerprint
}

type jobFileFingerprint struct {
	path string
	sum  [32]byte
}

func fingerprintJobsDir(dir string) (jobsFingerprint, error) {
	patterns := []string{filepath.Join(dir, "*.yaml"), filepath.Join(dir, "*.yml")}
	var files []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return jobsFingerprint{}, err
		}
		files = append(files, matches...)
	}
	sort.Strings(files)

	fingerprint := jobsFingerprint{files: make([]jobFileFingerprint, 0, len(files))}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return jobsFingerprint{}, err
		}
		fingerprint.files = append(fingerprint.files, jobFileFingerprint{
			path: file,
			sum:  sha256.Sum256(data),
		})
	}
	return fingerprint, nil
}

func (f jobsFingerprint) Equal(other jobsFingerprint) bool {
	if len(f.files) != len(other.files) {
		return false
	}
	for i := range f.files {
		if f.files[i] != other.files[i] {
			return false
		}
	}
	return true
}

func (d *Daemon) sameFingerprint(fingerprint jobsFingerprint) bool {
	d.scheduleMu.Lock()
	defer d.scheduleMu.Unlock()
	return d.fingerprint.Equal(fingerprint)
}

func (d *Daemon) setFingerprint(fingerprint jobsFingerprint) {
	d.scheduleMu.Lock()
	defer d.scheduleMu.Unlock()
	d.fingerprint = fingerprint
}

func (d *Daemon) runJob(ctx context.Context, job *config.Job, event runner.TriggerEvent) {
	if !d.tryStart(job.ID) {
		log.Printf("job %s is already running; skipping trigger %s", job.ID, event.Type)
		return
	}
	defer d.finish(job.ID)

	log.Printf("run %s: started by %s", job.ID, event.Type)
	result, _ := runner.RunJob(ctx, job, event)
	if result.Status == "success" {
		log.Printf("run %s: success in %s", job.ID, result.Duration.Round(time.Millisecond))
	} else {
		log.Printf("run %s: failed in %s: %s", job.ID, result.Duration.Round(time.Millisecond), result.Error)
	}
}

func (d *Daemon) tryStart(jobID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running[jobID] {
		return false
	}
	d.running[jobID] = true
	return true
}

func (d *Daemon) finish(jobID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.running, jobID)
}
