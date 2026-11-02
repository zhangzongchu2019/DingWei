package m8

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// 包装器在 New 之前安装，测试期间不替换共享 Repo 指针。
type helperOfferCASRepository struct {
	store.Repository
	beforeCAS func(context.Context, string, bool, string, string) (bool, error)
}

func (s *helperOfferCASRepository) CompareAndSwapAppConfig(ctx context.Context, key string, exists bool, previous, next string) (bool, error) {
	if s.beforeCAS != nil {
		return s.beforeCAS(ctx, key, exists, previous, next)
	}
	return s.Repository.CompareAndSwapAppConfig(ctx, key, exists, previous, next)
}

func newHelperOfferRaceHub(t *testing.T, wrap func(store.Repository) store.Repository) (*Hub, context.Context, *helperUpdateJob) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "offer.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var repo store.Repository = db
	if wrap != nil {
		repo = wrap(repo)
	}
	h := New(repo, ConsoleAuthDisabled())
	t.Cleanup(h.Close)
	digest := strings.Repeat("a", 64)
	job, err := h.createHelperUpdateJob(ctx, HelperUpdateCatalog{
		SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", Platform: "linux",
		URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example",
	}, "offer-agent", "zzc", "agent", "zzc")
	if err != nil {
		t.Fatal(err)
	}
	return h, ctx, job
}

func helperOfferReceipt(h *Hub, ctx context.Context, job *helperUpdateJob, stage string) {
	h.recordHelperUpdateReceipt(ctx, &sessionClient{metricsAgentID: job.AgentID, connSeq: 20}, model.Envelope{Meta: map[string]any{
		"type": "provision_ack", "action": "update_self", "stage": stage,
		"job_id": job.JobID, "release_id": job.ReleaseID, "sha256": job.SHA256, "lock_sha256": job.LockSHA256,
	}})
}

func TestHelperUpdateOfferConcurrentFirstCASIsIdempotent(t *testing.T) {
	for _, receipt := range []string{"", "installed"} {
		t.Run("receipt_"+receipt, func(t *testing.T) {
			entered := make(chan int, 2)
			gates := map[int]chan struct{}{10: make(chan struct{}), 20: make(chan struct{})}
			release := func(seq int) {
				select {
				case <-gates[seq]:
				default:
					close(gates[seq])
				}
			}
			h, ctx, snapshot := newHelperOfferRaceHub(t, func(repo store.Repository) store.Repository {
				return &helperOfferCASRepository{Repository: repo, beforeCAS: func(ctx context.Context, key string, exists bool, previous, next string) (bool, error) {
					var oldJob, nextJob helperUpdateJob
					if exists && key == helperUpdateJobKey("offer-agent") {
						if err := json.Unmarshal([]byte(previous), &oldJob); err != nil {
							return false, err
						}
						if err := json.Unmarshal([]byte(next), &nextJob); err != nil {
							return false, err
						}
						if oldJob.OfferConnSeq == 0 && nextJob.OfferConnSeq > 0 {
							entered <- nextJob.OfferConnSeq
							<-gates[nextJob.OfferConnSeq]
						}
					}
					return repo.CompareAndSwapAppConfig(ctx, key, exists, previous, next)
				}}
			})
			type result struct {
				seq int
				job *helperUpdateJob
				err error
			}
			results := make(chan result, 2)
			var workers sync.WaitGroup
			t.Cleanup(func() { release(10); release(20); workers.Wait() })
			for _, seq := range []int{10, 20} {
				workers.Add(1)
				go func(seq int) {
					defer workers.Done()
					job, err := h.recordHelperUpdateOfferConnection(ctx, &sessionClient{connSeq: seq}, snapshot)
					results <- result{seq, job, err}
				}(seq)
			}
			seen := map[int]bool{}
			for len(seen) < 2 {
				select {
				case seq := <-entered:
					seen[seq] = true
				case <-time.After(5 * time.Second):
					t.Fatal("CAS_BARRIER_NOT_REACHED")
				}
			}
			release(20)
			winner := <-results
			if winner.seq != 20 || winner.err != nil {
				t.Fatalf("first offer: %+v", winner)
			}
			if receipt != "" {
				helperOfferReceipt(h, ctx, snapshot, receipt)
			}
			release(10)
			loser := <-results
			if loser.err != nil || loser.job == nil || loser.job.OfferConnSeq != 20 {
				t.Fatalf("OFFER_CAS_LOSER_NOT_IDEMPOTENT seq=%d job=%+v err=%v", loser.seq, loser.job, loser.err)
			}
			stored, _, err := h.helperUpdateJob(ctx, snapshot.AgentID)
			wantStage, wantRevision := "offered", 2
			if receipt != "" {
				wantStage, wantRevision = receipt, 3
			}
			if err != nil || stored == nil || stored.OfferConnSeq != 20 || stored.Revision != wantRevision || stored.Stage != wantStage || loser.job.Stage != wantStage {
				t.Fatalf("OFFER_WINNER_STATE_CHANGED stored=%+v loser=%+v err=%v", stored, loser.job, err)
			}
			t.Logf("OFFER_CAS winner=20 loser=10 first_boundary=20 stage=%s revision=%d", stored.Stage, stored.Revision)
		})
	}
}

func TestHelperUpdateOfferStaleSnapshotPreservesBoundaryAndRevision(t *testing.T) {
	for _, stage := range []string{"", "downloaded", "restarted"} {
		t.Run("stage_"+stage, func(t *testing.T) {
			h, ctx, snapshot := newHelperOfferRaceHub(t, nil)
			if _, err := h.recordHelperUpdateOfferConnection(ctx, &sessionClient{connSeq: 20}, snapshot); err != nil {
				t.Fatal(err)
			}
			if stage != "" {
				helperOfferReceipt(h, ctx, snapshot, stage)
			}
			before, _, err := h.helperUpdateJob(ctx, snapshot.AgentID)
			if err != nil {
				t.Fatal(err)
			}
			for _, lateSeq := range []int{20, 10} {
				got, err := h.recordHelperUpdateOfferConnection(ctx, &sessionClient{connSeq: lateSeq}, snapshot)
				after, _, readErr := h.helperUpdateJob(ctx, snapshot.AgentID)
				if err != nil || readErr != nil || got == nil || after == nil || after.OfferConnSeq != 20 || after.Revision != before.Revision || after.Stage != before.Stage || got.OfferConnSeq != 20 {
					t.Fatalf("OFFER_FROZEN_REWRITTEN late=%d before=%+v after=%+v got=%+v err=%v readErr=%v", lateSeq, before, after, got, err, readErr)
				}
			}
			t.Logf("OFFER_STALE boundary=20 stage=%s revision=%d unchanged=true", before.Stage, before.Revision)
		})
	}
}

func TestHelperUpdateOfferCASLossRejectsChangedJobOrUnfrozenState(t *testing.T) {
	for _, change := range []string{"new_job", "terminal", "unfrozen"} {
		t.Run(change, func(t *testing.T) {
			var expected string
			h, ctx, snapshot := newHelperOfferRaceHub(t, func(repo store.Repository) store.Repository {
				return &helperOfferCASRepository{Repository: repo, beforeCAS: func(ctx context.Context, key string, exists bool, previous, next string) (bool, error) {
					if !exists || key != helperUpdateJobKey("offer-agent") {
						return repo.CompareAndSwapAppConfig(ctx, key, exists, previous, next)
					}
					var current helperUpdateJob
					if err := json.Unmarshal([]byte(previous), &current); err != nil {
						return false, err
					}
					current.OfferConnSeq, current.Revision = 30, current.Revision+1
					switch change {
					case "new_job":
						current.JobID, current.OfferID = "replacement-job", "replacement-job"
					case "terminal":
						current.Stage = "confirmed"
					case "unfrozen":
						current.OfferConnSeq = 0
					}
					var err error
					expected, err = marshalHelperUpdate(current)
					if err != nil {
						return false, err
					}
					return false, repo.UpsertAppConfig(ctx, key, expected)
				}}
			})
			if _, err := h.recordHelperUpdateOfferConnection(ctx, &sessionClient{connSeq: 20}, snapshot); err == nil {
				t.Fatal("OFFER_CAS_LOSS_WRONGLY_ACCEPTED")
			}
			_, raw, err := h.helperUpdateJob(ctx, snapshot.AgentID)
			if err != nil || raw != expected {
				t.Fatalf("OFFER_NEW_STATE_OVERWRITTEN raw=%s err=%v", raw, err)
			}
		})
	}
}
