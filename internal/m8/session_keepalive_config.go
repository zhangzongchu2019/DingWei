package m8

import (
	"fmt"
	"strings"
	"time"
)

// ConfigureSessionKeepalive is called before admitting connections. Empty
// values preserve the deployed 30s/5s defaults; invalid input never disables
// keepalive. These are positive bounded startup settings, not live tuning.
func (h *Hub) ConfigureSessionKeepalive(intervalText, timeoutText string) error {
	interval, timeout := defaultSessionKeepaliveInterval, defaultSessionKeepaliveTimeout
	var err error
	if strings.TrimSpace(intervalText) != "" {
		interval, err = time.ParseDuration(intervalText)
		if err != nil {
			return fmt.Errorf("invalid WP_SESSION_KEEPALIVE_INTERVAL")
		}
	}
	if strings.TrimSpace(timeoutText) != "" {
		timeout, err = time.ParseDuration(timeoutText)
		if err != nil {
			return fmt.Errorf("invalid WP_SESSION_KEEPALIVE_TIMEOUT")
		}
	}
	if interval < 10*time.Second || interval > 60*time.Second || timeout < time.Second || timeout > 30*time.Second || interval+timeout >= 75*time.Second {
		return fmt.Errorf("session keepalive outside bounded interval/timeout/idle budget")
	}
	h.keepaliveInterval, h.keepaliveTimeout = interval, timeout
	return nil
}

func (h *Hub) SessionKeepaliveInterval() time.Duration { return h.keepaliveInterval }
func (h *Hub) SessionKeepaliveTimeout() time.Duration  { return h.keepaliveTimeout }
