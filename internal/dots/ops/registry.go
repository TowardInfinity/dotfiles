package ops

import (
	"fmt"
	"sync"
)

// Request is deliberately small. Concrete requests carry typed parameters;
// the registry owns their planner and rejects mismatched request types.
type Request interface {
	ActionID() ActionID
}

type Planner func(Request) (Plan, error)

type Definition struct {
	ID      ActionID
	Summary string
	Scope   Scope
	Risk    Risk
	Plan    Planner
}

type Registry struct {
	mu   sync.RWMutex
	defs map[ActionID]Definition
}

func NewRegistry() *Registry { return &Registry{defs: make(map[ActionID]Definition)} }

func (r *Registry) Register(def Definition) error {
	if def.ID == "" || def.Summary == "" || !validScope(def.Scope) || !validRisk(def.Risk) || def.Plan == nil {
		return fmt.Errorf("operation definition is incomplete")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.defs[def.ID]; exists {
		return fmt.Errorf("operation %q is already registered", def.ID)
	}
	r.defs[def.ID] = def
	return nil
}

func (r *Registry) Build(req Request) (Plan, error) {
	if req == nil {
		return Plan{}, fmt.Errorf("nil operation request")
	}
	r.mu.RLock()
	def, ok := r.defs[req.ActionID()]
	r.mu.RUnlock()
	if !ok {
		return Plan{}, fmt.Errorf("unknown operation %q", req.ActionID())
	}
	plan, err := def.Plan(req)
	if err != nil {
		return Plan{}, err
	}
	if plan.Action == "" {
		plan.Action = def.ID
	}
	if plan.Summary == "" {
		plan.Summary = def.Summary
	}
	if plan.Scope == "" {
		plan.Scope = def.Scope
	}
	if plan.Risk == "" {
		plan.Risk = def.Risk
	}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (r *Registry) Definitions() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]Definition, 0, len(r.defs))
	for _, def := range r.defs {
		defs = append(defs, def)
	}
	return defs
}
