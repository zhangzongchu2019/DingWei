package fileservice

import (
	"context"
	"net/http"
	"time"
)

type Service struct {
	Manager       *Manager
	Capabilities  *GrantStore
	UploadTickets *UploadTicketStore
	IssuerToken   string
	AuditToken    string
}

func (s *Service) PublicHandler() http.Handler {
	return HTTPHandler{Manager: s.Manager, Capabilities: s.Capabilities, Uploads: s.UploadTickets}
}

func (s *Service) InternalHandler() http.Handler {
	mux := http.NewServeMux()
	var repo *Repository
	if s.Manager != nil {
		repo = s.Manager.Repo
	}
	issuer := IssuerHandler{Capabilities: s.Capabilities, Uploads: s.UploadTickets, Token: s.IssuerToken}
	mux.Handle("/internal/files/issue/", issuer)
	mux.Handle("/internal/files/audit", AuditHandler{Repo: repo, Token: s.AuditToken})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if s.Manager == nil || s.Manager.Repo == nil || s.Manager.Objects == nil || s.Capabilities == nil || s.UploadTickets == nil || s.IssuerToken == "" || s.AuditToken == "" {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func (s *Service) RunSweeper(ctx context.Context, interval time.Duration, onError func(error)) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Manager.Sweep(ctx); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}
