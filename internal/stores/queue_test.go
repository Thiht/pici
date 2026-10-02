package stores

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExecutionQueue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	projectID := mustUUID("11111111-1111-1111-1111-111111111111")

	if _, err := s.CreateProject(ctx, Project{ID: projectID, Name: "demo"}); err != nil {
		t.Fatal(err)
	}

	first := Execution{ProjectID: projectID, Workflow: "build", Status: StatusPending, CreatedAt: time.Now()}
	if err := s.CreateExecution(ctx, &first); err != nil {
		t.Fatal(err)
	}
	second := Execution{ProjectID: projectID, Workflow: "test", Status: StatusPending, CreatedAt: time.Now()}
	if err := s.CreateExecution(ctx, &second); err != nil {
		t.Fatal(err)
	}

	if n, err := s.CountPendingExecutions(ctx); err != nil || n != 2 {
		t.Fatalf("expected 2 pending executions, got %d (err=%v)", n, err)
	}

	// Workers claim the oldest pending execution first.
	claimed, err := s.ClaimPendingExecution(ctx, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != first.ID || claimed.Status != StatusRunning || claimed.StartedAt == nil {
		t.Fatalf("unexpected claimed execution: %+v", claimed)
	}
	if next, err := s.ClaimPendingExecution(ctx, "worker-2"); err != nil || next.ID != second.ID {
		t.Fatalf("unexpected second claim: %+v (err=%v)", next, err)
	}
	if _, err := s.ClaimPendingExecution(ctx, "worker-3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on an empty queue, got %v", err)
	}
	if n, err := s.CountPendingExecutions(ctx); err != nil || n != 0 {
		t.Fatalf("expected no pending execution left, got %d (err=%v)", n, err)
	}

	if err := s.SetCancelRequested(ctx, projectID, first.ID); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.IsCancelRequested(ctx, projectID, first.ID); err != nil || !ok {
		t.Fatalf("expected the cancel request to be recorded, got %v (err=%v)", ok, err)
	}

	// Restarting the process requeues whatever was running, and keeps the
	// cancel requests.
	if err := s.RequeueOrphanedExecutions(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := s.CountPendingExecutions(ctx); err != nil || n != 2 {
		t.Fatalf("expected both executions requeued, got %d (err=%v)", n, err)
	}
	requeued, err := s.GetExecution(ctx, projectID, first.ID)
	if err != nil || requeued.Status != StatusPending {
		t.Fatalf("unexpected requeued execution: %+v (err=%v)", requeued, err)
	}
	if ok, err := s.IsCancelRequested(ctx, projectID, first.ID); err != nil || !ok {
		t.Fatalf("the cancel request should survive a requeue, got %v (err=%v)", ok, err)
	}
}

func TestSchedules(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	projectID := mustUUID("11111111-1111-1111-1111-111111111111")

	if _, err := s.CreateProject(ctx, Project{ID: projectID, Name: "demo"}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := s.UpsertSchedule(ctx, Schedule{ProjectID: projectID, Workflow: "deps", CronExpr: "0 0 * * 0", NextRunAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSchedule(ctx, Schedule{ProjectID: projectID, Workflow: "release", CronExpr: "0 4 * * *", NextRunAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListSchedules(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("expected 2 schedules, got %d (err=%v)", len(all), err)
	}

	due, err := s.ListDueSchedules(ctx, now)
	if err != nil || len(due) != 1 {
		t.Fatalf("expected 1 due schedule, got %+v (err=%v)", due, err)
	}
	if due[0].Workflow != "deps" || due[0].CronExpr != "0 0 * * 0" || due[0].ProjectID != projectID {
		t.Fatalf("unexpected due schedule: %+v", due[0])
	}

	// Upserting an existing workflow moves its next run instead of adding one.
	if err := s.UpsertSchedule(ctx, Schedule{ProjectID: projectID, Workflow: "deps", CronExpr: "0 0 * * 0", NextRunAt: now.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if all, err := s.ListSchedules(ctx); err != nil || len(all) != 2 {
		t.Fatalf("expected the upsert to replace the schedule, got %d (err=%v)", len(all), err)
	}
	if due, err := s.ListDueSchedules(ctx, now); err != nil || len(due) != 0 {
		t.Fatalf("expected no due schedule after the move, got %+v (err=%v)", due, err)
	}

	if err := s.DeleteSchedule(ctx, projectID, "deps"); err != nil {
		t.Fatal(err)
	}
	if all, err := s.ListSchedules(ctx); err != nil || len(all) != 1 || all[0].Workflow != "release" {
		t.Fatalf("expected only the release schedule left, got %+v (err=%v)", all, err)
	}
}
