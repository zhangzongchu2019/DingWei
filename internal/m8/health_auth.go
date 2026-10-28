package m8

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/version"
)

const HealthAuthPath = "/internal/health/auth"
const healthWindow = int64(60)

var healthNoncePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// NewHealthAuthHandler is registered only on the internal HTTP listener. Keys
// are independent of application credentials and reread for rotation. Nonces
// are atomically claimed after authentication; capacity exhaustion fails closed.
func NewHealthAuthHandler() http.Handler {
	return &healthAuthHandler{secretFile: os.Getenv("WP_HEALTH_SECRET_FILE"), target: os.Getenv("WP_HEALTH_TARGET"), now: time.Now, nonces: make(map[string]int64)}
}

type healthAuthHandler struct {
	secretFile, target string
	now                func() time.Time
	mu                 sync.Mutex
	nonces             map[string]int64
}

func (h *healthAuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"health": "ok", "version": version.Version, "target": h.target})
}

func (h *healthAuthHandler) authorized(r *http.Request) bool {
	if r.Method != http.MethodGet || r.URL.EscapedPath() != HealthAuthPath || r.URL.RawQuery != "" || r.ContentLength > 0 || len(r.TransferEncoding) > 0 || !validHealthTarget(h.target) {
		return false
	}
	single := func(name string) string {
		values := r.Header.Values(name)
		if len(values) != 1 {
			return ""
		}
		return values[0]
	}
	timestamp, nonce, signature := single("X-DW-Health-Timestamp"), single("X-DW-Health-Nonce"), single("X-DW-Health-Signature")
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	now := h.now().Unix()
	if err != nil || strconv.FormatInt(ts, 10) != timestamp || ts < now-healthWindow || ts > now+healthWindow || !healthNoncePattern.MatchString(nonce) {
		return false
	}
	supplied, err := hex.DecodeString(signature)
	if err != nil || len(supplied) != sha256.Size {
		return false
	}
	f, err := os.Open(h.secretFile)
	if err != nil {
		return false
	}
	defer f.Close()
	key, err := io.ReadAll(io.LimitReader(f, 4097))
	if len(key) > 4096 {
		return false
	}
	key = []byte(strings.TrimSpace(string(key)))
	if err != nil || len(key) < 32 || len(key) > 4096 {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = io.WriteString(mac, "dingwei-health/v1\nGET\n"+HealthAuthPath+"\n"+h.target+"\n"+timestamp+"\n"+nonce)
	if !hmac.Equal(supplied, mac.Sum(nil)) {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for n, expiry := range h.nonces {
		if expiry < now {
			delete(h.nonces, n)
		}
	}
	if _, exists := h.nonces[nonce]; exists || len(h.nonces) >= 10000 {
		return false
	}
	h.nonces[nonce] = ts + healthWindow
	return true
}

func validHealthTarget(target string) bool {
	if len(target) == 0 || len(target) > 64 {
		return false
	}
	for _, c := range target {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
