package fcs

// webhookingress.go — FCS-process Feishu webhook ingress (走甲 phase-2). Moves the
// hub's POST /webhook/{channel} handling (cmd/workpulse handleFeishuWebhook) into
// FCS so the webhook transport, like the WS transport, terminates at the single
// Feishu boundary and feeds the SAME dedup+fanout pipeline (service.Ingest). It is
// byte-equivalent to the hub handler: resolve channel → verify (WebhookVerifier) →
// challenge shortcut → parse (feishu.WebhookMessageFromPayload) → Ingest.
//
// Deployments using WS transport don't need this (a.Run drives ingress); it exists
// so a webhook-configured tenant also works in the independent FCS process, closing
// the transport gap for grayscale.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Ingester is the FCS ingress sink: one inbound model.Message through dedup+fanout.
// Implemented by *service (Ingest); kept as an interface so the webhook ingress does
// not depend on the concrete service type.
type Ingester interface {
	Ingest(ctx context.Context, m model.Message) error
}

// NewWebhookIngress returns the POST /webhook/{channel} handler for the FCS process.
// It resolves the channel via provider, verifies+decrypts with the channel's webhook
// keys, answers the url_verification challenge, then parses + Ingests the event.
// logger may be nil.
func NewWebhookIngress(provider BotChannelProvider, ingester Ingester, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		channel := strings.TrimSpace(r.PathValue("channel"))
		if channel == "" {
			channel = "default"
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		botID, err := resolveWebhookChannel(r.Context(), provider, channel)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		encKey, vtoken, err := provider.WebhookKeys(r.Context(), botID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		verifier := NewWebhookVerifier(func(context.Context, string) (string, string, error) {
			return encKey, vtoken, nil
		})
		data, _, err := verifier.VerifyWebhook(r.Context(), botID, raw)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if challenge := WebhookChallenge(data); challenge != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"challenge": challenge})
			return
		}
		msg, err := feishu.WebhookMessageFromPayload(botID, data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := ingester.Ingest(r.Context(), msg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"queued"}`))
	})
}

// resolveWebhookChannel maps a channel path segment (id or name) to a canonical bot
// channel id via the provider's active list, checking it can receive. Falls back to
// the raw segment when unknown (byte-equivalent to hub resolveWebhookBotChannel's
// permissive default for bootstrap/synthetic channels).
func resolveWebhookChannel(ctx context.Context, provider BotChannelProvider, channel string) (string, error) {
	channels, err := provider.ListActive(ctx)
	if err != nil {
		return "", err
	}
	for _, ch := range channels {
		if strings.EqualFold(ch.ID, channel) || strings.EqualFold(ch.Name, channel) {
			if !ch.CanReceive {
				return "", errChannelCannotReceive(ch.ID)
			}
			return ch.ID, nil
		}
	}
	return channel, nil
}

type errChannelCannotReceive string

func (e errChannelCannotReceive) Error() string {
	return "bot channel " + string(e) + " cannot receive webhook"
}
