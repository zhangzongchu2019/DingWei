package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestShutdownRecordsBothServersAndExitClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		err  error
	}{{"graceful", 0, nil}, {"failure", 1, errors.New("synthetic commit failure")}, {"timeout", 2, context.DeadlineExceeded}} {
		t.Run(tc.name, func(t *testing.T) {
			r := coordinateShutdown(time.Now(), "fixture", shutdownPlan{Flush: func(context.Context) error { return tc.err }})
			if r.Code != tc.code || len(r.Report.Stages) != 5 {
				t.Fatalf("code=%d stages=%+v", r.Code, r.Report.Stages)
			}
		})
	}
}
func TestShutdownDoesNotCloseDBWithLiveUsers(t *testing.T) {
	var closed atomic.Bool
	r := coordinateShutdown(time.Now(), "fixture", shutdownPlan{Drain: func(context.Context) error { return context.DeadlineExceeded }, Storage: func(context.Context) error { closed.Store(true); return nil }})
	if r.Code != 2 || closed.Load() {
		t.Fatalf("code=%d closed=%t", r.Code, closed.Load())
	}
}
func TestShutdownBlockingFinalizerIsBounded(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	start := time.Now()
	r := coordinateShutdown(start, "fixture", shutdownPlan{Finalize: func(context.Context) error { <-release; return nil }})
	if r.Code != 2 || time.Since(start) > 3*time.Second {
		t.Fatalf("code=%d elapsed=%v", r.Code, time.Since(start))
	}
}
func TestShutdownTimeoutHasPriority(t *testing.T) {
	r := coordinateShutdown(time.Now(), "fixture", shutdownPlan{Drain: func(context.Context) error { return errors.New("synthetic failure") }, Flush: func(context.Context) error { return context.DeadlineExceeded }})
	if r.Code != 2 || r.Report.Stages[1].Error != "operation_failed" {
		t.Fatalf("report=%+v", r.Report)
	}
}
