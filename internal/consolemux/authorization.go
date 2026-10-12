package consolemux

import (
	"context"
	"time"
)

type Agent struct {
	ID       string
	FullName string
	Owner    string
	Session  string
}
type Authorization struct {
	Principal string
	Expiry    time.Time
	Agents    map[string]Agent
}
type Authorizer interface {
	AuthorizeConsoleMux(context.Context, string, []string) (Authorization, error)
}
