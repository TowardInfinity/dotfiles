package ops_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
)

type request struct{ id ops.ActionID }

func (r request) ActionID() ops.ActionID { return r.id }

type executable struct {
	label string
	run   func() error
}

type contextExecutable func(context.Context) error

func (e contextExecutable) Describe() string                        { return "context" }
func (e contextExecutable) Run(ctx context.Context, _ ops.IO) error { return e(ctx) }

func (e executable) Describe() string { return e.label }
func (e executable) Run(context.Context, ops.IO) error {
	if e.run == nil {
		return nil
	}
	return e.run()
}

func TestRegistryBuildsAndValidatesPlans(t *testing.T) {
	registry := ops.NewRegistry()
	err := registry.Register(ops.Definition{
		ID: "test.apply", Summary: "Apply test configuration", Scope: ops.ScopeLocal, Risk: ops.RiskReversible,
		Plan: func(req ops.Request) (ops.Plan, error) {
			return ops.Plan{Title: "Apply", Steps: []ops.Step{{ID: "one", Title: "One", Exec: executable{label: "true"}}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := registry.Build(request{id: "test.apply"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != "test.apply" || plan.Summary != "Apply test configuration" || plan.Scope != ops.ScopeLocal || plan.Risk != ops.RiskReversible {
		t.Fatalf("defaults were not applied: %#v", plan)
	}
	if got := plan.CommandSummary(); got != "true" {
		t.Fatalf("summary = %q", got)
	}
}

func TestRunnerStopsAndMarksRemainingSteps(t *testing.T) {
	want := errors.New("boom")
	plan := ops.Plan{Action: "test", Title: "Test", Scope: ops.ScopeLocal, Risk: ops.RiskReversible, Steps: []ops.Step{
		{ID: "ok", Title: "OK", Exec: executable{label: "ok"}},
		{ID: "bad", Title: "Bad", Exec: executable{label: "bad", run: func() error { return want }}},
		{ID: "never", Title: "Never", Exec: executable{label: "never", run: func() error { t.Fatal("skipped step ran"); return nil }}},
	}}
	result := ops.NewRunner().Run(context.Background(), plan, ops.IO{Stdout: io.Discard, Stderr: io.Discard})
	if result.Status != ops.StatusFailed || len(result.Steps) != 3 || result.Steps[2].Status != ops.StatusSkipped {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRunnerRejectsConcurrentMutation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	plan := ops.Plan{Action: "slow", Title: "Slow", Scope: ops.ScopeLocal, Risk: ops.RiskReversible, Steps: []ops.Step{{
		ID: "wait", Title: "Wait", Exec: executable{label: "wait", run: func() error {
			once.Do(func() { close(started) })
			<-release
			return nil
		}},
	}}}
	runner := ops.NewRunner()
	done := make(chan ops.Result, 1)
	go func() { done <- runner.Run(context.Background(), plan, ops.IO{}) }()
	<-started
	second := runner.Run(context.Background(), plan, ops.IO{})
	if second.Error != ops.ErrBusy.Error() {
		t.Fatalf("second result = %#v", second)
	}
	close(release)
	<-done
}

func TestRunnerCancellationStopsContinueOnErrorPlan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	plan := ops.Plan{Action: "fleet", Title: "Fleet", Scope: ops.ScopeFleet, Risk: ops.RiskDisruptive, Steps: []ops.Step{
		{ID: "cancel", Title: "Cancel", ContinueOnError: true, Exec: contextExecutable(func(context.Context) error {
			cancel()
			return context.Canceled
		})},
		{ID: "never", Title: "Never", Exec: executable{label: "never", run: func() error {
			t.Fatal("step after cancellation ran")
			return nil
		}}},
	}}
	result := ops.NewRunner().Run(ctx, plan, ops.IO{})
	if result.Status != ops.StatusCancelled || result.Steps[1].Status != ops.StatusSkipped {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunnerRecordsConditionalSkipWithoutFailingPlan(t *testing.T) {
	ran := false
	var skipped ops.Event
	plan := ops.Plan{Action: "conditional", Title: "Conditional", Scope: ops.ScopeLocal, Risk: ops.RiskReversible, Steps: []ops.Step{
		{ID: "skip", Title: "Skip", Exec: executable{label: "skip", run: func() error { return ops.Skip("already current") }}},
		{ID: "next", Title: "Next", Exec: executable{label: "next", run: func() error { ran = true; return nil }}},
	}}
	result := ops.NewRunner().Run(context.Background(), plan, ops.IO{Event: func(event ops.Event) {
		if event.Kind == ops.EventStepDone && event.Status == ops.StatusSkipped {
			skipped = event
		}
	}})
	if !result.OK() || result.Steps[0].Status != ops.StatusSkipped || result.Steps[0].Error != "already current" || !ran {
		t.Fatalf("result = %#v, next ran = %t", result, ran)
	}
	if skipped.Err == nil || skipped.Err.Error() != "already current" {
		t.Fatalf("skip event = %#v", skipped)
	}
}
