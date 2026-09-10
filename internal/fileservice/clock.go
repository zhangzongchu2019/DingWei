package fileservice

import "time"

// Clock is mandatory service input so expiry and sweeps remain testable without
// changing the host clock (which would also distort MinIO lifecycle behavior).
type Clock interface{ Now() time.Time }

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

type FixedClock struct{ Time time.Time }

func (c *FixedClock) Now() time.Time { return c.Time.UTC() }
