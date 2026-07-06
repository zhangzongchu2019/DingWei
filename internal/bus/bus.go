// Package bus 定义消息总线（M0）：按会话主体分区的收/发队列抽象。
//
// 规范 §2.5 / §13.3：持久化层（at-least-once，幂等）与转发投递语义分离。
// 容量基线 ≤200 写/秒 → SQLite 表队列即可（§13.2），无需 Redis。
package bus

import (
	"context"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Queue 是按会话主体分区的持久化队列抽象（in / out 各一套）。
// 同主体内 FIFO 保序；主体间隔离。后端：SQLite 表（默认）/ Redis Stream（大规模可选）。
type Queue interface {
	// Enqueue 入队（持久化）。
	Enqueue(ctx context.Context, m model.Message) error
	// Dequeue 取下一条待处理（按会话主体保序）；无则阻塞或返回 nil（实现定）。
	Dequeue(ctx context.Context) (*model.Message, error)
	// Ack 标记处理完成。
	Ack(ctx context.Context, id string) error
	// Fail 标记失败（用于重试/死信，仅内部消费语义；字头 WS 转发为 at-most-once 不重投，见 §13.3）。
	Fail(ctx context.Context, id string, reason string) error
}

// Handler 接收消费者处理函数：inbound → 路由 → 业务/转发。
type Handler func(ctx context.Context, m model.Message) error
