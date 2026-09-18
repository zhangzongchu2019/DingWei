package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type AdminFile struct {
	ID           string    `json:"ID"`
	OriginalName string    `json:"OriginalName"`
	SHA256       string    `json:"SHA256"`
	MediaType    string    `json:"MediaType"`
	OwnerKey     string    `json:"OwnerKey"`
	SessionName  string    `json:"SessionName"`
	SizeBytes    int64     `json:"SizeBytes"`
	CreatedAt    time.Time `json:"CreatedAt"`
	ExpiresAt    time.Time `json:"ExpiresAt"`
	Archived     bool      `json:"Archived"`
}

type FilePage struct {
	Files      []AdminFile
	NextOffset int
}
type FileContent struct {
	Body      io.ReadCloser
	Size      int64
	MediaType string
}

// FileBrowser deliberately exposes no filesystem path. Implementations must
// obtain bytes through file-service capabilities.
type FileBrowser interface {
	List(context.Context, int, int) (FilePage, error)
	Open(context.Context, string) (FileContent, error)
}

func (s *Server) filesPage(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil {
		http.Error(w, "file browser disabled: startup configuration was not injected", http.StatusServiceUnavailable)
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	page, err := s.Files.List(r.Context(), offset, 50)
	if err != nil {
		http.Error(w, "file browser audit request failed: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!doctype html><meta charset=utf-8><title>文件对象</title><h3>文件对象</h3><p><a href="/admin">返回</a></p><table><tr><th>fileid</th><th>原名</th><th>字节</th><th>sha256</th><th>上传时刻</th><th>来源</th><th>archived</th><th>预计清理</th><th>内容</th></tr>`)
	for _, f := range page.Files {
		cleanup := f.ExpiresAt.UTC().Format(time.RFC3339)
		if f.Archived {
			cleanup = "无限豁免"
		}
		_, _ = fmt.Fprintf(w, `<tr><td><code>%s</code></td><td>%s</td><td>%d</td><td><code>%s</code></td><td>%s</td><td>%s/%s</td><td>%t</td><td>%s</td><td><a href="/admin/files/%s/content">预览/下载</a></td></tr>`, html.EscapeString(f.ID), html.EscapeString(f.OriginalName), f.SizeBytes, html.EscapeString(f.SHA256), f.CreatedAt.UTC().Format(time.RFC3339), html.EscapeString(f.OwnerKey), html.EscapeString(f.SessionName), f.Archived, cleanup, url.PathEscape(f.ID))
	}
	_, _ = io.WriteString(w, `</table>`)
	if page.NextOffset > 0 {
		_, _ = fmt.Fprintf(w, `<a href="/admin/files?offset=%d">下一页</a>`, page.NextOffset)
	}
}

func (s *Server) fileContent(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil {
		http.NotFound(w, r)
		return
	}
	c, err := s.Files.Open(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer c.Body.Close()
	mediaType, inline := adminPreviewMediaType(c.MediaType)
	w.Header().Set("Content-Type", mediaType)
	if !inline {
		w.Header().Set("Content-Disposition", "attachment")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.FormatInt(c.Size, 10))
	_, _ = io.Copy(w, c.Body)
}

func adminPreviewMediaType(raw string) (string, bool) {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "application/octet-stream", false
	}
	mediaType = strings.ToLower(mediaType)
	inline := (strings.HasPrefix(mediaType, "image/") && mediaType != "image/svg+xml") ||
		strings.HasPrefix(mediaType, "video/") || mediaType == "application/pdf" || mediaType == "text/plain"
	if !inline {
		return "application/octet-stream", false
	}
	return mediaType, true
}

type HTTPFileBrowser struct {
	InternalURL, PublicURL, IssuerToken, AuditToken string
	Client                                          *http.Client
}

func NewHTTPFileBrowser(internalURL, publicURL, issuer, audit string) *HTTPFileBrowser {
	return &HTTPFileBrowser{strings.TrimRight(internalURL, "/"), strings.TrimRight(publicURL, "/"), issuer, audit, &http.Client{Timeout: 15 * time.Second}}
}

func (b *HTTPFileBrowser) List(ctx context.Context, offset, limit int) (FilePage, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/internal/files/audit?state=ready&limit=%d&offset=%d", b.InternalURL, limit, offset), nil)
	req.Header.Set("Authorization", "Bearer "+b.AuditToken)
	resp, err := b.Client.Do(req)
	if err != nil {
		return FilePage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return FilePage{}, fmt.Errorf("audit status %d", resp.StatusCode)
	}
	var raw struct {
		Files      []AdminFile `json:"files"`
		NextOffset int         `json:"next_offset"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&raw); err != nil {
		return FilePage{}, err
	}
	return FilePage{raw.Files, raw.NextOffset}, nil
}

func (b *HTTPFileBrowser) ticket(ctx context.Context, id, subject, method, byteRange string) (string, error) {
	body := strings.NewReader(fmt.Sprintf(`{"subject":%q,"file_id":%q,"method":%q,"range":%q,"ttl_seconds":60}`, subject, id, method, byteRange))
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, b.InternalURL+"/internal/files/issue/retrieval", body)
	req.Header.Set("Authorization", "Bearer "+b.IssuerToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("ticket status %d", resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || out.Token == "" {
		return "", fmt.Errorf("invalid ticket")
	}
	return out.Token, nil
}

func (b *HTTPFileBrowser) Open(ctx context.Context, id string) (FileContent, error) {
	if len(id) != 32 {
		return FileContent{}, fmt.Errorf("invalid id")
	}
	metaReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, b.InternalURL+"/internal/files/audit?file_id="+url.QueryEscape(id), nil)
	metaReq.Header.Set("Authorization", "Bearer "+b.AuditToken)
	metaResp, err := b.Client.Do(metaReq)
	if err != nil {
		return FileContent{}, err
	}
	defer metaResp.Body.Close()
	var meta struct {
		Files []AdminFile `json:"files"`
	}
	if metaResp.StatusCode != 200 || json.NewDecoder(metaResp.Body).Decode(&meta) != nil || len(meta.Files) != 1 {
		return FileContent{}, fmt.Errorf("metadata unavailable")
	}
	subject := meta.Files[0].OwnerKey + "/" + meta.Files[0].SessionName
	headToken, err := b.ticket(ctx, id, subject, "HEAD", "")
	if err != nil {
		return FileContent{}, err
	}
	head, _ := http.NewRequestWithContext(ctx, http.MethodHead, b.PublicURL+"/files/ref/"+id, nil)
	head.Header.Set("Authorization", "Bearer "+headToken)
	hr, err := b.Client.Do(head)
	if err != nil {
		return FileContent{}, err
	}
	hr.Body.Close()
	if hr.StatusCode != 200 {
		return FileContent{}, fmt.Errorf("head status %d", hr.StatusCode)
	}
	size, err := strconv.ParseInt(hr.Header.Get("Content-Length"), 10, 64)
	if err != nil || size < 0 || size > 8<<20 {
		return FileContent{}, fmt.Errorf("preview too large")
	}
	media := hr.Header.Get("X-DingWei-Media-Type")
	if media == "" {
		media = "application/octet-stream"
	}
	rng := fmt.Sprintf("bytes=0-%d", size-1)
	token, err := b.ticket(ctx, id, subject, "GET", rng)
	if err != nil {
		return FileContent{}, err
	}
	get, _ := http.NewRequestWithContext(ctx, http.MethodGet, b.PublicURL+"/files/ref/"+id, nil)
	get.Header.Set("Authorization", "Bearer "+token)
	get.Header.Set("Range", rng)
	resp, err := b.Client.Do(get)
	if err != nil {
		return FileContent{}, err
	}
	if resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return FileContent{}, fmt.Errorf("get status %d", resp.StatusCode)
	}
	return FileContent{resp.Body, size, media}, nil
}
