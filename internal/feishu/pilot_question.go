package feishu

import (
	"context"
	"errors"
	"regexp"
)

var personalOpenID = regexp.MustCompile(`^ou_[A-Za-z0-9_-]{1,200}$`)
var questionRequestID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func IsPersonalOpenID(id string) bool { return personalOpenID.MatchString(id) }
func validateQuestionMessage(out OutMessage) error {
	if out.BotChannelID == "" || out.ToType != "personal" || out.ToName != "" || !IsPersonalOpenID(out.ToID) || !questionRequestID.MatchString(out.RequestID) {
		return errors.New("invalid private question recipient or stable request identity")
	}
	return nil
}

// Native Lark uses the provider UUID field; proxy/FCS retain their existing
// RequestID path. Unknown receipts still do not authorize a fresh send.
func SendPilotQuestion(ctx context.Context, gateway Gateway, out OutMessage) (string, error) {
	if err := validateQuestionMessage(out); err != nil {
		return "", err
	}
	if g, ok := gateway.(interface {
		SendQuestion(context.Context, OutMessage) (string, error)
	}); ok {
		return g.SendQuestion(ctx, out)
	}
	if gateway == nil {
		return "", ErrNotImplemented
	}
	return gateway.Send(ctx, out)
}
