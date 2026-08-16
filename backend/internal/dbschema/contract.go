package dbschema

import (
	"context"
	"fmt"
)

// ContractOptions carries the caller-verified preconditions that ADR 0005
// requires before a contract migration may run. The runner cannot observe
// these facts itself: data-backfill completion (Group 2 ledger), live
// incompatible instances (heartbeat), backup evidence, and drain or
// maintenance state all belong to the maintenance command that drives the
// runner. Every non-nil hook must succeed, for dry-runs as well, so operators
// see exactly what blocks execution.
type ContractOptions struct {
	DryRun           bool
	RequireBackfills func(ctx context.Context) error
	RequireCluster   func(ctx context.Context) error
	RequireBackup    func(ctx context.Context) error
	RequireWindow    func(ctx context.Context) error
}

// ContractPlan reports what an ApplyContract call would execute.
type ContractPlan struct {
	Migrations []Migration
	DryRun     bool
}

// PlanContract lists pending contract migrations for the dialect after ledger
// verification.
func (r *Runner) PlanContract(ctx context.Context) (ContractPlan, error) {
	if err := r.ensureLedger(ctx); err != nil {
		return ContractPlan{}, err
	}
	applied, err := r.loadApplied(ctx)
	if err != nil {
		return ContractPlan{}, err
	}
	if err := r.verifyApplied(applied); err != nil {
		return ContractPlan{}, err
	}
	if findApplied(applied, BaselineVersion) == nil {
		return ContractPlan{}, newError(ErrCodeBaselineMissing, BaselineVersion, errNoBaseline)
	}
	return ContractPlan{Migrations: r.pendingContracts(applied)}, nil
}

// ApplyContract executes pending contract migrations after every configured
// precondition passes. A dry run performs the same verification but executes
// nothing. Ordinary startup paths never call this (ADR 0005).
func (r *Runner) ApplyContract(ctx context.Context, options ContractOptions) (Result, error) {
	plan, err := r.PlanContract(ctx)
	if err != nil {
		return Result{}, err
	}
	plan.DryRun = options.DryRun
	if err := r.runContractPreconditions(ctx, options); err != nil {
		return Result{}, err
	}
	if options.DryRun || len(plan.Migrations) == 0 {
		return Result{}, nil
	}
	release, err := r.acquireLock(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()
	applied, err := r.loadApplied(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := r.verifyApplied(applied); err != nil {
		return Result{}, err
	}
	result := Result{}
	for _, m := range r.pendingContracts(applied) {
		record, outcome, applyErr := r.applyMigration(ctx, m)
		if applyErr != nil {
			return result, applyErr
		}
		if outcome == "success" {
			result.Applied = append(result.Applied, record)
		}
	}
	return result, nil
}

func (r *Runner) runContractPreconditions(ctx context.Context, options ContractOptions) error {
	checks := []struct {
		name string
		fn   func(ctx context.Context) error
	}{
		{"data_backfills_complete", options.RequireBackfills},
		{"cluster_compatible", options.RequireCluster},
		{"backup_evidence", options.RequireBackup},
		{"maintenance_window", options.RequireWindow},
	}
	for _, check := range checks {
		if check.fn == nil {
			continue
		}
		if err := check.fn(ctx); err != nil {
			return newError(ErrCodeContractPrecondition, 0, fmt.Errorf("%s: %w", check.name, err))
		}
	}
	return nil
}

func (r *Runner) pendingContracts(applied []Applied) []Migration {
	appliedSet := make(map[int64]bool, len(applied))
	for _, row := range applied {
		appliedSet[row.Version] = true
	}
	var pending []Migration
	for _, m := range r.registry {
		if appliedSet[m.Version] || m.Phase != PhaseContract || !m.appliesTo(r.dialect) {
			continue
		}
		pending = append(pending, m)
	}
	return pending
}
