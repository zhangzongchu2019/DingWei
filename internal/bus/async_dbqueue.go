package bus

import (
	"context"
	"log/slog"
	"os"
	"strconv"
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
	m.Direction = q.direction
	if m.Status == "" {
		m.Status = "queued"
	}
	select {
	case q.ch <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *AsyncDBQueue) Dequeue(ctx context.Context) (*model.Message, error) {
	return q.repo.ClaimNextMessage(ctx, q.direction)
}

func (q *AsyncDBQueue) Ack(ctx context.Context, id string) error {
	return q.repo.AckMessage(ctx, id)
}

func (q *AsyncDBQueue) Fail(ctx context.Context, id string, reason string) error {
	return q.repo.FailMessage(ctx, id, reason)
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

func (q *AsyncDBQueue) run(ctx context.Context) {
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
		flushCtx, cancel := context.WithTimeout(context.Background(), defaultWriteFlushTimeout)
		if err := q.repo.BatchEnqueueMessages(flushCtx, items); err != nil && q.logger != nil {
			q.logger.Warn("async write batch failed; dropping batch", "direction", q.direction, "count", len(items), "error", err)
		}
		cancel()
	}
	for {
		select {
		case <-ctx.Done():
			for {
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
