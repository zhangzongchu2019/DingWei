package main

import (
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"io"
	"net/http"
)

func registerHostedNodeAdmin(mux hostedRouteMux, d hostedDeps) {
	for _, route := range []struct {
		pattern string
		handler func(http.ResponseWriter, *http.Request, string)
	}{
		{"GET /api/console/admin/hosted/nodes", d.handleAdminNodeList},
		{"POST /api/console/admin/hosted/nodes", d.handleAdminNodeCreate},
		{"PATCH /api/console/admin/hosted/nodes/{id}", d.handleAdminNodeUpdate},
	} {
		next := route.handler
		mux.HandleFunc(route.pattern, func(w http.ResponseWriter, r *http.Request) {
			if d.actor == nil || len(r.Header.Values("Authorization")) != 0 {
				hostedForbidden(w)
				return
			}
			actor, err := d.actor(r)
			if err != nil || d.isAdmin == nil || !d.isAdmin(r, actor) {
				hostedForbidden(w)
				return
			}
			if r.Method != "GET" && (d.publicOrigin == "" || r.Header.Get("Origin") != d.publicOrigin) {
				hostedForbidden(w)
				return
			}
			if d.store == nil {
				writeJSON(w, 503, map[string]string{"error": "hosted_unavailable"})
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			next(w, r, actor)
		})
	}
}
func nodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSON(w, 400, map[string]string{"error": "params_invalid"})
		return false
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		writeJSON(w, 400, map[string]string{"error": "params_invalid"})
		return false
	}
	return true
}
func (d hostedDeps) handleNodeList(w http.ResponseWriter, r *http.Request) {
	user, err := d.actor(r)
	if err != nil {
		hostedForbidden(w)
		return
	}
	v, err := d.orch.Options(r.Context(), user)
	if err != nil {
		writeHostedError(w, err)
		return
	}
	if d.isAdmin != nil && d.isAdmin(r, user) && d.store != nil {
		for i := range v.Nodes {
			n, e := d.store.GetNode(r.Context(), v.Nodes[i].ID)
			if e != nil {
				writeHostedError(w, e)
				return
			}
			v.Nodes[i].Address = n.Address
			v.Nodes[i].SchedulingState = n.SchedulingState
		}
	}
	writeJSON(w, 200, map[string]any{"nodes": v.Nodes})
}
func (d hostedDeps) handleAdminNodeList(w http.ResponseWriter, r *http.Request, actor string) {
	nodes, err := d.store.ListNodes(r.Context())
	if err != nil {
		writeHostedError(w, err)
		return
	}
	out := make([]any, 0, len(nodes))
	for _, n := range nodes {
		v, e := d.store.GetNodeRegistration(r.Context(), n.ID, actor)
		if e != nil {
			writeHostedError(w, e)
			return
		}
		out = append(out, map[string]any{"node": v, "online": n.State == "online"})
	}
	writeJSON(w, 200, map[string]any{"nodes": out})
}
func (d hostedDeps) handleAdminNodeCreate(w http.ResponseWriter, r *http.Request, actor string) {
	var body hosted.NodeRegistration
	if !nodeBody(w, r, &body) {
		return
	}
	secret, err := d.store.RegisterManagedNode(r.Context(), body, actor)
	if errors.Is(err, hosted.ErrNodeExists) {
		writeJSON(w, 409, map[string]string{"error": "node_exists"})
		return
	}
	if err != nil {
		writeHostedError(w, err)
		return
	}
	writeJSON(w, 201, map[string]string{"node_id": body.ID, "credential": secret})
}
func (d hostedDeps) handleAdminNodeUpdate(w http.ResponseWriter, r *http.Request, actor string) {
	id := r.PathValue("id")
	body, err := d.store.GetNodeRegistration(r.Context(), id, actor)
	if err != nil {
		writeHostedError(w, err)
		return
	}
	if !nodeBody(w, r, &body) {
		return
	}
	if body.ID != id {
		writeJSON(w, 400, map[string]string{"error": "params_invalid"})
		return
	}
	if err = d.store.UpdateManagedNode(r.Context(), id, actor, body); err != nil {
		writeHostedError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"node_id": id})
}
