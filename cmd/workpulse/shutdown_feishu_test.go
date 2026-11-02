package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/lifecycle"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type shutdownReceiverFixture struct{ stop func(context.Context) error }

func (*shutdownReceiverFixture) Start(context.Context, func(context.Context, model.Message) error) error {
	return nil
}
func (r *shutdownReceiverFixture) StopIngress(ctx context.Context) error { return r.stop(ctx) }
func (*shutdownReceiverFixture) IngressSnapshot() feishu.IngressShutdownSnapshot {
	return feishu.IngressShutdownSnapshot{}
}

func TestShutdownFeishuCloseBeforeRootSealAndWait(t *testing.T) {
	var callbacks lifecycle.Group
	callbackDone, ok := callbacks.Enter("receiver_callback")
	if !ok {
		t.Fatal("fixture callback not admitted")
	}
	closed := make(chan struct{})
	receiver := &shutdownReceiverFixture{stop: func(context.Context) error {
		if done, ok := callbacks.Enter("close_callback"); ok {
			done()
		} else {
			t.Error("root callback gate sealed before SDK Close")
		}
		close(closed)
		return nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- stopFeishuIngress(ctx, receiver, &callbacks) }()
	<-closed
	select {
	case err := <-result:
		t.Fatalf("returned before root callback completed: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	callbackDone()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if _, ok := callbacks.Enter("late"); ok {
		t.Fatal("late root callback admitted")
	}
}

func TestShutdownFeishuUsesEightSecondDrainAndExitTwo(t *testing.T) {
	var callbacks lifecycle.Group
	var dbClosed atomic.Bool
	receiver := &shutdownReceiverFixture{stop: func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 8*time.Second {
			t.Error("receiver has no eight-second drain deadline")
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	result := coordinateShutdown(time.Now(), "SDK callback timeout fixture", shutdownPlan{
		Drain:   func(ctx context.Context) error { return stopFeishuIngress(ctx, receiver, &callbacks) },
		Storage: func(context.Context) error { dbClosed.Store(true); return nil },
	})
	stage := result.Report.Stages[1]
	if result.Code != 2 || !stage.Timeout || stage.BudgetMS != 8000 || dbClosed.Load() || stage.ElapsedMS < 7900 {
		t.Fatalf("SDK_DRAIN_TIMEOUT: code=%d db_closed=%t stage=%+v", result.Code, dbClosed.Load(), stage)
	}
	t.Logf("SDK drain_budget_ms=%d elapsed_ms=%d exit_code=%d db_closed=%t", stage.BudgetMS, stage.ElapsedMS, result.Code, dbClosed.Load())
}

func TestShutdownFeishuRemoteReceiverUsesExistingRootGate(t *testing.T) {
	var callbacks lifecycle.Group
	done, _ := callbacks.Enter("remote_callback")
	callbacks.Seal() // existing remote path seals at stop ingress
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	err := stopFeishuIngress(ctx, nil, &callbacks)
	done()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("remote in-flight callback was not joined: %v", err)
	}
}

func TestShutdownFeishuAdmittedCallbackKeepsWorkBudget(t *testing.T) {
	type callbackKey struct{}
	inbound, cancelInbound := context.WithCancel(context.WithValue(context.Background(), callbackKey{}, "synthetic"))
	work, cancelWork := context.WithCancel(context.Background())
	ctx, cancel := callbackWorkContext(inbound, work)
	defer cancel()
	cancelInbound()
	if ctx.Err() != nil || ctx.Value(callbackKey{}) != "synthetic" {
		t.Fatal("admitted callback lost work budget or context values at receiver cancel")
	}
	cancelWork()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("admitted callback ignored forced work deadline")
	}
}
