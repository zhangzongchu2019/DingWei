package m8

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func (h *Hub) SendProvision(ctx context.Context, keyID, sessionName, action, url, sha256Text, version, target string, extra map[string]any) error {
	keyID = stringsTrim(keyID)
	sessionName = stringsTrim(sessionName)
	if keyID == "" || sessionName == "" {
		return fmt.Errorf("key_id/session_name required")
	}
	h.mu.Lock()
	var c *sessionClient
	if sessions := h.sessionClients[keyID]; sessions != nil {
		c = sessions[sessionName]
	}
	h.mu.Unlock()
	if c == nil {
		return fmt.Errorf("session offline: %s#%s", sessionName, keyID)
	}
	meta := map[string]any{
		"type":      "provision",
		"system":    true,
		"no_mirror": true,
		"action":    stringsTrim(action),
		"url":       stringsTrim(url),
		"sha256":    stringsTrim(sha256Text),
		"version":   stringsTrim(version),
		"target":    stringsTrim(target),
	}
	if extra != nil {
		meta["extra"] = extra
	}
	env := model.Envelope{
		ID:   randomHex(16),
		To:   sessionAddress(sessionName, keyID),
		From: sessionAddress("workpulse", keyID),
		Body: "",
		TS:   time.Now().Unix(),
		Meta: meta,
	}
	return c.write(ctx, env)
}

func stringsTrim(v string) string {
	return strings.TrimSpace(v)
}
