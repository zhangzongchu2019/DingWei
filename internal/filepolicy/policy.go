package filepolicy

import "time"

const (
	MaxUploadBytes   int64         = 50 << 20
	DefaultRetention time.Duration = 7 * 24 * time.Hour
)
