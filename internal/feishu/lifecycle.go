package feishu

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/zhangzongchu2019/dingwei/internal/lifecycle"
)

var ErrReceiverDraining = errors.New("feishu receiver draining")

// ReceiverLifecycle is optional: remote FCS receivers retain their existing
// subscription lifecycle. Only an in-process SDK receiver implements it.
type ReceiverLifecycle interface {
	StopIngress(context.Context) error
	IngressSnapshot() IngressShutdownSnapshot
}

type IngressShutdownSnapshot struct {
	Active            int    `json:"active_callbacks"`
	Rejected          uint64 `json:"rejected_after_seal"`
	Sealed            bool   `json:"sealed"`
	CloseCompleted    bool   `json:"close_completed"`
	SDKStartNotJoined int    `json:"sdk_start_not_joined"`
}

type sdkConnection interface {
	Start(context.Context) error
	Close()
}

type receiverLifecycle struct {
	mu        sync.Mutex
	started   bool
	stopping  bool
	sealed    bool
	closed    bool
	client    sdkConnection
	ready     chan struct{}
	returned  chan struct{}
	closeDone chan struct{}
	closeOnce sync.Once
	closeErr  error
	callbacks lifecycle.Group
	rejected  atomic.Uint64
}

func (g *LarkGateway) startReceiver(ctx context.Context, disp *dispatcher.EventDispatcher) error {
	r := &g.receiver
	r.mu.Lock()
	if r.stopping || r.started {
		r.mu.Unlock()
		return ErrReceiverDraining
	}
	r.started = true
	r.ready, r.returned = make(chan struct{}), make(chan struct{})
	var readyOnce sync.Once
	ready := func() { readyOnce.Do(func() { close(r.ready) }) }
	factory := g.wsFactory
	if factory == nil {
		factory = g.newWSClient
	}
	r.client = factory(disp, ready)
	client := r.client
	r.mu.Unlock()
	// v3.9.7 enters select{} after a successful connection. Do not wrap this
	// return in an owned-work counter or claim that Close joins this frame.
	result := make(chan error, 1)
	go func() {
		defer close(r.returned)
		result <- client.Start(ctx)
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *LarkGateway) StopIngress(ctx context.Context) error {
	r := &g.receiver
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.stopping = true // prevents a receiver starting after shutdown
		r.closeDone = make(chan struct{})
		client, ready, returned := r.client, r.ready, r.returned
		r.mu.Unlock()
		go func() {
			var err error
			if client != nil {
				// Do not Close before Start has connected (or returned). A late
				// startup must not establish a new connection after Close.
				select {
				case <-ready:
				case <-returned:
				case <-ctx.Done():
					err = ctx.Err()
				}
				client.Close()
			}
			// The same gate coordinates callback admission and sealing. Close
			// may itself dispatch a final callback; that callback is joined.
			r.callbacks.Seal()
			r.mu.Lock()
			r.sealed, r.closed, r.closeErr = true, true, err
			r.mu.Unlock()
			close(r.closeDone)
		}()
	})
	select {
	case <-r.closeDone:
		r.mu.Lock()
		err := r.closeErr
		r.mu.Unlock()
		return errors.Join(err, r.callbacks.Wait(ctx), ctx.Err())
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *LarkGateway) IngressSnapshot() IngressShutdownSnapshot {
	r := &g.receiver
	r.mu.Lock()
	defer r.mu.Unlock()
	s := IngressShutdownSnapshot{Active: r.callbacks.Snapshot()["inbound"],
		Rejected: r.rejected.Load(), Sealed: r.sealed, CloseCompleted: r.closed}
	if r.started {
		select {
		case <-r.returned:
		default:
			s.SDKStartNotJoined = 1
		}
	}
	return s
}

func (m *MultiGateway) StopIngress(ctx context.Context) error {
	m.mu.RLock()
	gateways := make([]*LarkGateway, 0, len(m.gateways))
	for _, g := range m.gateways {
		gateways = append(gateways, g)
	}
	m.mu.RUnlock()
	results := make(chan error, len(gateways))
	for _, g := range gateways {
		go func(g *LarkGateway) { results <- g.StopIngress(ctx) }(g)
	}
	var errs []error
	for range gateways {
		select {
		case err := <-results:
			errs = append(errs, err)
		case <-ctx.Done():
			return errors.Join(append(errs, ctx.Err())...)
		}
	}
	return errors.Join(errs...)
}

func (m *MultiGateway) IngressSnapshot() IngressShutdownSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := IngressShutdownSnapshot{Sealed: true, CloseCompleted: true}
	for _, g := range m.gateways {
		s := g.IngressSnapshot()
		result.Active += s.Active
		result.Rejected += s.Rejected
		result.SDKStartNotJoined += s.SDKStartNotJoined
		result.Sealed = result.Sealed && s.Sealed
		result.CloseCompleted = result.CloseCompleted && s.CloseCompleted
	}
	return result
}
