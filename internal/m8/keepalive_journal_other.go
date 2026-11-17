//go:build !linux

package m8

// Platforms without the nonblocking Linux journal-pipe primitive expose a
// diagnostic drop rather than blocking a protocol owner on an unknown sink.
func writeKeepaliveJournal([]byte) bool { return false }
