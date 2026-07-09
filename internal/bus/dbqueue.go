package bus

import (
	"context"
	"sync"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// DBQueue 用 SQLite message 表实现 M0 持久化队列。
type DBQueue struct {
	Repo      store.Repository
	Direction model.Direction
	mu        sync.Mutex
	sensitive map[string]string
}

func NewDBQueue(repo store.Repository, direction model.Direction) *DBQueue {
	return &DBQueue{Repo: repo, Direction: direction, sensitive: map[string]string{}}
}

func (q *DBQueue) Enqueue(ctx context.Context, m model.Message) error {
	m.Direction = q.Direction
	if m.Status == "" {
		m.Status = "queued"
	}
	q.rememberSensitive(m)
	return q.Repo.EnqueueMessage(ctx, m)
}

func (q *DBQueue) Dequeue(ctx context.Context) (*model.Message, error) {
	msg, err := q.Repo.ClaimNextMessage(ctx, q.Direction)
	if err != nil || msg == nil {
		return msg, err
	}
	q.applySensitive(msg)
	return msg, nil
}

func (q *DBQueue) Ack(ctx context.Context, id string) error {
	if err := q.Repo.AckMessage(ctx, id); err != nil {
		return err
	}
	q.forgetSensitive(id)
	return nil
}

func (q *DBQueue) Fail(ctx context.Context, id string, reason string) error {
	return q.Repo.FailMessage(ctx, id, reason)
}

func (q *DBQueue) rememberSensitive(m model.Message) {
	if m.ID == "" || m.SensitiveContent == "" {
		return
	}
	q.RememberSensitiveContent(m.ID, m.SensitiveContent)
}

func (q *DBQueue) RememberSensitiveContent(id, content string) {
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

func (q *DBQueue) applySensitive(m *model.Message) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.sensitive == nil {
		return
	}
	if content := q.sensitive[m.ID]; content != "" {
		m.Content = content
	}
}

func (q *DBQueue) forgetSensitive(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.sensitive, id)
}

var _ Queue = (*DBQueue)(nil)
