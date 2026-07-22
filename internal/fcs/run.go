package fcs

// run.go — FCS assembly point (design §2). Wires config + store into a ready FCS:
// bot channel provider → underlying Feishu gateway/receiver (BuildGateway) →
// Service (Send single exit + fanout) → WebhookVerifier (inbound webhook trust).
//
// Both integration paths use this single assembly:
//   - phase-1 in-proc extraction: the hub's main.go builds Assembly here, then
//     wires the hub to NewHubGateway(svc)/NewHubReceiver(svc, hubID) so hub code
//     talks to FCS through the seam instead of the in-proc SDK directly;
//   - phase-2 separate process: cmd/fcs builds Assembly, serves the webhook HTTP
//     endpoint + WS via svc.Run, and exposes Send/Resolve/Subscribe over RPC.
//
// The hub↔FCS RPC transport (gRPC vs mTLS HTTP) is chosen with dev55 (B); this
// assembly is transport-agnostic and unchanged by that choice.

import (
	"context"
	"log/slog"

	"github.com/zhangzongchu2019/dingwei/internal/config"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// Assembly is a fully-wired FCS ready to run.
//
// NOTE (claudek3 CR ㈡): there is no Verifier field here. In phase-1 in-proc the
// HUB still serves the webhook HTTP endpoint and builds its own WebhookVerifier
// over the already-resolved bot's keys (cmd/workpulse handleFeishuWebhook). A
// store-backed Assembly.Verifier is re-introduced in phase-2, when FCS itself
// serves the webhook endpoint and resolves keys via the provider.
type Assembly struct {
	// Service is the FCS boundary (Send single exit, Resolve, Subscribe/fanout).
	Service *service
	// Provider owns bot channel config + credentials (FCS-internal).
	Provider BotChannelProvider
}

// New assembles the FCS from a store + bootstrap config (design §2/§4). The
// underlying Feishu gateway/receiver is built via BuildGateway using the store's
// bot channels; credentials are resolved FCS-internally and never leave FCS (§8).
func New(ctx context.Context, logger *slog.Logger, repo store.Repository, bs config.Bootstrap) (*Assembly, error) {
	if logger == nil {
		logger = slog.Default()
	}
	provider := NewStoreProvider(repo, bs)
	// N1 (claudek3 CR): seed the env-configured bootstrap bot channel BEFORE
	// BuildGateway lists it — else a fresh deploy's Feishu WS silently dies.
	if err := SeedBootstrap(ctx, logger, repo, bs); err != nil {
		return nil, err
	}
	gw, rx, err := BuildGateway(ctx, logger, provider, bs.FeishuTransport)
	if err != nil {
		return nil, err
	}
	return &Assembly{
		Service:  NewService(gw, rx),
		Provider: provider,
	}, nil
}

// Run drives the FCS inbound: it starts the underlying receiver and fans each
// Feishu event out to subscribed hubs (blocks until ctx cancelled). Callers add
// hubs via a.Service.Subscribe (in-proc) or the RPC subscribe stream (phase 2).
func (a *Assembly) Run(ctx context.Context) error { return a.Service.Run(ctx) }
