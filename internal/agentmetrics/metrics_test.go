package agentmetrics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func valid() []byte {
	return []byte(`{"schema":"agent-disk/v1","status":"ok","producer":"helper","sample_instance":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","sample_seq":"1","sampled_at_ms":1000000,"elapsed_send_age_ms":0,"path_basis":"cli_workdir_filesystem","filesystem_instance":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","used_bytes":0,"total_bytes":100,"available_bytes":100}`)
}
func TestValidation(t *testing.T) {
	now := time.UnixMilli(1000000)
	t.Run("fresh_actual_zero", func(t *testing.T) {
		w, _, e := Parse(valid(), now)
		if e != nil || *w.Used != 0 {
			t.Fatal(e)
		}
	})
	for name, change := range map[string]func(map[string]any){
		"missing_age":        func(x map[string]any) { delete(x, "elapsed_send_age_ms") },
		"null_age":           func(x map[string]any) { x["elapsed_send_age_ms"] = nil },
		"uppercase_optional": func(x map[string]any) { x["USED_BYTES"] = x["used_bytes"]; delete(x, "used_bytes") },
		"forged_agent":       func(x map[string]any) { x["agent_id"] = "other" },
		"forged_principal":   func(x map[string]any) { x["principal"] = "owner" },
		"wrong_producer":     func(x map[string]any) { x["producer"] = "view-service" },
		"invalid_seq":        func(x map[string]any) { x["sample_seq"] = "01" },
		"future_time":        func(x map[string]any) { x["sampled_at_ms"] = 1030001 },
		"old_time":           func(x map[string]any) { x["sampled_at_ms"] = 969999 },
		"send_too_old":       func(x map[string]any) { x["elapsed_send_age_ms"] = 5001 },
		"fake_expired":       func(x map[string]any) { x["status"] = "expired" },
		"bool_zero":          func(x map[string]any) { x["used_bytes"] = false },
		"unsafe_total":       func(x map[string]any) { x["total_bytes"] = float64(1 << 53) },
		"over_total":         func(x map[string]any) { x["used_bytes"] = 101 },
		"missing_total":      func(x map[string]any) { delete(x, "total_bytes") },
	} {
		t.Run(name, func(t *testing.T) {
			var x map[string]any
			json.Unmarshal(valid(), &x)
			change(x)
			b, _ := json.Marshal(x)
			if _, _, e := Parse(b, now); e == nil {
				t.Fatal("accepted")
			}
		})
	}
	t.Run("oversize", func(t *testing.T) {
		if _, _, e := Parse([]byte(strings.Repeat("x", 2049)), now); e == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("duplicate_key", func(t *testing.T) {
		if _, _, e := Parse([]byte(strings.Replace(string(valid()), `"used_bytes":0`, `"used_bytes":1,"used_bytes":0`, 1)), now); e == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("trailing_json", func(t *testing.T) {
		if _, _, e := Parse(append(valid(), []byte(`{}`)...), now); e == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("unavailable_no_zero", func(t *testing.T) {
		var x map[string]any
		json.Unmarshal(valid(), &x)
		for _, k := range []string{"used_bytes", "total_bytes", "available_bytes", "filesystem_instance"} {
			delete(x, k)
		}
		x["status"] = "unavailable"
		x["reason"] = "workdir_unbound"
		b, _ := json.Marshal(x)
		w, _, e := Parse(b, now)
		if e != nil || w.Used != nil {
			t.Fatal(e)
		}
	})
}
func TestLifetimeAndBinding(t *testing.T) {
	now := time.UnixMilli(1000000)
	b := Binding{"agent-a", "conn-a", true}
	t.Run("duplicate_no_renew", func(t *testing.T) {
		c := New(b)
		if changed, e := c.Accept(valid(), b, now); e != nil || !changed {
			t.Fatal(e)
		}
		if changed, e := c.Accept(valid(), b, now.Add(time.Second)); e != nil || changed {
			t.Fatal(e)
		}
		s := c.Read(b, now.Add(359*time.Second))
		if s.ExpiresInMS != 1000 || s.ReceivedAt != now.UnixMilli() {
			t.Fatal(s)
		}
		s = c.Read(b, now.Add(Lifetime))
		if s.Status != "expired" || s.Sample.Total != nil {
			t.Fatal(s)
		}
	})
	t.Run("wrong_agent_or_generation", func(t *testing.T) {
		for _, wrong := range []Binding{{"agent-b", "conn-a", true}, {"agent-a", "conn-b", true}, {"agent-a", "conn-a", false}} {
			c := New(b)
			if _, e := c.Accept(valid(), wrong, now); e == nil {
				t.Fatal("accepted")
			}
			c.Accept(valid(), b, now)
			if s := c.Read(wrong, now); s.Status != "unavailable" || s.Sample.Total != nil {
				t.Fatal(s)
			}
		}
	})
	t.Run("new_instance_same_connection_rejected", func(t *testing.T) {
		c := New(b)
		c.Accept(valid(), b, now)
		raw := []byte(strings.Replace(string(valid()), strings.Repeat("a", 32), strings.Repeat("c", 32), 1))
		if _, e := c.Accept(raw, b, now); e == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("same_seq_changed_value_rejected", func(t *testing.T) {
		c := New(b)
		c.Accept(valid(), b, now)
		raw := []byte(strings.Replace(string(valid()), `"used_bytes":0`, `"used_bytes":1`, 1))
		if _, e := c.Accept(raw, b, now); e == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("not_reaged_by_read", func(t *testing.T) {
		c := New(b)
		c.Accept(valid(), b, now)
		a := c.Read(b, now.Add(time.Second))
		z := c.Read(b, now.Add(2*time.Second))
		if z.ExpiresInMS >= a.ExpiresInMS || z.ReceivedAt != a.ReceivedAt {
			t.Fatal(a, z)
		}
	})
	t.Run("backward_clock_unknown", func(t *testing.T) {
		c := New(b)
		c.Accept(valid(), b, now)
		if s := c.Read(b, now.Add(-time.Second)); s.Status != "unavailable" {
			t.Fatal(s)
		}
	})
	t.Run("snapshot_no_alias", func(t *testing.T) {
		c := New(b)
		c.Accept(valid(), b, now)
		s := c.Read(b, now)
		*s.Sample.Total = 9
		if *c.Read(b, now).Sample.Total != 100 {
			t.Fatal("alias")
		}
	})
}

func TestRateAndSendAgeBudget(t *testing.T) {
	now := time.UnixMilli(1000000)
	b := Binding{"agent", "connection", true}
	c := New(b)
	c.Accept(valid(), b, now)
	update := strings.Replace(string(valid()), `"sample_seq":"1"`, `"sample_seq":"2"`, 1)
	update = strings.Replace(update, `"sampled_at_ms":1000000`, `"sampled_at_ms":1001000`, 1)
	if _, err := c.Accept([]byte(update), b, now.Add(time.Second)); err == nil {
		t.Fatal("rate bound missing")
	}
	update = strings.Replace(update, `"sampled_at_ms":1001000`, `"sampled_at_ms":1025000`, 1)
	update = strings.Replace(update, `"elapsed_send_age_ms":0`, `"elapsed_send_age_ms":5000`, 1)
	if _, err := c.Accept([]byte(update), b, now.Add(25*time.Second-time.Millisecond)); err == nil {
		t.Fatal("sample accepted before the 25 second boundary")
	}
	if changed, err := c.Accept([]byte(update), b, now.Add(25*time.Second)); err != nil || !changed {
		t.Fatal(err)
	}
	if got := c.Read(b, now.Add(25*time.Second)); got.ExpiresInMS != 355000 || got.AgeMS != 5000 {
		t.Fatal(got)
	}
	if _, _, err := Parse([]byte(strings.Replace(string(valid()), `"filesystem_instance":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`, `"filesystem_instance":null`, 1)), now); err == nil {
		t.Fatal("null optional field accepted")
	}
}
