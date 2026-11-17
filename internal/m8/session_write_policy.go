package m8

import (
	"context"
	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"io"
	"time"
)

const (
	sessionControlWriteLimit = 5 * time.Second
	sessionDataWriteLimit    = 20 * time.Second
	sessionControlMaxBytes   = 64 << 10
)

// Classification is server-side and never changes permission or durability.
// Unknown/large messages keep a bounded data budget; caller cancellation wins.
func sessionPhysicalWriteBudget(env model.Envelope, size int) time.Duration {
	if size <= sessionControlMaxBytes {
		switch metaString(env.Meta, "type") {
		case "outbound_receipt", "delivery_ack_confirm", "delivery_status_result_v1", "session_heartbeat_result_v1", "terminal_resize", "terminal_refresh", "terminal_interrupt", "agent_network_skill", "error":
			return sessionControlWriteLimit
		}
	}
	return sessionDataWriteLimit
}

// The no-fragment 10Mbps/3s-pause fixture fails healthy Ping before completion.
// Streaming retains one JSON message/ID while releasing the frame lock between
// bounded payload fragments; application JSON messages remain serialized.
const sessionFramePayloadLimit = 32 << 10

func (c *sessionClient) writeSessionPayload(ctx context.Context, payload []byte) error {
	if len(payload) <= sessionControlMaxBytes {
		return c.conn.Write(ctx, websocket.MessageText, payload)
	}
	writer, err := c.conn.Writer(ctx, websocket.MessageText)
	if err != nil {
		return err
	}
	for offset := 0; offset < len(payload); {
		end := offset + sessionFramePayloadLimit
		if end > len(payload) {
			end = len(payload)
		}
		n, err := writer.Write(payload[offset:end])
		if err != nil {
			return err
		}
		if n != end-offset {
			return io.ErrShortWrite
		}
		offset = end
	}
	return writer.Close()
}
