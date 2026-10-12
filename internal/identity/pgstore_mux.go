package identity

import "github.com/zhangzongchu2019/dingwei/internal/consolemux"

// ConsoleMuxSlots reuses the identity store's platform access context. Slot SQL
// still explicitly scopes every statement to principal and canonical origin.
func (s *PGStore) ConsoleMuxSlots(b *consolemux.Budget) consolemux.Slots {
	return consolemux.NewPGSlots(s.pool, s.t, b)
}
