package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

type acceptHeartbeat struct{ unixNano atomic.Int64 }

func (h *acceptHeartbeat) mark() { h.unixNano.Store(time.Now().UnixNano()) }

func (h *acceptHeartbeat) healthy(maxAge time.Duration) bool {
	last := h.unixNano.Load()
	return last > 0 && time.Since(time.Unix(0, last)) <= maxAge
}

type heartbeatListener struct {
	net.Listener
	heartbeat *acceptHeartbeat
}

func (l heartbeatListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.heartbeat.mark()
	}
	return c, err
}

func listenWithHeartbeat(addr string) (net.Listener, *acceptHeartbeat, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	hb := &acceptHeartbeat{}
	return heartbeatListener{Listener: ln, heartbeat: hb}, hb, nil
}

func pulseAddress(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func startAcceptPulse(ctx context.Context, addr string, interval time.Duration) {
	if interval <= 0 {
		return
	}
	addr = pulseAddress(addr)
	go func() {
		pulse := func() {
			conn, err := net.DialTimeout("tcp", addr, time.Second)
			if err == nil {
				_ = conn.Close()
			}
		}
		pulse()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pulse()
			}
		}
	}()
}

type livezDB interface{ Ping(context.Context) error }

func livezHandler(hub *m8.Hub, db livezDB, publicHB, internalHB *acceptHeartbeat) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lockTimeout := envDuration("WP_HUB_LIVEZ_LOCK_TIMEOUT", 2*time.Second)
		dbTimeout := envDuration("WP_HUB_LIVEZ_DB_TIMEOUT", 2*time.Second)
		acceptMaxAge := envDuration("WP_HUB_LIVEZ_ACCEPT_MAX_AGE", 20*time.Second)
		status := map[string]bool{}
		status["hub_lock"] = hub.ProbeLock(r.Context(), lockTimeout, 50*time.Millisecond)
		ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
		status["db"] = db != nil && db.Ping(ctx) == nil
		cancel()
		status["public_accept"] = publicHB != nil && publicHB.healthy(acceptMaxAge)
		status["internal_accept"] = internalHB != nil && internalHB.healthy(acceptMaxAge)
		healthy := status["hub_lock"] && status["db"] && status["public_accept"] && status["internal_accept"]
		w.Header().Set("Content-Type", "application/json")
		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": healthy, "probes": status})
	}
}

func sdNotify(state string) error {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" {
		return nil
	}
	if strings.HasPrefix(socket, "@") {
		socket = "\x00" + socket[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socket, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(state))
	return err
}

func watchdogInterval() (time.Duration, bool) {
	raw := strings.TrimSpace(os.Getenv("WATCHDOG_USEC"))
	usec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || usec <= 0 {
		return 0, false
	}
	return time.Duration(usec) * time.Microsecond, true
}

func startSystemdWatchdog(ctx context.Context, logger *slog.Logger, hub *m8.Hub) {
	window, enabled := watchdogInterval()
	if !enabled {
		return
	}
	interval := envDuration("WP_HUB_LOCK_WATCHDOG_INTERVAL", window/3)
	if interval <= 0 || interval >= window {
		interval = window / 3
	}
	probeTimeout := envDuration("WP_HUB_LOCK_WATCHDOG_PROBE_TIMEOUT", 200*time.Millisecond)
	confirmations := envInt("WP_HUB_LOCK_WATCHDOG_CONFIRMATIONS", envInt("WP_HUB_LOCK_WATCHDOG_FAILURES", 3))
	if confirmations < 1 {
		confirmations = 3
	}
	confirmInterval := envDuration("WP_HUB_LOCK_WATCHDOG_CONFIRM_INTERVAL", 500*time.Millisecond)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if hub.ProbeLock(ctx, probeTimeout, 50*time.Millisecond) {
					if err := sdNotify("WATCHDOG=1"); err != nil {
						logger.Warn("sd_notify watchdog failed", "error", err)
					}
					continue
				}
				logger.Warn("hub lock watchdog probe failed; starting fast confirmation", "confirmations", confirmations, "interval", confirmInterval)
				if watchdogFastConfirm(ctx, confirmations, confirmInterval, func() bool {
					return hub.ProbeLock(ctx, probeTimeout, 50*time.Millisecond)
				}) {
					logger.Info("hub lock watchdog recovered during fast confirmation")
					if err := sdNotify("WATCHDOG=1"); err != nil {
						logger.Warn("sd_notify watchdog recovery failed", "error", err)
					}
					continue
				}
				// Do not renew the lease after a confirmed failure. The loop keeps
				// probing, so a later recovery resumes feeding before systemd fires.
				logger.Error("hub lock watchdog confirmed stuck; withholding WATCHDOG")
			}
		}
	}()
}

func watchdogFastConfirm(ctx context.Context, attempts int, interval time.Duration, probe func() bool) bool {
	if attempts < 1 || probe == nil {
		return false
	}
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for attempt := 0; attempt < attempts; attempt++ {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
		}
		if probe() {
			return true
		}
		if attempt+1 < attempts {
			timer.Reset(interval)
		}
	}
	return false
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}
