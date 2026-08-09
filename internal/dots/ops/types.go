// Package ops defines the mutation contract shared by dots' CLI and TUI.
// Planning is inert: a Plan describes work, but only Runner may execute it.
package ops

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

type ActionID string
type ResourceID string

const (
	ScopeObserve Scope = "observe"
	ScopeLocal   Scope = "local"
	ScopeRepo    Scope = "repository"
	ScopeFleet   Scope = "fleet"
)

type Scope string

const (
	RiskReadOnly   Risk = "read-only"
	RiskReversible Risk = "reversible"
	RiskOutbound   Risk = "outbound"
	RiskDisruptive Risk = "disruptive"
)

type Risk string

// Executable is a typed provider call. Implementations may invoke a process or
// a platform API, but views never receive or construct those details.
type Executable interface {
	Describe() string
	Run(context.Context, IO) error
}

type Step struct {
	ID              string
	Title           string
	Detail          string
	Exec            Executable
	ContinueOnError bool
}

type Plan struct {
	Action       ActionID
	Title        string
	Summary      string
	Target       string
	Scope        Scope
	Risk         Risk
	Confirm      string
	Steps        []Step
	Verification []string
	Recovery     string
	Affects      []ResourceID
	Timeout      time.Duration
}

func (p Plan) Validate() error {
	if p.Action == "" {
		return fmt.Errorf("plan has no action ID")
	}
	if p.Title == "" {
		return fmt.Errorf("plan %s has no title", p.Action)
	}
	if !validScope(p.Scope) {
		return fmt.Errorf("plan %s has invalid scope %q", p.Action, p.Scope)
	}
	if !validRisk(p.Risk) {
		return fmt.Errorf("plan %s has invalid risk %q", p.Action, p.Risk)
	}
	if len(p.Steps) == 0 {
		return fmt.Errorf("plan %s has no steps", p.Action)
	}
	seen := make(map[string]bool, len(p.Steps))
	for i, step := range p.Steps {
		if step.ID == "" {
			return fmt.Errorf("plan %s step %d has no ID", p.Action, i+1)
		}
		if seen[step.ID] {
			return fmt.Errorf("plan %s repeats step ID %q", p.Action, step.ID)
		}
		seen[step.ID] = true
		if step.Title == "" || step.Exec == nil {
			return fmt.Errorf("plan %s step %q is incomplete", p.Action, step.ID)
		}
	}
	return nil
}

func validScope(scope Scope) bool {
	switch scope {
	case ScopeObserve, ScopeLocal, ScopeRepo, ScopeFleet:
		return true
	default:
		return false
	}
}

func validRisk(risk Risk) bool {
	switch risk {
	case RiskReadOnly, RiskReversible, RiskOutbound, RiskDisruptive:
		return true
	default:
		return false
	}
}

func (p Plan) CommandSummary() string {
	parts := make([]string, 0, len(p.Steps))
	for _, step := range p.Steps {
		parts = append(parts, step.Exec.Describe())
	}
	return strings.Join(parts, "  →  ")
}

type IO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Event  func(Event)
}

type EventKind string

const (
	EventPlanStarted EventKind = "plan_started"
	EventStepStarted EventKind = "step_started"
	EventStepDone    EventKind = "step_done"
	EventPlanDone    EventKind = "plan_done"
)

type Event struct {
	Kind   EventKind
	Action ActionID
	StepID string
	Title  string
	Status Status
	Err    error
}

type Status string

const (
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusPartial    Status = "partial"
	StatusSkipped    Status = "skipped"
	StatusCancelled  Status = "cancelled"
	StatusUnverified Status = "unverified"
)

type StepResult struct {
	ID       string
	Title    string
	Status   Status
	ExitCode int
	Error    string
	Started  time.Time
	Ended    time.Time
}

type Result struct {
	Action   ActionID
	Status   Status
	Steps    []StepResult
	Affects  []ResourceID
	Started  time.Time
	Ended    time.Time
	Error    string
	ExitCode int
}

func (r Result) OK() bool { return r.Status == StatusCompleted }

// ExitError lets providers preserve a process' useful exit status without
// coupling the operation layer to os/exec.
type ExitError interface {
	error
	ExitCode() int
}

// SkipError is a successful conditional outcome: the step was considered but
// had no work to do. It is distinct from both failure and cancellation so a
// plan can report why a later mutation was deliberately not run.
type SkipError struct{ Reason string }

func (e SkipError) Error() string { return e.Reason }

func Skip(reason string) error { return SkipError{Reason: reason} }
