package m8

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	viewUnlockControlPath = "/internal/control/view-page/unlock"
	hubToVSDirection      = "hub-to-view-service/v1"
)

type hmacViewPageUnlocker struct {
	client *http.Client
	now    func() time.Time
	nonce  func() (string, error)
}

type viewUnlockControlBody struct {
	CommandID    string `json:"command_id"`
	RequestID    string `json:"request_id"`
	Session      string `json:"session"`
	Code         string `json:"code"`
	OwnerKey     string `json:"owner_key"`
	SenderOpenID string `json:"sender_open_id"`
	GrantedAt    string `json:"granted_at"`
}

func newHMACViewPageUnlocker() *hmacViewPageUnlocker {
	return &hmacViewPageUnlocker{
		client: &http.Client{Timeout: 3 * time.Second},
		now:    time.Now,
		nonce: func() (string, error) {
			var raw [16]byte
			if _, err := rand.Read(raw[:]); err != nil {
				return "", err
			}
			return hex.EncodeToString(raw[:]), nil
		},
	}
}

func (u *hmacViewPageUnlocker) UnlockViewPage(ctx context.Context, request ViewPageUnlockRequest) error {
	targetName, target := loadViewRouting().resolve(request.FullSessionName, ownerFromSessionName(request.FullSessionName))
	if target.Builtin || !target.ViewUnlockV2 || strings.TrimSpace(target.URL) == "" || strings.TrimSpace(target.HubToVSSecretFile) == "" {
		return errors.New("页级解锁 capability 未启用")
	}
	secret, err := os.ReadFile(target.HubToVSSecretFile)
	if err != nil || len(bytes.TrimSpace(secret)) < 32 {
		return errors.New("页级解锁服务认证不可用")
	}
	secret = bytes.TrimSpace(secret)
	nonce, err := u.nonce()
	if err != nil {
		return errors.New("生成解锁请求失败")
	}
	now := u.now().UTC()
	commandID := strings.TrimSpace(request.MessageID)
	if commandID == "" {
		commandID = "unlock-" + nonce
	}
	body, err := json.Marshal(viewUnlockControlBody{
		CommandID: commandID, RequestID: commandID, Session: request.FullSessionName,
		Code: request.Code, OwnerKey: request.OwnerKey, SenderOpenID: request.SenderOpenID,
		GrantedAt: now.Format(time.RFC3339Nano),
	})
	if err != nil {
		return fmt.Errorf("encode unlock request: %w", err)
	}
	base, err := url.Parse(target.URL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return errors.New("页级解锁 target 地址无效")
	}
	endpoint := *base
	endpoint.Path = strings.TrimRight(base.Path, "/") + viewUnlockControlPath
	endpoint.RawQuery = ""
	timestamp := fmt.Sprintf("%d", now.Unix())
	signature := signViewServiceRequest(secret, hubToVSDirection, http.MethodPost, endpoint.Path, timestamp, nonce, body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build unlock request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-DW-VS-Target", targetName)
	httpReq.Header.Set("X-DW-Timestamp", timestamp)
	httpReq.Header.Set("X-DW-Nonce", nonce)
	httpReq.Header.Set("X-DW-Request-ID", commandID)
	httpReq.Header.Set("X-DW-Signature", signature)
	resp, err := u.client.Do(httpReq)
	if err != nil {
		return errors.New("页级解锁服务暂不可达")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return errors.New("页面码不存在")
	case http.StatusConflict:
		return errors.New("页面已过期或撤销")
	default:
		return errors.New("页级解锁服务拒绝请求")
	}
}

func signViewServiceRequest(secret []byte, direction, method, path, timestamp, nonce string, body []byte) string {
	directionKey := hmac.New(sha256.New, secret)
	_, _ = directionKey.Write([]byte(direction))
	bodyHash := sha256.Sum256(body)
	canonical := method + "\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + hex.EncodeToString(bodyHash[:])
	signer := hmac.New(sha256.New, directionKey.Sum(nil))
	_, _ = signer.Write([]byte(canonical))
	return hex.EncodeToString(signer.Sum(nil))
}
