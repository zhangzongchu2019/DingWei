package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/clarificationimage"
	"io"
	"net/http"
	"net/url"
)

type clarificationImageClient interface {
	PutClarificationImage(context.Context, []byte) (clarificationimage.File, error)
	ReadClarificationImage(context.Context, string) ([]byte, string, error)
}

func (i *HTTPFileTicketIssuer) clarificationImageRequest(ctx context.Context, method, id string, body []byte) (*http.Response, error) {
	if i == nil || i.Client == nil || i.Credential == "" {
		return nil, errors.New("private image service unavailable")
	}
	path := i.InternalBaseURL + "/internal/files/clarification-images"
	if method == "GET" {
		path += "?file_id=" + url.QueryEscape(id)
	}
	req, e := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(body))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+i.Credential)
	req.Header.Set("Content-Type", "application/octet-stream")
	client := *i.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client.Do(req)
}
func (i *HTTPFileTicketIssuer) PutClarificationImage(ctx context.Context, b []byte) (clarificationimage.File, error) {
	var f clarificationimage.File
	media, e := clarificationimage.Validate(b)
	if e != nil {
		return f, e
	}
	resp, e := i.clarificationImageRequest(ctx, "POST", "", b)
	if e != nil {
		return f, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return f, errors.New("private image upload failed")
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&f); e != nil || !clarificationLoginToken(f.ID) || !clarificationLoginToken(f.SHA) || f.Size != int64(len(b)) || f.Media != media {
		return clarificationimage.File{}, errors.New("invalid private image receipt")
	}
	return f, nil
}
func (i *HTTPFileTicketIssuer) ReadClarificationImage(ctx context.Context, id string) ([]byte, string, error) {
	if !clarificationLoginToken(id) {
		return nil, "", errors.New("invalid private image identity")
	}
	resp, e := i.clarificationImageRequest(ctx, "GET", id, nil)
	if e != nil {
		return nil, "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", errors.New("private image retrieval failed")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, clarificationimage.MaxBytes+1))
	if e != nil {
		return nil, "", e
	}
	media, e := clarificationimage.Validate(b)
	if e != nil || resp.Header.Get("Content-Type") != media {
		return nil, "", errors.New("private image integrity failed")
	}
	return b, media, nil
}
