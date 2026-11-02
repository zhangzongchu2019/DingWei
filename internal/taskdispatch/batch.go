package taskdispatch

import "context"

type ItemResult struct {
	OperationID string  `json:"operation_id"`
	Key         Key     `json:"key"`
	Result      *Result `json:"result,omitempty"`
	Error       string  `json:"error,omitempty"`
}
type BatchResult struct {
	Items     []ItemResult `json:"items"`
	Succeeded int          `json:"succeeded"`
	Failed    int          `json:"failed"`
	Partial   bool         `json:"partial"`
}

// ExecuteBatch is deliberately a sequence of independent single-repository
// decisions. It neither holds a cross-repository transaction nor hides failure.
func (s *Service) ExecuteBatch(ctx context.Context, actor Actor, requests []Request) (BatchResult, error) {
	out := BatchResult{Items: []ItemResult{}}
	if !s.ready() {
		return out, ErrUnavailable
	}
	if len(requests) < 1 || len(requests) > 50 {
		return out, ErrInvalid
	}
	seen := map[string]bool{}
	for _, r := range requests {
		if !ValidOperation(r.OperationID) || seen[r.OperationID] {
			return out, ErrInvalid
		}
		seen[r.OperationID] = true
	}
	for _, r := range requests {
		result, e := s.Execute(ctx, actor, r)
		item := ItemResult{OperationID: r.OperationID, Key: r.Key}
		if e != nil {
			item.Error = errClass(e)
			out.Failed++
		} else {
			item.Result = &result
			out.Succeeded++
		}
		out.Items = append(out.Items, item)
	}
	out.Partial = out.Succeeded > 0 && out.Failed > 0
	return out, nil
}
func (s *Service) RecoverNotices(ctx context.Context) error {
	if !s.ready() {
		return ErrUnavailable
	}
	for {
		pending, e := s.Repo.TaskDispatchNotices(ctx, "attempting", 1000)
		if e != nil {
			return e
		}
		if len(pending) == 0 {
			return nil
		}
		for _, n := range pending {
			if _, e = s.Repo.UpdateTaskDispatchNotice(ctx, n.ID, "attempting", "unknown", "", "hub_restarted_route_outcome_unknown", s.Now()); e != nil {
				return e
			}
		}
	}
}
