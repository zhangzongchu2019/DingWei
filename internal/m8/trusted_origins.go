package m8

import "github.com/zhangzongchu2019/dingwei/internal/trustedorigin"

func (h *Hub) originAllowed(raw string, legacyAllowed bool, headerValues int) bool {
	return trustedorigin.Allows(raw, h.PublicOrigin, h.TrustedOrigins, legacyAllowed, headerValues)
}
