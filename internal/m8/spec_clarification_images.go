package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/zhangzongchu2019/dingwei/internal/clarificationimage"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"io"
	"net/http"
	"time"
)

type clarificationImageRepository interface {
	RegisterClarificationImage(context.Context, store.QuestionContext, store.BlockerActor, string, clarificationimage.File, func() error, time.Time) error
	ReadClarificationImageMetadata(context.Context, store.QuestionContext, store.BlockerActor, string, string, func() error) (clarificationimage.File, error)
}

func (h *Hub) HandleClarificationImage(w http.ResponseWriter, r *http.Request) {
	repo, e := h.clarificationRepo()
	if e != nil {
		taskError(w, e)
		return
	}
	images, ok := h.Repo.(clarificationImageRepository)
	if !ok {
		taskJSON(w, 503, map[string]any{"error": "private_images_pending_config"})
		return
	}
	client, ok := h.fileIssuer.(clarificationImageClient)
	if !ok {
		taskJSON(w, 503, map[string]any{"error": "private_images_pending_config"})
		return
	}
	actor, e := questionCredential(r)
	if e != nil {
		taskError(w, e)
		return
	}
	if r.URL.RawQuery != "" {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	qid := r.PathValue("questionID")
	id := r.PathValue("fileID")
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c, e := repo.QuestionAuthorizationContext(ctx, "", "", qid)
	if e != nil {
		taskError(w, e)
		return
	}
	p, e := repo.ClarificationPolicy(ctx, c.SpecID)
	if e != nil {
		taskError(w, e)
		return
	}
	var data []byte
	var f clarificationimage.File
	if r.Method == "POST" {
		if !h.SpecClarificationEnabled {
			taskJSON(w, 503, map[string]any{"error": "clarification_disabled"})
			return
		}
		if id != "" {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		data, e = io.ReadAll(http.MaxBytesReader(w, r.Body, clarificationimage.MaxBytes+1))
		if e != nil || len(data) > clarificationimage.MaxBytes {
			taskJSON(w, 413, map[string]any{"error": "image_too_large"})
			return
		}
		if _, e = clarificationimage.Validate(data); e != nil {
			taskJSON(w, 415, map[string]any{"error": "image_type_rejected"})
			return
		}
	} else if r.Method != "GET" || !clarificationLoginToken(id) {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	e = h.withClarificationIdentity(ctx, repo, c, p, actor, nil, func(_ identity.ContextAuthorization, check func() error) error {
		if r.Method == "POST" {
			// Verify object access before transferring any bytes to the private file
			// service. Completion is registered only after a valid file-service receipt.
			if _, err := repo.ReadClarificationThread(ctx, c, actor.actor, qid, 0, 1, check); err != nil {
				return err
			}
			var err error
			f, err = client.PutClarificationImage(ctx, data)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			if f.SHA != hex.EncodeToString(sum[:]) {
				return store.ErrTaskFlowConflict
			}
			return images.RegisterClarificationImage(ctx, c, actor.actor, qid, f, check, h.now())
		}
		var err error
		f, err = images.ReadClarificationImageMetadata(ctx, c, actor.actor, qid, id, check)
		if err != nil {
			return err
		}
		var media string
		data, media, err = client.ReadClarificationImage(ctx, id)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if media != f.Media || int64(len(data)) != f.Size || hex.EncodeToString(sum[:]) != f.SHA {
			return store.ErrTaskFlowConflict
		}
		return check()
	})
	if e != nil {
		taskError(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method == "POST" {
		taskJSON(w, 200, map[string]any{"image": f, "execution_authorized": false})
		return
	}
	w.Header().Set("Content-Type", f.Media)
	w.Header().Set("Content-Disposition", "inline; filename=clarification-image")
	w.Write(data)
}
