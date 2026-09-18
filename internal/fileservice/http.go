package fileservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type UploadGrant struct {
	OwnerKey    string
	SessionName string
	MaxBytes    int64
	ExpiresAt   time.Time
}

// UploadVerifier is intentionally separate from CapabilityVerifier. A View
// upload ticket must never become a retrieval capability by type confusion.
type UploadVerifier interface {
	VerifyUpload(raw, sessionName string, contentLength int64, now time.Time) (UploadGrant, error)
}

type HTTPHandler struct {
	Manager      *Manager
	Capabilities CapabilityVerifier
	Uploads      UploadVerifier
}

func (h HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	switch {
	case r.URL.Path == "/files/upload" && r.Method == http.MethodPost:
		h.upload(w, r)
	case strings.HasPrefix(r.URL.Path, "/files/ref/") && (r.Method == http.MethodHead || r.Method == http.MethodGet):
		h.retrieve(w, r)
	default:
		hide(w)
	}
}

func (h HTTPHandler) upload(w http.ResponseWriter, r *http.Request) {
	if h.Manager == nil || h.Manager.Clock == nil || h.Uploads == nil {
		uploadError(w, http.StatusServiceUnavailable, "upload service unavailable")
		return
	}
	if r.ContentLength < 0 {
		uploadError(w, http.StatusBadRequest, "content length is required")
		return
	}
	if r.ContentLength > h.Manager.MaxFileBytes {
		uploadError(w, http.StatusRequestEntityTooLarge, "file exceeds upload limit")
		return
	}
	session := r.Header.Get("X-DingWei-Session")
	grant, err := h.Uploads.VerifyUpload(bearer(r), session, r.ContentLength, h.Manager.Clock.Now())
	if err != nil || grant.OwnerKey == "" || grant.SessionName != session || (grant.MaxBytes > 0 && r.ContentLength > grant.MaxBytes) {
		uploadError(w, http.StatusUnauthorized, "upload ticket is invalid or expired")
		return
	}
	f, err := h.Manager.Put(r.Context(), Upload{
		OwnerKey: grant.OwnerKey, SessionName: grant.SessionName,
		OriginalName: r.Header.Get("X-File-Name"), MediaType: r.Header.Get("Content-Type"),
		ExpectedSHA256: r.Header.Get("X-Content-SHA256"), ContentLength: r.ContentLength, Body: r.Body,
	})
	if err != nil {
		if errors.Is(err, ErrIntegrity) {
			uploadError(w, http.StatusUnprocessableEntity, "uploaded bytes failed integrity validation")
		} else if errors.Is(err, ErrInvalidUpload) {
			uploadError(w, http.StatusBadRequest, "invalid upload")
		} else {
			uploadError(w, http.StatusServiceUnavailable, "upload storage unavailable")
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"file_ref": "dwfile:" + f.ID, "name": f.OriginalName, "size": f.SizeBytes,
		"sha256": f.SHA256, "media_type": f.MediaType, "expires_at": f.ExpiresAt, "range": true,
	})
}

func uploadError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (h HTTPHandler) retrieve(w http.ResponseWriter, r *http.Request) {
	if h.Manager == nil {
		hide(w)
		return
	}
	fileID, err := url.PathUnescape(strings.TrimPrefix(r.URL.EscapedPath(), "/files/ref/"))
	if err != nil || fileID == "" || strings.Contains(fileID, "/") {
		hide(w)
		return
	}
	byteRange := r.Header.Get("Range")
	if r.Method == http.MethodHead {
		if byteRange != "" {
			hide(w)
			return
		}
		f, err := h.Manager.Describe(r.Context(), h.Capabilities, bearer(r), r.Method, fileID, "")
		if h.fileError(w, err) {
			return
		}
		setDownloadHeaders(w, f)
		w.Header().Set("Content-Length", strconv.FormatInt(f.SizeBytes, 10))
		w.WriteHeader(http.StatusOK)
		return
	}
	start, length, err := parseSingleRange(byteRange, -1)
	if err != nil {
		// Authenticate before revealing whether the requested range is valid for
		// the object. Size-dependent validation follows authorization below.
		if _, authErr := h.Manager.Describe(r.Context(), h.Capabilities, bearer(r), r.Method, fileID, byteRange); authErr != nil {
			h.fileError(w, authErr)
			return
		}
		http.Error(w, "range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	f, err := h.Manager.Describe(r.Context(), h.Capabilities, bearer(r), r.Method, fileID, byteRange)
	if h.fileError(w, err) {
		return
	}
	start, length, err = parseSingleRange(byteRange, f.SizeBytes)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", f.SizeBytes))
		http.Error(w, "range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	f, body, err := h.Manager.Open(r.Context(), h.Capabilities, bearer(r), fileID, byteRange, start, length)
	if err != nil {
		if h.fileError(w, err) {
			return
		}
		http.Error(w, "read failed", http.StatusServiceUnavailable)
		return
	}
	defer body.Close()
	setDownloadHeaders(w, f)
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, f.SizeBytes))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = io.CopyN(w, body, length)
}

func (h HTTPHandler) fileError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotReady) {
		http.Error(w, "gone", http.StatusGone)
	} else if errors.Is(err, ErrAccessDenied) || errors.Is(err, ErrCapabilityDenied) {
		hide(w)
	} else {
		http.Error(w, "read failed", http.StatusServiceUnavailable)
	}
	return true
}

func parseSingleRange(raw string, size int64) (int64, int64, error) {
	if !strings.HasPrefix(raw, "bytes=") || strings.Contains(raw, ",") {
		return 0, 0, ErrAccessDenied
	}
	parts := strings.Split(strings.TrimPrefix(raw, "bytes="), "-")
	if len(parts) != 2 || (parts[0] == "" && parts[1] == "") {
		return 0, 0, ErrAccessDenied
	}
	if size < 0 { // syntax-only pass before authorization
		for _, p := range parts {
			if p != "" {
				if _, err := strconv.ParseInt(p, 10, 64); err != nil {
					return 0, 0, err
				}
			}
		}
		return 0, 0, nil
	}
	if size <= 0 {
		return 0, 0, ErrAccessDenied
	}
	if parts[0] == "" {
		n, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, ErrAccessDenied
		}
		if n > size {
			n = size
		}
		return size - n, n, nil
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, ErrAccessDenied
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return 0, 0, ErrAccessDenied
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end - start + 1, nil
}

func setDownloadHeaders(w http.ResponseWriter, f File) {
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(safeName(f.OriginalName)))
	w.Header().Set("X-Content-SHA256", f.SHA256)
	w.Header().Set("X-DingWei-Media-Type", f.MediaType)
}
func bearer(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if !strings.HasPrefix(v, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(v, "Bearer ")
}
func hide(w http.ResponseWriter) { http.Error(w, "not found", http.StatusNotFound) }
