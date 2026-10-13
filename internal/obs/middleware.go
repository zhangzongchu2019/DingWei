package obs

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Middleware 给一个 ServeMux 外面包一层计时。route 标签取自 mux 的注册模板（go1.22 没有 r.Pattern，
// 这里用 mux.Handler(r) 再匹配一次），不取请求路径，所以基数受注册表约束。
//
// 被劫持的连接（WebSocket 升级）和 text/event-stream 响应不进短请求直方图，按流单独计数与计时。
// 包装器保留 Flush、Hijack、WriteString、ReadFrom 并实现 Unwrap，使 http.ResponseController 的
// SetWriteDeadline 等能力继续可用。
func (r *Registry) Middleware(server string, mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		route := routeOf(mux, req)
		method := methodLabel(req.Method)
		start := r.now()
		ow := &obsWriter{inner: w}
		var stream *streamStat
		ow.onStreamStart = func(kind string) { stream = r.streamOpened(server, route, kind) }

		panicked := true
		defer func() {
			d := r.now().Sub(start)
			if panicked {
				rec := recover()
				class := "panic"
				if rec == http.ErrAbortHandler {
					class = "aborted"
				}
				if stream != nil {
					r.streamClosed(stream, d, ow.bytes.Load())
				} else {
					r.observeRoute(server, route, method, class, ms(d))
				}
				panic(rec)
			}
			if stream != nil {
				r.streamClosed(stream, d, ow.bytes.Load())
				return
			}
			class := statusClass(ow.status, ow.wroteHeader, req.Context().Err() != nil)
			r.observeRoute(server, route, method, class, ms(d))
		}()
		mux.ServeHTTP(ow, req)
		panicked = false
	})
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func routeOf(mux *http.ServeMux, req *http.Request) string {
	_, pattern := mux.Handler(req)
	if pattern == "" {
		return unmatched
	}
	// 模板形如 "GET /api/x/{id}"；去掉方法前缀，只留路径模板。
	if i := strings.IndexByte(pattern, ' '); i >= 0 {
		pattern = strings.TrimSpace(pattern[i+1:])
	}
	return pattern
}

func methodLabel(m string) string {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions:
		return m
	}
	return "OTHER"
}

// statusClass：401／403／404／429 与 500／502／503／504 单列，其余按百位；没写出响应头且请求上下文已结束 ⇒ client_gone。
func statusClass(status int, wrote, ctxDone bool) string {
	if !wrote {
		if ctxDone {
			return "client_gone"
		}
		status = http.StatusOK // 处理器什么都没写，net/http 会补 200
	}
	switch status {
	case 401, 403, 404, 429, 500, 502, 503, 504:
		return itoa3(status)
	}
	switch {
	case status >= 100 && status < 200:
		return "1xx"
	case status < 300:
		return "2xx"
	case status < 400:
		return "3xx"
	case status < 500:
		return "4xx"
	case status < 600:
		return "5xx"
	}
	return "other"
}

func itoa3(n int) string {
	return string([]byte{byte('0' + n/100), byte('0' + n/10%10), byte('0' + n%10)})
}

type obsWriter struct {
	inner         http.ResponseWriter
	status        int
	wroteHeader   bool
	hijacked      bool
	bytes         atomic.Uint64
	onStreamStart func(kind string)
	streamStarted bool
}

func (w *obsWriter) Header() http.Header { return w.inner.Header() }

func (w *obsWriter) WriteHeader(code int) {
	if !w.wroteHeader && code >= 200 {
		w.wroteHeader = true
		w.status = code
		if strings.HasPrefix(strings.ToLower(w.inner.Header().Get("Content-Type")), "text/event-stream") {
			w.startStream("sse")
		}
	}
	w.inner.WriteHeader(code)
}

func (w *obsWriter) startStream(kind string) {
	if w.streamStarted {
		return
	}
	w.streamStarted = true
	if w.onStreamStart != nil {
		w.onStreamStart(kind)
	}
}

func (w *obsWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.inner.Write(p)
	w.bytes.Add(uint64(n))
	return n, err
}

func (w *obsWriter) WriteString(s string) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	var n int
	var err error
	if sw, ok := w.inner.(io.StringWriter); ok {
		n, err = sw.WriteString(s)
	} else {
		n, err = w.inner.Write([]byte(s))
	}
	w.bytes.Add(uint64(n))
	return n, err
}

func (w *obsWriter) ReadFrom(src io.Reader) (int64, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	var n int64
	var err error
	if rf, ok := w.inner.(io.ReaderFrom); ok {
		n, err = rf.ReadFrom(src)
	} else {
		n, err = io.Copy(writerOnly{w.inner}, src)
	}
	w.bytes.Add(uint64(n))
	return n, err
}

type writerOnly struct{ io.Writer }

func (w *obsWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.inner).Flush()
}

func (w *obsWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.inner).Hijack()
	if err == nil {
		w.hijacked = true
		w.wroteHeader = true
		w.status = http.StatusSwitchingProtocols
		w.startStream("upgrade")
	}
	return conn, rw, err
}

func (w *obsWriter) Unwrap() http.ResponseWriter { return w.inner }

// SnapshotHandler 输出 JSON 快照。鉴权由调用方在外面套（只给 management）。
func (r *Registry) SnapshotHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := json.NewEncoder(w).Encode(r.Snapshot()); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		return
	}
}
