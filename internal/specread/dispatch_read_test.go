package specread

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestR10GitHubPullRequestIsReadOnlyAndSameStableRepository(t *testing.T) {
	for _, name := range []string{"valid", "foreign_repo", "wrong_number", "short_head", "missing_ref", "missing_updated", "forbidden", "outage"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			g := testGitHub(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Host != "api.github.com" || r.URL.Path != "/repos/owner/repository/pulls/7" || r.URL.RawQuery != "" {
					t.Fatal("R10_PR_PROVIDER_WRITE_OR_RAW_URL", r.Method, r.URL.Path)
				}
				repo := 1
				num := 7
				sha := strings.Repeat("a", 40)
				ref := "main"
				updated := "2026-11-02T12:00:00Z"
				status := 200
				switch name {
				case "foreign_repo":
					repo = 2
				case "wrong_number":
					num = 8
				case "short_head":
					sha = "aaaa"
				case "missing_ref":
					ref = ""
				case "missing_updated":
					updated = "0001-01-01T00:00:00Z"
				case "forbidden":
					status = 403
				case "outage":
					status = 503
				}
				return reply(r, status, map[string]any{"number": num, "state": "open", "updated_at": updated, "head": map[string]any{"sha": sha, "ref": "topic"}, "base": map[string]any{"ref": ref, "repo": map[string]any{"id": repo}}}), nil
			})
			pr, err := g.PullRequest(context.Background(), Repository{ID: "1", FullName: "owner/repository"}, 7)
			if name == "valid" {
				if err != nil || pr.Repository != "1" || pr.HeadSHA != strings.Repeat("a", 40) {
					t.Fatal("R10_VALID_PR_REJECTED", pr, err)
				}
			} else if err == nil {
				t.Fatal("R10_UNSAFE_PR_ACCEPTED", name)
			}
			if calls != 1 {
				t.Fatal("R10_PR_UNEXPECTED_REQUEST_COUNT", calls)
			}
		})
	}
}
