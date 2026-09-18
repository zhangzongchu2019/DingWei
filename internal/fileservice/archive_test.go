package fileservice

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestArchiveRequiresMatchingOwnerScopeAndWritesAudit(t *testing.T) {
	m, _, _ := testManager(t, 100)
	f, err := m.Put(context.Background(), Upload{OwnerKey: "tenant-a", SessionName: "agent", OriginalName: "x", ContentLength: 1, Body: strings.NewReader("x")})
	if err != nil {
		t.Fatal(err)
	}
	h := ArchiveHandler{Manager: m, Token: "issuer", Clock: m.Clock}
	call := func(owner string) int {
		body := bytes.NewBufferString(`{"file_id":"` + f.ID + `","actor_principal":"owner-principal","owner_key":"` + owner + `","reason":"keep for case","archived":true}`)
		r := httptest.NewRequest(http.MethodPost, "/internal/files/archive", body)
		r.Header.Set("Authorization", "Bearer issuer")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := call("tenant-b"); code != http.StatusNotFound {
		t.Fatalf("cross-owner archive code=%d", code)
	}
	if code := call("tenant-a"); code != http.StatusNoContent {
		t.Fatalf("own archive code=%d", code)
	}
	got, err := m.Repo.Get(context.Background(), f.ID)
	if err != nil || !got.Archived || got.ArchivedAt == nil {
		t.Fatalf("file=%+v err=%v", got, err)
	}
	if !strings.Contains(got.ObjectKey, "/pinned/") {
		t.Fatalf("archived object was not moved out of dated directory: %s", got.ObjectKey)
	}
	var actor, owner, reason string
	var archived int
	if err := m.Repo.db.QueryRow(`SELECT actor_principal,owner_key,archived,reason FROM file_archive_audit WHERE file_id=?`, f.ID).Scan(&actor, &owner, &archived, &reason); err != nil {
		t.Fatal(err)
	}
	if actor != "owner-principal" || owner != "tenant-a" || archived != 1 || reason != "keep for case" {
		t.Fatalf("audit actor=%q owner=%q archived=%d reason=%q", actor, owner, archived, reason)
	}
}
