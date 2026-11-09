package consolemux

import (
	"context"
	"time"
)

type Agent struct {
	RootID         string
	NodeID         string
	LaneGeneration int64
	ID             string
	FullName       string
	Owner          string
	Session        string
}
type Authorization struct {
	Principal string
	Expiry    time.Time
	Agents    map[string]Agent
}
type Authorizer interface {
	AuthorizeConsoleMux(context.Context, string, []string) (Authorization, error)
}
