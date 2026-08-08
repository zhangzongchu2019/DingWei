// replay.go —— 模拟件①【真实消息回放】:把 dingwei hub(dingwei.wegoab.com)拷下来的消息记录
// 逐条喂进收件面。格式=JSONL,每行一条 ReplayRecord。
//
// 关键红线(与生产同构):记录里的 session 只是"会话引用",principal 必须经 Authenticator
// 盖章得出;盖不了章的记录不进收件面(fail-closed),绝不信正文/记录自称。
package demo

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/mcsd"
)

// ReplayRecord 一条回放记录(从 hub 消息日志拷/转换)。
type ReplayRecord struct {
	Session string `json:"session"`            // 会话引用(盖章输入,非身份本身)
	MetaOp  string `json:"meta_op,omitempty"`  // 结构化 op(有则填)
	TaskID  string `json:"task_id,omitempty"`
	Segment string `json:"segment,omitempty"`
	Body    string `json:"body"`
}

// ReplayResult 单条回放结果(演示日志/断言用)。
type ReplayResult struct {
	Line     int
	Rec      ReplayRecord
	Stamped  bool   // false=盖章失败被拦(未进收件面)
	StampErr string // 盖章失败原因
	Decision mcsd.Decision
}

// Replay 逐行回放:盖章→收件面 Handle。emit 每条回调(nil=不回调);返回处理行数。
// 收件面自身的拒(路由闸)不算错误——那正是演示要看的;只有 IO/库错误才中断。
func Replay(ctx context.Context, r io.Reader, auth Authenticator, rc *mcsd.Receiver, emit func(ReplayResult)) (int, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		n++
		var rec ReplayRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return n, fmt.Errorf("demo/replay: 第 %d 行 JSON 解析失败: %w", n, err)
		}
		res := ReplayResult{Line: n, Rec: rec}
		stamp, err := auth.StampSession(ctx, rec.Session)
		if err != nil {
			res.StampErr = err.Error() // fail-closed:不进收件面
			if emit != nil {
				emit(res)
			}
			continue
		}
		res.Stamped = true
		d, err := rc.Handle(ctx, mcsd.Inbound{
			Principal: stamp.Principal,
			MetaOp:    rec.MetaOp,
			TaskID:    rec.TaskID,
			Segment:   rec.Segment,
			Body:      rec.Body,
		})
		if err != nil {
			return n, fmt.Errorf("demo/replay: 第 %d 行收件失败: %w", n, err)
		}
		res.Decision = d
		if emit != nil {
			emit(res)
		}
	}
	return n, sc.Err()
}
