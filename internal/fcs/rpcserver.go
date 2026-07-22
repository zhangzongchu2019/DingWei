package fcs

// rpcserver.go — FCS server side of the phase-2 hub↔FCS HTTP RPC (走甲). Wraps any
// Service (the in-proc *service in cmd/fcs) as an http.Handler so remote hubs can
// call Send/Resolve and subscribe to the inbound fanout stream. The Service seam is
// unchanged — this is purely a transport adapter, mirror-image of NewRPCClient.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

// NewRPCServer mounts the hub-facing FCS RPC over svc. Mount it under the FCS
// process's public listener (cmd/fcs). logger may be nil.
func NewRPCServer(svc Service, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	s := &rpcServer{svc: svc, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+RPCPathSend, s.handleSend)
	mux.HandleFunc("POST "+RPCPathResolve, s.handleResolve)
	mux.HandleFunc("GET "+RPCPathInbound, s.handleInbound)
	mux.HandleFunc("POST "+RPCPathReceipt, s.handleReceipt)
	return mux
}

type rpcServer struct {
	svc    Service
	logger *slog.Logger
}

func (s *rpcServer) handleSend(w http.ResponseWriter, r *http.Request) {
	var req OutboundRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	res, err := s.svc.Send(r.Context(), req)
	resp := wireSendResp{FeishuMsgID: res.FeishuMsgID}
	if err != nil {
		resp.Err = err.Error() // app-level error carried in body (HTTP 200)
	}
	writeJSON(w, resp)
}

func (s *rpcServer) handleResolve(w http.ResponseWriter, r *http.Request) {
	var req wireResolveReq
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var value string
	var err error
	switch req.Kind {
	case resolveChat:
		value, err = s.svc.ResolveChatName(r.Context(), req.BotChannelID, req.A)
	case resolveUser:
		value, err = s.svc.ResolveUserName(r.Context(), req.BotChannelID, req.A)
	case resolveName:
		value, err = s.svc.ResolveNameToID(r.Context(), req.BotChannelID, req.A, req.B)
	default:
		http.Error(w, "fcs rpc: unknown resolve kind "+req.Kind, http.StatusBadRequest)
		return
	}
	resp := wireResolveResp{Value: value}
	if err != nil {
		resp.Err = err.Error()
	}
	writeJSON(w, resp)
}

// handleInbound is the fanout stream: it subscribes the calling hub and writes each
// broadcast InboundEnvelope as one NDJSON line, flushing per line. It blocks until
// the hub disconnects (r.Context cancelled).
//
// CONCURRENCY: the ResponseWriter is touched ONLY by this (ServeHTTP) goroutine. The
// fanout runs on a different goroutine (service.Run), so the sink must not write to w
// directly — it hands each envelope over a buffered channel that this goroutine
// drains. This is what keeps a fanout mid-write from racing the request teardown.
// A full buffer or a torn-down request yields EffectRetryable (best-effort; per-hub
// retry + durable ledger is 后补, not 本轮).
func (s *rpcServer) handleInbound(w http.ResponseWriter, r *http.Request) {
	hub := r.URL.Query().Get("hub")
	if hub == "" {
		http.Error(w, "fcs rpc: inbound requires ?hub=<id>", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "fcs rpc: streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan InboundEnvelope, 64)
	retryable := func(env InboundEnvelope) FanoutReceipt {
		return FanoutReceipt{RequestID: env.RequestID, AuthenticatedHubID: hub, Effect: EffectRetryable, Attempt: 1}
	}
	sink := sinkFunc(func(ctx context.Context, env InboundEnvelope) (FanoutReceipt, error) {
		select {
		case ch <- env:
			return FanoutReceipt{RequestID: env.RequestID, AuthenticatedHubID: hub, Effect: EffectDelivered, Attempt: 1}, nil
		case <-r.Context().Done(): // hub disconnected / handler returning — never touch w
			return retryable(env), r.Context().Err()
		case <-ctx.Done():
			return retryable(env), ctx.Err()
		default: // buffer full: drop this hub's copy, do not block the fanout goroutine
			return retryable(env), nil
		}
	})

	cancel, err := s.svc.Subscribe(hub, sink)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer cancel()
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	flusher.Flush() // establish the stream so the client's Subscribe returns promptly
	s.logger.Info("fcs hub subscribed to inbound stream", "hub", hub)

	enc := json.NewEncoder(w)
	for {
		select {
		case <-r.Context().Done():
			s.logger.Info("fcs hub inbound stream closed", "hub", hub)
			return
		case env := <-ch:
			if err := enc.Encode(env); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleReceipt records a best-effort hub receipt. With no durable ledger this round
// it is logged only (per-hub retry lands with the ledger, 后补).
func (s *rpcServer) handleReceipt(w http.ResponseWriter, r *http.Request) {
	var rc FanoutReceipt
	if err := decodeJSON(r, &rc); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if rc.Effect != EffectDelivered {
		s.logger.Warn("fcs hub receipt non-delivered",
			"hub", rc.AuthenticatedHubID, "request_id", rc.RequestID, "effect", rc.Effect, "attempt", rc.Attempt)
	}
	w.WriteHeader(http.StatusAccepted)
}

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
