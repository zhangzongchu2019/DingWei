package mcpgw

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestOAuthKeyPrivateFileOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	key := randomKey(t)
	if e := os.WriteFile(path, key, 0600); e != nil {
		t.Fatal(e)
	}
	got, e := ReadOAuthKeyFile(path)
	if e != nil || !bytes.Equal(got, key) {
		t.Fatal("private key read failed")
	}
	if e = os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadOAuthKeyFile(path); e == nil {
		t.Fatal("public key file accepted")
	}
	if e = os.Chmod(path, 0700); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadOAuthKeyFile(path); e == nil {
		t.Fatal("executable key file accepted")
	}
	if e = os.Chmod(path, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, key[:31], 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadOAuthKeyFile(path); e == nil {
		t.Fatal("short key accepted")
	}
	if _, e = ReadOAuthKeyFile(dir); e == nil {
		t.Fatal("directory accepted")
	}
}
