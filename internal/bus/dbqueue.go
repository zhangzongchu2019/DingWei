package bus

import (
	"context"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// DBQueue 用 SQLite message 表实现 M0 持久化队列。
type DBQueue struct {
	Repo      store.Repository
	Direction model.Direction
}

func NewDBQueue(repo store.Repository, direction model.Direction) *DBQueue {
	return &DBQueue{Repo: repo, Direction: direction}
}

func (q *DBQueue) Enqueue(ctx context.Context, m model.Message) error {
	m.Direction = q.Direction
	if m.Status == "" {
		m.Status = "queued"
	}
	return q.Repo.EnqueueMessage(ctx, m)
}

func (q *DBQueue) Dequeue(ctx context.Context) (*model.Message, error) {
	return q.Repo.ClaimNextMessage(ctx, q.Direction)
}

func (q *DBQueue) Ack(ctx context.Context, id string) error {
	return q.Repo.AckMessage(ctx, id)
}

func (q *DBQueue) Fail(ctx context.Context, id string, reason string) error {
	return q.Repo.FailMessage(ctx, id, reason)
}

var _ Queue = (*DBQueue)(nil)
