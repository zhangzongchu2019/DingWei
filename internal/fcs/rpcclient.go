package fcs

// rpcclient.go — hub side of the phase-2 hub↔FCS HTTP RPC (走甲). Implements the
// Service seam by calling a remote FCS process. Because NewHubGateway/NewHubReceiver
// program against the Service interface, wiring the hub to a remote FCS is a pure
// composition-root swap (cmd/workpulse: in-proc *service → NewRPCClient) — no hub
// business code changes. Mirror-image of NewRPCServer.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NewRPCClient returns a Service backed by a remote FCS process at baseURL (e.g.
// "http://fcs:8080"). hc may be nil (a default client with no overall timeout is
// used — the inbound stream is long-lived; per-call timeouts come from ctx). The
// returned Service is safe for concurrent use.
func NewRPCClient(baseURL string, hc *http.Client) Service {
	if hc == nil {
		hc = &http.Client{} // NO client.Timeout: the inbound stream is long-lived
	}
	return &rpcClient{base: strings.TrimRight(baseURL, "/"), hc: hc}
}

type rpcClient struct {
	base string
	hc   *http.Client
}

func (c *rpcClient) Send(ctx context.Context, req OutboundRequest) (OutboundResult, error) {
	var resp wireSendResp
	if err := c.postJSON(ctx, RPCPathSend, req, &resp); err != nil {
		return OutboundResult{}, err
	}
	if resp.Err != "" {
		return OutboundResult{}, errors.New(resp.Err)
	}
	return OutboundResult{FeishuMsgID: resp.FeishuMsgID}, nil
}

func (c *rpcClient) ResolveChatName(ctx context.Context, botChannelID, chatID string) (string, error) {
	return c.resolve(ctx, wireResolveReq{Kind: resolveChat, BotChannelID: botChannelID, A: chatID})
}
func (c *rpcClient) ResolveUserName(ctx context.Context, botChannelID, openID string) (string, error) {
	return c.resolve(ctx, wireResolveReq{Kind: resolveUser, BotChannelID: botChannelID, A: openID})
}
func (c *rpcClient) ResolveNameToID(ctx context.Context, botChannelID, name, chatType string) (string, error) {
	return c.resolve(ctx, wireResolveReq{Kind: resolveName, BotChannelID: botChannelID, A: name, B: chatType})
}

func (c *rpcClient) resolve(ctx context.Context, req wireResolveReq) (string, error) {
	var resp wireResolveResp
	if err := c.postJSON(ctx, RPCPathResolve, req, &resp); err != nil {
		return "", err
	}
	if resp.Err != "" {
		return "", errors.New(resp.Err)
	}
	return resp.Value, nil
}

// Subscribe opens the FCS inbound fanout stream for authenticatedHubID and pumps
// each broadcast InboundEnvelope into sink in a background goroutine, posting the
// resulting receipt back (best-effort). The returned cancel closes the stream and
// stops the pump. It blocks until the stream is established (or errors) so callers
// (receiverClient.Start) know the subscription is live before returning.
func (c *rpcClient) Subscribe(authenticatedHubID string, sink InboundSink) (func(), error) {
	if authenticatedHubID == "" || sink == nil {
		return nil, fmt.Errorf("fcs rpc: subscribe requires non-empty hub id and sink")
	}
	ctx, cancel := context.WithCancel(context.Background())
	body, err := c.openInbound(ctx, authenticatedHubID)
	if err != nil {
		cancel()
		return nil, err
	}
	go c.maintainInbound(ctx, body, authenticatedHubID, sink)
	return cancel, nil
}

func (c *rpcClient) openInbound(ctx context.Context, authenticatedHubID string) (io.ReadCloser, error) {
	u := c.base + RPCPathInbound + "?hub=" + url.QueryEscape(authenticatedHubID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fcs rpc: open inbound stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		resp.Body.Close()
		return nil, fmt.Errorf("fcs rpc: inbound stream status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}

// maintainInbound restores the subscription after a clean EOF or transport
// failure. Events during the gap remain at-most-once until the durable ledger
// lands, but a transient disconnect must not permanently blind the hub.
func (c *rpcClient) maintainInbound(ctx context.Context, body io.ReadCloser, hubID string, sink InboundSink) {
	backoff := 250 * time.Millisecond
	for {
		c.pumpInbound(ctx, body, hubID, sink)
		if ctx.Err() != nil {
			return
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		var err error
		body, err = c.openInbound(ctx, hubID)
		if err != nil {
			if backoff < 5*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 250 * time.Millisecond
	}
}

// pumpInbound reads NDJSON InboundEnvelope lines and delivers each to sink, posting
// the receipt back best-effort. It returns when the stream ends or ctx is cancelled.
func (c *rpcClient) pumpInbound(ctx context.Context, body io.ReadCloser, hubID string, sink InboundSink) {
	defer body.Close()
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20) // allow large envelopes (content_json)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var env InboundEnvelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue // skip a malformed line rather than tearing down the stream
		}
		receipt, _ := sink.DeliverInbound(ctx, env)
		c.postReceipt(ctx, receipt)
		if ctx.Err() != nil {
			return
		}
	}
}

// postReceipt reports a fanout receipt back to FCS (best-effort; ledger 后补).
func (c *rpcClient) postReceipt(ctx context.Context, receipt FanoutReceipt) {
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = c.postJSON(rctx, RPCPathReceipt, receipt, nil) // best-effort
}

// postJSON POSTs body as JSON to path and, if out != nil, decodes the JSON response.
// A non-2xx status is a transport error; app-level errors ride in the response body.
func (c *rpcClient) postJSON(ctx context.Context, path string, body, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("fcs rpc %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// Compile-time assertion: the RPC client satisfies the Service seam (so it plugs
// into NewHubGateway/NewHubReceiver with zero hub code changes).
var _ Service = (*rpcClient)(nil)
