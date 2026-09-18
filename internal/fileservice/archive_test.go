package fileservice

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestArchiveRequiresMatchingOwnerScopeAndWritesAudit(t *testing.T) {
	m, clock, _ := testManager(t, 100)
	f, err := m.Put(context.Background(), Upload{OwnerKey: "tenant-a", SessionName: "agent", OriginalName: "x", ContentLength: 1, Body: strings.NewReader("x")})
	if err != nil {
		t.Fatal(err)
	}
	h := ArchiveHandler{Manager: m, Token: "issuer", Clock: m.Clock}
	call := func(owner string, archived bool) int {
		body := bytes.NewBufferString(`{"file_id":"` + f.ID + `","actor_principal":"owner-principal","owner_key":"` + owner + `","reason":"keep for case","archived":` + fmt.Sprint(archived) + `}`)
		r := httptest.NewRequest(http.MethodPost, "/internal/files/archive", body)
		r.Header.Set("Authorization", "Bearer issuer")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := call("tenant-b", true); code != http.StatusNotFound {
		t.Fatalf("cross-owner archive code=%d", code)
	}
	if code := call("tenant-a", true); code != http.StatusNoContent {
		t.Fatalf("own archive code=%d", code)
	}
	got, err := m.Repo.Get(context.Background(), f.ID)
	if err != nil || !got.Archived || got.ArchivedAt == nil {
		t.Fatalf("file=%+v err=%v", got, err)
	}
	if got.ObjectKey != f.ObjectKey {
		t.Fatalf("archive changed stable object key: before=%s after=%s", f.ObjectKey, got.ObjectKey)
	}
	if !got.ExpiresAt.Equal(f.ExpiresAt) {
		t.Fatalf("archive changed expires_at: before=%s after=%s", f.ExpiresAt, got.ExpiresAt)
	}
	var actor, owner, reason string
	var archived int
	if err := m.Repo.db.QueryRow(`SELECT actor_principal,owner_key,archived,reason FROM file_archive_audit WHERE file_id=?`, f.ID).Scan(&actor, &owner, &archived, &reason); err != nil {
		t.Fatal(err)
	}
	if actor != "owner-principal" || owner != "tenant-a" || archived != 1 || reason != "keep for case" {
		t.Fatalf("audit actor=%q owner=%q archived=%d reason=%q", actor, owner, archived, reason)
	}
	clock.Time = clock.Time.Add(30 * 24 * time.Hour)
	if code := call("tenant-a", false); code != http.StatusNoContent {
		t.Fatalf("unarchive code=%d", code)
	}
	unarchived, err := m.Repo.Get(context.Background(), f.ID)
	if err != nil || unarchived.Archived || unarchived.UnarchivedAt == nil || !unarchived.ExpiresAt.Equal(clock.Time.Add(m.Retention)) || unarchived.ObjectKey != f.ObjectKey {
		t.Fatalf("unarchived=%+v err=%v", unarchived, err)
	}
}
