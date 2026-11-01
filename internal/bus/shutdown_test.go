package bus

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type shutdownBatchRepo struct {
	store.Repository
	count atomic.Int64
	fail  error
}

// The writer starts only after SealAndFlush has sealed admission. Every item
// is demonstrably still buffered at shutdown, rather than already committed by
// the runtime writer while the test was enqueuing.
func bufferedShutdownQueue(t *testing.T, repo BatchMessageRepository, total, size int) *AsyncDBQueue {
	t.Helper()
	q := &AsyncDBQueue{repo: repo, direction: model.DirectionIn,
		ch: make(chan model.Message, total), batchSize: size, flushEvery: time.Hour,
		flushReq: make(chan chan struct{}), stop: make(chan context.Context, 1), done: make(chan struct{})}
	for i := 0; i < total; i++ {
		if err := q.Enqueue(context.Background(), model.Message{ID: fmt.Sprintf("s008-%02d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	return q
}

func startSealedWriter(t *testing.T, q *AsyncDBQueue, ctx context.Context) <-chan error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- q.SealAndFlush(ctx) }()
	deadline := time.Now().Add(time.Second)
	for {
		q.admission.RLock()
		sealed := q.sealed
		q.admission.RUnlock()
		if sealed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("S008 queue admission did not seal")
		}
		time.Sleep(time.Millisecond)
	}
	if got := len(q.ch); got != cap(q.ch) {
		t.Fatalf("S008 fixture not buffered: %d/%d", got, cap(q.ch))
	}
	if err := q.Enqueue(context.Background(), model.Message{ID: "late"}); err == nil {
		t.Fatal("S008 sealed enqueue silently accepted")
	}
	go q.run(context.Background())
	t.Cleanup(func() {
		select {
		case <-q.done:
		case <-time.After(time.Second):
			t.Error("S008 writer did not finish")
		}
	})
	return result
}

type multiBatchShutdownRepo struct {
	store.Repository
	sizes     []int
	committed map[string]bool
	failures  map[int]error
	blockAt   int
}

func (r *multiBatchShutdownRepo) BatchEnqueueMessages(ctx context.Context, ms []model.Message) error {
	r.sizes = append(r.sizes, len(ms))
	call := len(r.sizes)
	if call == r.blockAt {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := r.failures[call]; err != nil {
		return err
	}
	if r.committed == nil {
		r.committed = map[string]bool{}
	}
	for _, m := range ms {
		if r.committed[m.ID] {
			return errors.New("S008 duplicate commit")
		}
		r.committed[m.ID] = true
	}
	return nil
}

func TestShutdownQueueS008BufferedMultipleBatches(t *testing.T) {
	r := &multiBatchShutdownRepo{}
	q := bufferedShutdownQueue(t, r, 23, 4)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := <-startSealedWriter(t, q, ctx); err != nil {
		remaining, failed := q.ShutdownCounts()
		t.Fatalf("S008_ALL_BUFFERED_COMMITTED: error=%v committed=%d remaining=%d failed=%d", err, len(r.committed), remaining, failed)
	}
	remaining, failed := q.ShutdownCounts()
	if len(r.committed) != 23 || remaining != 0 || failed != 0 || !reflect.DeepEqual(r.sizes, []int{4, 4, 4, 4, 4, 3}) {
		t.Fatalf("S008_ALL_BUFFERED_COMMITTED: committed=%d remaining=%d failed=%d batches=%v", len(r.committed), remaining, failed, r.sizes)
	}
	t.Logf("S008 buffered_before=23 batches=%v committed=%d remaining=%d failed=%d late_enqueue=rejected", r.sizes, len(r.committed), remaining, failed)
}

func TestShutdownQueueS008FirstFailureAndUncommittedCount(t *testing.T) {
	first, later := errors.New("synthetic first failure"), errors.New("synthetic later failure")
	r := &multiBatchShutdownRepo{failures: map[int]error{2: first, 4: later}}
	q := bufferedShutdownQueue(t, r, 23, 4)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := <-startSealedWriter(t, q, ctx)
	remaining, failed := q.ShutdownCounts()
	if !errors.Is(err, first) || errors.Is(err, later) || len(r.committed) != 15 || remaining != 8 || failed != 8 || len(q.ch) != 0 {
		t.Fatalf("S008_FIRST_ERROR_AND_REMAINDER: error=%v committed=%d remaining=%d failed=%d buffered=%d", err, len(r.committed), remaining, failed, len(q.ch))
	}
	t.Logf("S008 first_error_preserved=true batches=%v committed=%d remaining=%d failed=%d", r.sizes, len(r.committed), remaining, failed)
}

func TestShutdownQueueS008DeadlinePreservesUnattemptedItems(t *testing.T) {
	r := &multiBatchShutdownRepo{blockAt: 2}
	q := bufferedShutdownQueue(t, r, 23, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := <-startSealedWriter(t, q, ctx)
	<-q.done
	remaining, failed := q.ShutdownCounts()
	if !errors.Is(err, context.DeadlineExceeded) || len(r.committed) != 4 || remaining != 19 || failed != 4 || len(q.ch) != 15 {
		t.Fatalf("S008_DEADLINE_REMAINDER: error=%v committed=%d remaining=%d failed=%d buffered=%d", err, len(r.committed), remaining, failed, len(q.ch))
	}
	t.Logf("S008 timeout=true committed=%d remaining=%d failed=%d unattempted=%d", len(r.committed), remaining, failed, len(q.ch))
}

func TestShutdownQueueS008AdmissionWaitIsBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := &AsyncDBQueue{ch: make(chan model.Message, 1)}
	if err := q.Enqueue(ctx, model.Message{}); err != nil {
		t.Fatal(err)
	}
	blocked := make(chan error, 1)
	go func() { blocked <- q.Enqueue(ctx, model.Message{}) }()
	ready := time.Now().Add(time.Second)
	for q.pending.Load() != 2 {
		if time.Now().After(ready) {
			t.Fatal("S008 producer did not reach blocked admission")
		}
		time.Sleep(time.Millisecond)
	}
	stop, done := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer done()
	start := time.Now()
	err := q.SealAndFlush(stop)
	cancel()
	<-blocked
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("S008_ADMISSION_BOUND: error=%v elapsed=%v", err, time.Since(start))
	}
}

func (r *shutdownBatchRepo) BatchEnqueueMessages(ctx context.Context, ms []model.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.fail != nil {
		return r.fail
	}
	r.count.Add(int64(len(ms)))
	return nil
}
func TestShutdownQueueDrainsChannelAndPartialBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &shutdownBatchRepo{}
	q := NewAsyncDBQueue(ctx, r, model.DirectionIn, AsyncDBQueueConfig{BatchSize: 200, FlushInterval: time.Hour})
	for i := 0; i < 100; i++ {
		if err := q.Enqueue(ctx, model.Message{}); err != nil {
			t.Fatal(err)
		}
	}
	stop, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	if err := q.SealAndFlush(stop); err != nil {
		t.Fatal(err)
	}
	remaining, failed := q.ShutdownCounts()
	if r.count.Load() != 100 || remaining != 0 || failed != 0 {
		t.Fatalf("not durable: committed=%d remaining=%d failed=%d", r.count.Load(), remaining, failed)
	}
	if err := q.Enqueue(ctx, model.Message{}); err == nil {
		t.Fatal("enqueue admitted after seal")
	}
}
func TestShutdownQueueFailureIsNotSuccessfulFlush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &shutdownBatchRepo{fail: errors.New("synthetic commit failure")}
	q := NewAsyncDBQueue(ctx, r, model.DirectionIn, AsyncDBQueueConfig{BatchSize: 200, FlushInterval: time.Hour})
	if err := q.Enqueue(ctx, model.Message{}); err != nil {
		t.Fatal(err)
	} // unchanged runtime admission
	stop, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	if err := q.SealAndFlush(stop); err == nil {
		t.Fatal("shutdown swallowed commit failure")
	}
	remaining, failed := q.ShutdownCounts()
	if remaining != 1 || failed != 1 {
		t.Fatalf("remaining=%d failed=%d", remaining, failed)
	}
}

func TestShutdownQueueRuntimeAcknowledgementRemainsBestEffort(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := &shutdownBatchRepo{fail: errors.New("synthetic runtime commit failure")}
	q := NewAsyncDBQueue(ctx, r, model.DirectionOut, AsyncDBQueueConfig{BatchSize: 1})
	if err := q.Enqueue(ctx, model.Message{}); err != nil {
		t.Fatalf("runtime admission changed: %v", err)
	}
	for q.failed.Load() == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("runtime fixture did not reach failed commit")
		case <-time.After(time.Millisecond):
		}
	}
	if err := q.Flush(ctx); err != nil {
		t.Fatalf("runtime best-effort Flush changed: %v", err)
	}
	if err := q.SealAndFlush(ctx); !errors.Is(err, r.fail) {
		t.Fatalf("shutdown must expose the first failed commit: %v", err)
	}
	t.Log("runtime Enqueue=nil Flush=nil; shutdown reports first persistence failure")
}
