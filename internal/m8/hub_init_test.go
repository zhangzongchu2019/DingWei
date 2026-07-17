package m8

import (
	"reflect"
	"testing"
)

// Any map added to Hub must be initialized by New. A nil map can survive
// reads and then panic on the first production write while h.mu is held.
func TestNewInitializesEveryHubMap(t *testing.T) {
	hub := New(nil)
	v := reflect.ValueOf(hub).Elem()
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() != reflect.Map {
			continue
		}
		if v.Field(i).IsNil() {
			t.Errorf("Hub.%s is a nil map after New", typ.Field(i).Name)
		}
	}
}
