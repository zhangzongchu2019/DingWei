package extshared

import (
	"fmt"
	"strings"
)

// MessageBodyMaxBytes 是最终业务正文的 UTF-8 字节上限，包含服务器抬头。
const MessageBodyMaxBytes = 1 << 20

// MessageJSONMaxBytes 容纳每字节最坏 JSON Unicode 转义及 64KiB 信封元数据。
const MessageJSONMaxBytes = 6*MessageBodyMaxBytes + (64 << 10)

type BodyTooLargeError struct {
	ActualBytes int `json:"actual_bytes"`
	LimitBytes  int `json:"limit_bytes"`
}

func (e *BodyTooLargeError) Error() string {
	return fmt.Sprintf("body_too_large actual_bytes=%d limit_bytes=%d", e.ActualBytes, e.LimitBytes)
}
func CheckMessageBody(body string) error {
	if len(body) > MessageBodyMaxBytes {
		return &BodyTooLargeError{len(body), MessageBodyMaxBytes}
	}
	return nil
}

// ExternalSenderHeader 只由已验证的服务器身份快照生成，两端复用以精确计算长度。
func ExternalSenderHeader(source, display, short, target string) string {
	owner := strings.TrimSpace(display)
	if owner == "" {
		owner = strings.TrimSpace(short)
	}
	return fmt.Sprintf("【来自外部 Agent %s（MCP，授权人 %s）→ %s；回复请发给 %s】", source, owner, target, source)
}
