package feishu

// request_id 确定性派生(T17 v2.3 §C1):UUIDv5(NS_T17OB, name),
// name=长度前缀 canonical tuple("t17ob.v1" 0x1F kind 0x1F <len>:<comp1> 0x1F <len>:<comp2>)。
// 同一业务发送在 HTTP 重试/worker 重试/进程重启后派生值恒同=托管幂等键稳定来源。

import (
	"crypto/sha1"
	"fmt"
)

// nsT17OB §C1 固定 namespace 常量(3f2504e0-4f89-41d3-9a0c-0305e82c3301)。
var nsT17OB = [16]byte{0x3f, 0x25, 0x04, 0xe0, 0x4f, 0x89, 0x41, 0xd3,
	0x9a, 0x0c, 0x03, 0x05, 0xe8, 0x2c, 0x33, 0x01}

// DeriveRequestID kind∈{reply,schedule};reply=(触发消息ID, send_slot),
// schedule=(schedule项ID, occurrence)。产物=UUIDv5 字符串(§K2 正则可过)。
func DeriveRequestID(kind, comp1, comp2 string) string {
	name := fmt.Sprintf("t17ob.v1\x1f%s\x1f%d:%s\x1f%d:%s", kind, len(comp1), comp1, len(comp2), comp2)
	h := sha1.New()
	h.Write(nsT17OB[:])
	h.Write([]byte(name))
	sum := h.Sum(nil)
	var u [16]byte
	copy(u[:], sum[:16])
	u[6] = (u[6] & 0x0f) | 0x50 // version 5
	u[8] = (u[8] & 0x3f) | 0x80 // RFC4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}
