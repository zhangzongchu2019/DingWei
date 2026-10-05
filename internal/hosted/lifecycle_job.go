package hosted

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// LifecycleJobRequest is persisted before the HTTP request returns.  The
// worker reuses the existing lifecycle state machine; this type only provides
// a durable queue/read model around it.
type LifecycleJobRequest struct {
	AgentID string
	Actor   string
	Op      string
	Version string
	Confirm bool
}

type lifecycleJob struct {
	ID, AgentID, CreatedBy, Op, Stage, Reason string
	Args                                      json.RawMessage
}

func (l *Lifecycle) EnqueueJob(ctx context.Context, req LifecycleJobRequest) (string, error) {
	if err := l.validateJob(ctx, req); err != nil {
		return "", err
	}
	id, err := newID("job_")
	if err != nil {
		return "", err
	}
	args, _ := json.Marshal(map[string]any{"version": req.Version, "confirm": req.Confirm})
	_, err = l.st.exec(ctx, `
INSERT INTO g4_lifecycle_job(id,agent_id,created_by,op,args) VALUES($1,$2,$3,$4,$5)`,
		id, req.AgentID, req.Actor, req.Op, args)
	return id, err
}

func (l *Lifecycle) validateJob(ctx context.Context, req LifecycleJobRequest) error {
	if req.Op == "destroy" {
		if !req.Confirm {
			return ErrConfirmRequired
		}
		seat, err := l.st.GetSeat(ctx, req.AgentID)
		if err != nil {
			return err
		}
		if req.Actor == "" || seat.CreatedBy == "" || req.Actor != seat.CreatedBy {
			return ErrNotOwner
		}
		return nil
	}
	seat, err := l.guard(ctx, req.AgentID, req.Actor, false)
	if err != nil {
		return err
	}
	switch req.Op {
	case "upgrade":
		if seat.State != "online" {
			return ErrInvalidState
		}
		if _, ok := PlanByID(seat.Plan); !ok {
			return ErrParams
		}
		if req.Version == "" {
			if _, ok := l.images.Latest(seat.CLI); !ok {
				return ErrParams
			}
		}
	case "restart":
		if seat.State != "online" && seat.State != "error" {
			return ErrInvalidState
		}
	case "disable":
		if !req.Confirm {
			return ErrConfirmRequired
		}
		if seat.State != "online" {
			return ErrInvalidState
		}
	case "enable":
		if seat.State != "disabled" {
			return ErrInvalidState
		}
	default:
		return ErrInvalidState
	}
	return nil
}

func (l *Lifecycle) RunJob(ctx context.Context, jobID string) {
	job, err := l.getJob(ctx, jobID)
	if err != nil {
		return
	}
	res, err := l.st.exec(ctx, `UPDATE g4_lifecycle_job SET stage='running',updated_at=now() WHERE id=$1 AND stage='queued'`, jobID)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return
	}
	var args struct {
		Version string `json:"version"`
		Confirm bool   `json:"confirm"`
	}
	_ = json.Unmarshal(job.Args, &args)
	switch job.Op {
	case "upgrade":
		err = l.Upgrade(ctx, job.AgentID, job.CreatedBy, args.Version, false)
	case "restart":
		err = l.Restart(ctx, job.AgentID, job.CreatedBy, false)
	case "disable":
		err = l.Disable(ctx, job.AgentID, job.CreatedBy, false, args.Confirm)
	case "enable":
		err = l.Enable(ctx, job.AgentID, job.CreatedBy, false)
	case "destroy":
		err = l.Destroy(ctx, job.AgentID, job.CreatedBy, false, args.Confirm)
	default:
		err = ErrInvalidState
	}
	stage, reason := "completed", ""
	if err != nil {
		stage, reason = "failed", lifecycleJobReason(err)
	} else if job.Op == "restart" || job.Op == "enable" {
		// These operations only become complete when the Agent reconnects.
		// Their state machine deliberately leaves the seat in starting until
		// MarkOnline (or the timeout sweeper) resolves it.
		stage = "running"
	}
	_, _ = l.st.exec(context.WithoutCancel(ctx), `UPDATE g4_lifecycle_job SET stage=$2,reason=$3,updated_at=now() WHERE id=$1`, jobID, stage, reason)
}

func lifecycleJobReason(err error) string {
	for _, item := range []struct {
		err  error
		code string
	}{
		{ErrInvalidState, "invalid_state"}, {ErrNodeOffline, "node_offline"},
		{ErrStartFailed, "start_failed"}, {ErrRolledBack, "upgrade_failed_rolled_back"},
		{ErrBackupFailed, "backup_failed"}, {ErrParams, "params_invalid"},
		{ErrNotOwner, "not_owner"}, {ErrConfirmRequired, "confirm_required"},
	} {
		if errors.Is(err, item.err) {
			return item.code
		}
	}
	return "internal_error"
}

func (l *Lifecycle) getJob(ctx context.Context, id string) (lifecycleJob, error) {
	var j lifecycleJob
	row, err := l.st.qrow(ctx, `SELECT id,agent_id,created_by,op,args,stage,reason FROM g4_lifecycle_job WHERE id=$1`, id)
	if err == nil {
		err = row.Scan(&j.ID, &j.AgentID, &j.CreatedBy, &j.Op, &j.Args, &j.Stage, &j.Reason)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return j, ErrNotFound
	}
	return j, err
}

func (l *Lifecycle) JobProgressFor(ctx context.Context, id, actor string) (JobView, error) {
	j, err := l.getJob(ctx, id)
	if err != nil {
		return JobView{}, err
	}
	if actor == "" || actor != j.CreatedBy {
		return JobView{}, ErrNotOwner
	}
	if j.Stage == "running" && (j.Op == "restart" || j.Op == "enable") {
		if seat, seatErr := l.st.GetSeat(ctx, j.AgentID); seatErr == nil {
			switch seat.State {
			case "online":
				j.Stage, j.Reason = "completed", ""
			case "error":
				j.Stage, j.Reason = "failed", lifecycleSeatFailureReason(seat.StateDetail)
			}
			if j.Stage != "running" {
				_, _ = l.st.exec(ctx, `UPDATE g4_lifecycle_job SET stage=$2,reason=$3,updated_at=now() WHERE id=$1 AND stage='running'`, j.ID, j.Stage, j.Reason)
			}
		}
	}
	v := JobView{Stage: j.Stage, Reason: j.Reason}
	if j.Stage == "completed" {
		v.AgentID = j.AgentID
	}
	return v, nil
}

func lifecycleSeatFailureReason(detail string) string {
	switch detail {
	case "start_timeout", "start_failed", "node_offline":
		return detail
	default:
		return "start_failed"
	}
}
