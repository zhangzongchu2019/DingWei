package bus

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

const (
	defaultWriteQueueSize    = 50000
	defaultWriteBatchSize    = 200
	defaultWriteFlushMillis  = 20
	defaultWriteFlushTimeout = 5 * time.Second
)

type BatchMessageRepository interface {
	store.Repository
	BatchEnqueueMessages(ctx context.Context, messages []model.Message) error
}

type AsyncDBQueueConfig struct {
	QueueSize     int
	BatchSize     int
	FlushInterval time.Duration
	Logger        *slog.Logger
}

func AsyncDBQueueConfigFromEnv(logger *slog.Logger) AsyncDBQueueConfig {
	return AsyncDBQueueConfig{
		QueueSize:     envInt("WP_WRITE_QUEUE_SIZE", defaultWriteQueueSize),
		BatchSize:     envInt("WP_WRITE_BATCH_SIZE", defaultWriteBatchSize),
		FlushInterval: time.Duration(envInt("WP_WRITE_FLUSH_MS", defaultWriteFlushMillis)) * time.Millisecond,
		Logger:        logger,
	}
}

type AsyncDBQueue struct {
	repo       BatchMessageRepository
	direction  model.Direction
	ch         chan model.Message
	batchSize  int
	flushEvery time.Duration
	logger     *slog.Logger
	flushReq   chan chan struct{}
	mu         sync.Mutex
	sensitive  map[string]string
	admission  sync.RWMutex
	sealed     bool
	pending    atomic.Int64
	failed     atomic.Int64
	failureMu  sync.Mutex
	failure    error
	stop       chan context.Context
	done       chan struct{}
}

func NewAsyncDBQueue(ctx context.Context, repo BatchMessageRepository, direction model.Direction, cfg AsyncDBQueueConfig) *AsyncDBQueue {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaultWriteQueueSize
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultWriteBatchSize
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = defaultWriteFlushMillis * time.Millisecond
	}
	q := &AsyncDBQueue{
		repo:       repo,
		direction:  direction,
		ch:         make(chan model.Message, cfg.QueueSize),
		batchSize:  cfg.BatchSize,
		flushEvery: cfg.FlushInterval,
		logger:     cfg.Logger,
		flushReq:   make(chan chan struct{}),
		sensitive:  map[string]string{},
		stop:       make(chan context.Context, 1), done: make(chan struct{}),
	}
	go q.run(ctx)
	return q
}

func NewBestEffortDBQueue(ctx context.Context, repo store.Repository, direction model.Direction, cfg AsyncDBQueueConfig) Queue {
	if batchRepo, ok := repo.(BatchMessageRepository); ok {
		return NewAsyncDBQueue(ctx, batchRepo, direction, cfg)
	}
	return NewDBQueue(repo, direction)
}

func (q *AsyncDBQueue) Enqueue(ctx context.Context, m model.Message) error {
	q.admission.RLock()
	defer q.admission.RUnlock()
	if q.sealed {
		return errors.New("queue sealed for shutdown")
	}
	m.Direction = q.direction
	if m.Status == "" {
		m.Status = "queued"
	}
	q.rememberSensitive(m)
	q.pending.Add(1)
	select {
	case q.ch <- m:
		return nil
	case <-ctx.Done():
		q.pending.Add(-1)
		return ctx.Err()
	}
}

func (q *AsyncDBQueue) Dequeue(ctx context.Context) (*model.Message, error) {
	msg, err := q.repo.ClaimNextMessage(ctx, q.direction)
	if err != nil || msg == nil {
		return msg, err
	}
	q.applySensitive(msg)
	return msg, nil
}

func (q *AsyncDBQueue) Ack(ctx context.Context, id string) error {
	if err := q.repo.AckMessage(ctx, id); err != nil {
		return err
	}
	q.forgetSensitive(id)
	return nil
}

func (q *AsyncDBQueue) Fail(ctx context.Context, id string, reason string) error {
	return q.repo.FailMessage(ctx, id, reason)
}

func (q *AsyncDBQueue) rememberSensitive(m model.Message) {
	if m.ID == "" || m.SensitiveContent == "" {
		return
	}
	q.RememberSensitiveContent(m.ID, m.SensitiveContent)
}

func (q *AsyncDBQueue) RememberSensitiveContent(id, content string) {
	if id == "" || content == "" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.sensitive == nil {
		q.sensitive = map[string]string{}
	}
	q.sensitive[id] = content
}

func (q *AsyncDBQueue) applySensitive(m *model.Message) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.sensitive == nil {
		return
	}
	if content := q.sensitive[m.ID]; content != "" {
		m.Content = content
	}
}

func (q *AsyncDBQueue) forgetSensitive(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.sensitive, id)
}

func (q *AsyncDBQueue) Flush(ctx context.Context) error {
	done := make(chan struct{})
	select {
	case q.flushReq <- done:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SealAndFlush is shutdown-only: callers first join all producers/consumers.
// Runtime Enqueue and Flush keep their best-effort acknowledgement semantics.
// The terminal writer drains both the channel and partial batch and reports
// failures (including a batch already in flight when shutdown began).
func (q *AsyncDBQueue) SealAndFlush(ctx context.Context) error {
	// An abnormal producer may still hold admission while blocked on a full
	// channel. Bound even that wait by the database phase deadline.
	for !q.admission.TryLock() {
		select {
		case <-ctx.Done():
			return errors.Join(q.ShutdownError(), ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if !q.sealed {
		q.sealed = true
		q.stop <- ctx
	}
	q.admission.Unlock()
	select {
	case <-q.done:
		// A deadline and writer completion may become ready together. Do not
		// turn that race into a successful shutdown.
		err := errors.Join(q.ShutdownError(), ctx.Err())
		if err == nil && q.pending.Load() != 0 {
			err = errors.New("queue shutdown incomplete")
		}
		return err
	case <-ctx.Done():
		return errors.Join(q.ShutdownError(), ctx.Err())
	}
}
func (q *AsyncDBQueue) ShutdownCounts() (int64, int64) { return q.pending.Load(), q.failed.Load() }
func (q *AsyncDBQueue) ShutdownError() error {
	q.failureMu.Lock()
	defer q.failureMu.Unlock()
	return q.failure
}

// Keep the first database cause available to errors.Is without exposing raw
// database text (which may contain message content) in lifecycle logs.
type batchPersistenceError struct{ cause error }

func (e *batchPersistenceError) Error() string { return "async batch persistence failed" }
func (e *batchPersistenceError) Unwrap() error { return e.cause }

func (q *AsyncDBQueue) run(ctx context.Context) {
	defer close(q.done)
	flushParent := context.Background()

	batch := make([]model.Message, 0, q.batchSize)
	var timer *time.Timer
	var timerC <-chan time.Time
	stopTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer = nil
			timerC = nil
		}
	}
	defer stopTimer()
	startTimer := func() {
		if timer == nil {
			timer = time.NewTimer(q.flushEvery)
			timerC = timer.C
		}
	}
	flush := func() {
		stopTimer()
		if len(batch) == 0 {
			return
		}
		items := append([]model.Message(nil), batch...)
		batch = batch[:0]
		flushCtx, cancel := context.WithTimeout(flushParent, defaultWriteFlushTimeout)
		if err := q.repo.BatchEnqueueMessages(flushCtx, items); err != nil {
			q.failed.Add(int64(len(items)))
			q.failureMu.Lock()
			if q.failure == nil {
				q.failure = &batchPersistenceError{cause: err}
			}
			q.failureMu.Unlock()
			// Keep raw database errors out of lifecycle logs (they may contain payload).
			if q.logger != nil {
				q.logger.Warn("async write batch failed; dropping batch", "direction", q.direction, "count", len(items), "error", err)
			}
		} else {
			q.pending.Add(-int64(len(items)))
		}
		cancel()
	}
	drain := func() {
		for {
			// Leave the unattempted items accounted for at the DB deadline.
			if flushParent.Err() != nil {
				return
			}
			select {
			case m := <-q.ch:
				batch = append(batch, m)
				if len(batch) >= q.batchSize {
					flush()
				}
			default:
				flush()
				return
			}
		}
	}
	for {
		// Prefer an already requested shutdown to starting another runtime
		// batch with the longer best-effort database timeout.
		select {
		case stopCtx := <-q.stop:
			flushParent = stopCtx
			drain()
			return
		default:
		}
		select {
		case stopCtx := <-q.stop:
			flushParent = stopCtx
			drain()
			return
		case <-ctx.Done():
			drain()
			return

		case reply := <-q.flushReq:
			flush()
			close(reply)
		case m := <-q.ch:
			batch = append(batch, m)
			if len(batch) >= q.batchSize {
				flush()
			} else {
				startTimer()
			}
		case <-timerC:
			flush()
		}
	}
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

var _ Queue = (*AsyncDBQueue)(nil)
