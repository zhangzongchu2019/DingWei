package m8

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type keepaliveACKBarrier struct {
	store.Repository
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (r *keepaliveACKBarrier) ApplySessionDeliveryACK(ctx context.Context, a model.SessionDeliveryACK, now time.Time) (*model.SessionDelivery, bool, error) {
	if r.armed.CompareAndSwap(true, false) {
		close(r.entered)
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	return r.Repository.ApplySessionDeliveryACK(ctx, a, now)
}
func TestSessionIngressACKWaitsForCommitDuringBusinessGate(t *testing.T) {
	var gate *keepaliveACKBarrier
	f := newACKV2FixtureWithKindHook(t, true, "", false, func(e *agentEnv) {
		gate = &keepaliveACKBarrier{Repository: e.hub.Repo, entered: make(chan struct{}), release: make(chan struct{})}
		e.hub.Repo = gate
		useTestSessionKeepalive(t, e.hub, 30*time.Millisecond, 500*time.Millisecond)
	})
	var released bool
	defer func() {
		if !released {
			close(gate.release)
		}
	}()
	gate.armed.Store(true)
	confirmed := make(chan model.Envelope, 1)
	readerCtx, stop := context.WithCancel(f.ctx)
	defer stop()
	go func() {
		for {
			_, raw, err := f.conn.Read(readerCtx)
			if err != nil {
				return
			}
			var e model.Envelope
			if json.Unmarshal(raw, &e) == nil && metaString(e.Meta, "type") == "delivery_ack_confirm" {
				confirmed <- e
				return
			}
		}
	}()
	f.sendEnv(f.conn, f.ack("queued", "", "", f.stamp))
	select {
	case <-gate.entered:
	case <-time.After(time.Second):
		t.Fatal("ACK did not reach transaction gate")
	}
	select {
	case <-confirmed:
		t.Fatal("ACK was confirmed before DB commit")
	case <-time.After(150 * time.Millisecond):
	}
	if f.record().DeliveredAt != nil {
		t.Fatal("queued receipt invented durable stage while transaction blocked")
	}
	close(gate.release)
	released = true
	select {
	case <-confirmed:
	case <-time.After(time.Second):
		t.Fatal("committed ACK not confirmed")
	}
	if f.record().DeliveredAt == nil {
		t.Fatal("wire confirmation preceded durable receipt")
	}
}
