package fileservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
)

type fakeMinIO struct {
	bucket, key, body, mediaType string
	size, rangeStart, rangeEnd   int64
	disableMultipart             bool
	fail                         string
}

func (f *fakeMinIO) putObject(_ context.Context, bucket, key string, r io.Reader, size int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	if f.fail == "put" {
		return minio.UploadInfo{}, errors.New("put down")
	}
	b, err := io.ReadAll(r)
	f.bucket, f.key, f.body, f.size, f.mediaType, f.disableMultipart = bucket, key, string(b), size, opts.ContentType, opts.DisableMultipart
	return minio.UploadInfo{Size: int64(len(b))}, err
}
func (f *fakeMinIO) statObject(_ context.Context, bucket, key string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
	if f.fail == "stat" {
		return minio.ObjectInfo{}, errors.New("stat down")
	}
	f.bucket, f.key = bucket, key
	return minio.ObjectInfo{Size: f.size, ContentType: f.mediaType}, nil
}
func (f *fakeMinIO) getObject(_ context.Context, bucket, key string, opts minio.GetObjectOptions) (io.ReadCloser, minio.ObjectInfo, http.Header, error) {
	if f.fail == "get" {
		return nil, minio.ObjectInfo{}, nil, errors.New("get down")
	}
	f.bucket, f.key = bucket, key
	rangeHeader := opts.Header().Get("Range")
	_, err := fmtSscanfRange(rangeHeader, &f.rangeStart, &f.rangeEnd)
	if err != nil {
		return nil, minio.ObjectInfo{}, nil, err
	}
	return io.NopCloser(bytes.NewBufferString(f.body[f.rangeStart : f.rangeEnd+1])), minio.ObjectInfo{}, nil, nil
}
func (f *fakeMinIO) removeObject(_ context.Context, bucket, key string, _ minio.RemoveObjectOptions) error {
	if f.fail == "delete" {
		return errors.New("delete down")
	}
	f.bucket, f.key = bucket, key
	return nil
}

func fmtSscanfRange(raw string, start, end *int64) (int, error) {
	if len(raw) < len("bytes=0-0") {
		return 0, errors.New("missing range")
	}
	return fmt.Sscanf(raw, "bytes=%d-%d", start, end)
}

func TestMinIOObjectStoreMapsAtomicPutStatRangeAndDelete(t *testing.T) {
	fake := &fakeMinIO{}
	store := &MinIOObjectStore{client: fake, bucket: "files"}
	if err := store.Put(context.Background(), "objects/id", bytes.NewBufferString("abcdef"), 6, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if fake.bucket != "files" || fake.key != "objects/id" || fake.body != "abcdef" || !fake.disableMultipart {
		t.Fatalf("put mapping: %+v", fake)
	}
	info, err := store.Stat(context.Background(), "objects/id")
	if err != nil || info.Size != 6 || info.ContentType != "text/plain" {
		t.Fatalf("stat=%+v err=%v", info, err)
	}
	body, err := store.Open(context.Background(), "objects/id", 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(body)
	_ = body.Close()
	if string(got) != "cde" || fake.rangeStart != 2 || fake.rangeEnd != 4 {
		t.Fatalf("range=%d-%d body=%q", fake.rangeStart, fake.rangeEnd, got)
	}
	if err := store.Delete(context.Background(), "objects/id"); err != nil {
		t.Fatal(err)
	}
}

func TestMinIOObjectStorePropagatesEveryBackendFailure(t *testing.T) {
	for _, op := range []string{"put", "stat", "get", "delete"} {
		t.Run(op, func(t *testing.T) {
			fake := &fakeMinIO{body: "abc", size: 3, fail: op}
			store := &MinIOObjectStore{client: fake, bucket: "files"}
			var err error
			switch op {
			case "put":
				err = store.Put(context.Background(), "key", strings.NewReader("abc"), 3, "application/octet-stream")
			case "stat":
				_, err = store.Stat(context.Background(), "key")
			case "get":
				_, err = store.Open(context.Background(), "key", 0, 1)
			case "delete":
				err = store.Delete(context.Background(), "key")
			}
			if err == nil {
				t.Fatal("backend failure was swallowed")
			}
		})
	}
}
