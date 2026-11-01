//go:build !linux

package store

import (
	"context"
	"errors"
)

func lockLegacySpecFile(ctx context.Context, _ string, exclusive bool) (func(), error) {
	// Preserve default-off legacy behavior on other platforms; they cannot turn
	// on a cutover without the cross-process lock supported by the Linux Hub.
	if exclusive {
		return nil, errors.New("legacy spec cutover requires Linux")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return func() {}, nil
}
