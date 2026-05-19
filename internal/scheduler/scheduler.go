package scheduler

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/callumalpass/tickle/internal/config"
	"github.com/callumalpass/tickle/internal/paths"
	"github.com/callumalpass/tickle/internal/runner"
)

type Daemon struct {
	jobs    []*config.Job
	cron    *cron.Cron
	mu      sync.Mutex
	running map[string]bool
}

func New(jobs []*config.Job) *Daemon {
	return &Daemon{
		jobs: jobs,
		cron: cron.New(
			cron.WithParser(config.CronParser()),
			cron.WithChain(cron.Recover(cron.DefaultLogger)),
		),
		running: map[string]bool{},
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
	return New(jobs), nil
}

func (d *Daemon) Run(ctx context.Context) error {
	active := 0
	for _, job := range d.jobs {
		if !job.Active() {
			log.Printf("job %s is disabled", job.ID)
			continue
		}
		for _, trigger := range job.Triggers {
			if trigger.Type == "manual" {
				continue
			}
			active++
			if err := d.registerTrigger(ctx, job, trigger); err != nil {
				return err
			}
		}
	}
	log.Printf("tickle daemon loaded %d jobs, %d active scheduled triggers", len(d.jobs), active)

	d.cron.Start()
	defer d.cron.Stop()

	<-ctx.Done()
	log.Printf("tickle daemon stopping")
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

func (d *Daemon) registerTrigger(ctx context.Context, job *config.Job, trigger config.Trigger) error {
	switch trigger.Type {
	case "cron":
		_, err := d.cron.AddFunc(trigger.Schedule, func() {
			d.runJob(ctx, job, runner.TriggerEvent{Type: "cron", Source: trigger.Schedule, Reason: "cron schedule matched"})
		})
		return err
	case "interval":
		every, err := time.ParseDuration(trigger.Every)
		if err != nil {
			return err
		}
		go d.intervalLoop(ctx, every, func() {
			d.runJob(ctx, job, runner.TriggerEvent{Type: "interval", Source: trigger.Every, Reason: "interval elapsed"})
		})
		return nil
	case "script":
		if trigger.Schedule != "" {
			_, err := d.cron.AddFunc(trigger.Schedule, func() {
				d.checkScript(ctx, job, trigger)
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
			go d.intervalLoop(ctx, every, func() {
				d.checkScript(ctx, job, trigger)
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

func (d *Daemon) checkScript(ctx context.Context, job *config.Job, trigger config.Trigger) {
	result, _ := runner.EvaluateScriptTrigger(ctx, job, trigger)
	log.Printf("check %s: %s (%s)", job.ID, result.Status, result.Reason)
	if result.Status == "failed" {
		return
	}
	if !result.ShouldRun {
		return
	}
	d.runJob(ctx, job, runner.TriggerEvent{
		Type:    "script",
		Source:  "script trigger",
		Reason:  result.Reason,
		EventID: result.EventID,
		Payload: result.Payload,
	})
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
