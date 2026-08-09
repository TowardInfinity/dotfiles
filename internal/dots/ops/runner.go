package ops

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrBusy = errors.New("another mutation is already running")

type Runner struct {
	mu      sync.Mutex
	running bool
}

func NewRunner() *Runner { return &Runner{} }

func (r *Runner) Run(ctx context.Context, plan Plan, streams IO) Result {
	started := time.Now()
	result := Result{Action: plan.Action, Status: StatusCompleted, Affects: append([]ResourceID(nil), plan.Affects...), Started: started}
	if err := plan.Validate(); err != nil {
		result.Status, result.Error, result.ExitCode, result.Ended = StatusFailed, err.Error(), 1, time.Now()
		return result
	}
	if !r.acquire() {
		result.Status, result.Error, result.ExitCode, result.Ended = StatusFailed, ErrBusy.Error(), 1, time.Now()
		return result
	}
	defer r.release()

	emit(streams, Event{Kind: EventPlanStarted, Action: plan.Action, Title: plan.Title})
	partial := false
	for i, step := range plan.Steps {
		sr := StepResult{ID: step.ID, Title: step.Title, Status: StatusCompleted, Started: time.Now()}
		emit(streams, Event{Kind: EventStepStarted, Action: plan.Action, StepID: step.ID, Title: step.Title})
		err := step.Exec.Run(ctx, streams)
		sr.Ended = time.Now()
		if err != nil {
			var skipErr SkipError
			if errors.As(err, &skipErr) {
				sr.Status = StatusSkipped
				sr.Error = skipErr.Reason
				result.Steps = append(result.Steps, sr)
				emit(streams, Event{Kind: EventStepDone, Action: plan.Action, StepID: step.ID, Title: step.Title, Status: sr.Status, Err: err})
				continue
			}
			sr.Error = err.Error()
			sr.Status = StatusFailed
			sr.ExitCode = 1
			var exitErr ExitError
			if errors.As(err, &exitErr) {
				sr.ExitCode = exitErr.ExitCode()
			}
			if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				sr.Status = StatusCancelled
			}
		}
		result.Steps = append(result.Steps, sr)
		emit(streams, Event{Kind: EventStepDone, Action: plan.Action, StepID: step.ID, Title: step.Title, Status: sr.Status, Err: err})
		if err == nil {
			continue
		}
		// Cancellation is a request to stop the operation, not a failed target
		// that ContinueOnError may skip past. In particular, cancelling a fleet
		// rollout must not start the remaining hosts with an already-dead context.
		if sr.Status == StatusCancelled {
			result.Status, result.Error, result.ExitCode = sr.Status, sr.Error, sr.ExitCode
			for _, rest := range plan.Steps[i+1:] {
				result.Steps = append(result.Steps, StepResult{ID: rest.ID, Title: rest.Title, Status: StatusSkipped})
			}
			break
		}
		if step.ContinueOnError {
			partial = true
			result.Error = sr.Error
			result.ExitCode = sr.ExitCode
			continue
		}

		result.Status, result.Error, result.ExitCode = sr.Status, sr.Error, sr.ExitCode
		for _, rest := range plan.Steps[i+1:] {
			result.Steps = append(result.Steps, StepResult{ID: rest.ID, Title: rest.Title, Status: StatusSkipped})
		}
		break
	}
	result.Ended = time.Now()
	if partial && result.Status == StatusCompleted {
		result.Status = StatusPartial
	}
	if result.Status == StatusCompleted && len(result.Steps) != len(plan.Steps) {
		result.Status, result.Error, result.ExitCode = StatusFailed, "operation ended before every step ran", 1
	}
	emit(streams, Event{Kind: EventPlanDone, Action: plan.Action, Title: plan.Title, Status: result.Status, Err: resultErr(result)})
	return result
}

func (r *Runner) acquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return false
	}
	r.running = true
	return true
}

func (r *Runner) release() {
	r.mu.Lock()
	r.running = false
	r.mu.Unlock()
}

func emit(streams IO, event Event) {
	if streams.Event != nil {
		streams.Event(event)
	}
}

func resultErr(result Result) error {
	if result.Error == "" {
		return nil
	}
	return fmt.Errorf("%s", result.Error)
}
