package main

import (
	"testing"

	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
	"github.com/TowardInfinity/dotfiles/internal/dots/providers"
)

func testCommandPlan(title string, argv ...string) ops.Plan {
	return ops.Plan{
		Action: "test.command", Scope: ops.ScopeLocal, Risk: ops.RiskReversible,
		Title: title,
		Steps: []ops.Step{{
			ID: "command", Title: title, Exec: providers.Command("", argv...),
		}},
	}
}

func planProcess(t *testing.T, plan ops.Plan, step int) providers.Process {
	t.Helper()
	if step < 0 || step >= len(plan.Steps) {
		t.Fatalf("step %d is out of range for %#v", step, plan)
	}
	process, ok := plan.Steps[step].Exec.(providers.Process)
	if !ok {
		t.Fatalf("step %d uses %T, want providers.Process", step, plan.Steps[step].Exec)
	}
	return process
}
