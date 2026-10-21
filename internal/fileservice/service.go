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
	PublicBaseURL string
}

func (s *Service) PublicHandler() http.Handler {
	return HTTPHandler{Manager: s.Manager, Capabilities: s.Capabilities, Uploads: s.UploadTickets, PublicBaseURL: s.PublicBaseURL}
}

func (s *Service) InternalHandler() http.Handler {
	mux := http.NewServeMux()
	var repo *Repository
	var clock Clock
	if s.Manager != nil {
		repo = s.Manager.Repo
		clock = s.Manager.Clock
	}
	issuer := IssuerHandler{Capabilities: s.Capabilities, Uploads: s.UploadTickets, Token: s.IssuerToken}
	mux.Handle("/internal/files/issue/", issuer)
	mux.Handle("/internal/files/audit", AuditHandler{Repo: repo, Token: s.AuditToken})
	mux.Handle("/internal/files/archive", ArchiveHandler{Manager: s.Manager, Token: s.IssuerToken, Clock: clock})
	mux.Handle("/internal/files/clarification-images", ClarificationImageHandler{Repo: repo, Token: s.IssuerToken})
	mux.Handle("/internal/files/spec-permanent", SpecPermanentHandler{Manager: s.Manager, Token: s.IssuerToken, PublicBaseURL: s.PublicBaseURL})
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
