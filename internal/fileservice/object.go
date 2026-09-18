package fileservice

import (
	"context"
	"io"
)

type ObjectInfo struct {
	Size        int64
	ContentType string
}

type ObjectStore interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Stat(context.Context, string) (ObjectInfo, error)
	Open(context.Context, string, int64, int64) (io.ReadCloser, error)
	Delete(context.Context, string) error
}
