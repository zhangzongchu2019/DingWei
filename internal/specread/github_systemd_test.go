package specread

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// All bytes are synthetic; no host/service credential directory is read.
func TestGitHubSystemdCredentialBoundary(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"directory_0440", true}, {"outside_directory_0440", false}, {"other_read_0444", false}, {"symlink", false}, {"parent_other_write", false}, {"environment_elsewhere", false},
		{"parent_group_write", false}, {"unset_environment", false}, {"relative_environment", false}, {"noncanonical_environment", false}, {"clean_file_parent", true}, {"outside_private_0600", true}, {"directory_0600", true}, {"group_mask_bits", true}, {"parent_symlink", false}, {"not_regular", false},
		{"short_content", false}, {"embedded_whitespace", false}, {"oversized_content", false}, {"single_newline", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "credentials")
			if e := os.Mkdir(directory, 0700); e != nil {
				t.Fatal(e)
			}
			t.Setenv("CREDENTIALS_DIRECTORY", directory)
			file := filepath.Join(directory, "pat")
			content := strings.Repeat("x", 93)
			mode := os.FileMode(0440)
			switch tc.name {
			case "outside_directory_0440", "outside_private_0600":
				file = filepath.Join(root, "outside")
			case "environment_elsewhere":
				elsewhere := filepath.Join(root, "elsewhere")
				if e := os.Mkdir(elsewhere, 0700); e != nil {
					t.Fatal(e)
				}
				t.Setenv("CREDENTIALS_DIRECTORY", elsewhere)
			case "unset_environment":
				t.Setenv("CREDENTIALS_DIRECTORY", "")
			case "relative_environment":
				t.Setenv("CREDENTIALS_DIRECTORY", "credentials")
			case "noncanonical_environment":
				t.Setenv("CREDENTIALS_DIRECTORY", directory+"/")
			case "clean_file_parent":
				if e := os.Mkdir(filepath.Join(directory, "child"), 0700); e != nil {
					t.Fatal(e)
				}
				file = directory + "/child/../pat"
			case "short_content":
				content = "short"
			case "embedded_whitespace":
				content = "synthetic invalid credential"
			case "oversized_content":
				content = strings.Repeat("x", 4097)
			case "single_newline":
				content += "\n"
			}
			switch tc.name {
			case "outside_private_0600", "directory_0600":
				mode = 0600
			case "other_read_0444":
				mode = 0444
			case "group_mask_bits":
				mode = 0670
			}
			if e := os.WriteFile(file, []byte(content), 0600); e != nil {
				t.Fatal(e)
			}
			if e := os.Chmod(file, mode); e != nil {
				t.Fatal(e)
			}
			if tc.name == "directory_0440" {
				if e := os.Chmod(directory, 0550); e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { _ = os.Chmod(directory, 0700) })
			}
			switch tc.name {
			case "symlink":
				target := filepath.Join(directory, "target")
				if e := os.Rename(file, target); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(target, file); e != nil {
					t.Fatal(e)
				}
			case "parent_other_write", "parent_group_write":
				parentMode := os.FileMode(0702)
				if tc.name == "parent_group_write" {
					parentMode = 0720
				}
				if e := os.Chmod(directory, parentMode); e != nil {
					t.Fatal(e)
				}
			case "parent_symlink":
				actual := filepath.Join(root, "actual")
				if e := os.Rename(directory, actual); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(actual, directory); e != nil {
					t.Fatal(e)
				}
			case "not_regular":
				if e := os.Remove(file); e != nil {
					t.Fatal(e)
				}
				if e := os.Mkdir(file, 0700); e != nil {
					t.Fatal(e)
				}
			}
			calls := 0
			g := NewGitHub(file)
			g.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return reply(r, 200, map[string]any{"id": 1, "full_name": "test/repo"}), nil
			})
			value, e := g.credential()
			if (e == nil) != tc.want {
				t.Fatalf("R5_CRED_%s accepted=%t want=%t", tc.name, e == nil, tc.want)
			}
			if tc.want && value != strings.TrimSuffix(content, "\n") {
				t.Fatalf("R5_CRED_%s content changed", tc.name)
			}
			if !tc.want {
				if value != "" || e != ErrUnavailable || g.Ready() {
					t.Fatalf("R5_CRED_%s failed open", tc.name)
				}
				if _, e = g.Repository(context.Background(), "1"); e != ErrUnavailable || calls != 0 {
					t.Fatalf("R5_CRED_%s rejected bytes reached provider", tc.name)
				}
			}
		})
	}
}
func TestGitHubSystemdCredentialRotationStillRechecks(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("CREDENTIALS_DIRECTORY", directory)
	file := filepath.Join(directory, "pat")
	g := NewGitHub(file)
	write := func(mode os.FileMode) {
		t.Helper()
		if e := os.Remove(file); e != nil && !os.IsNotExist(e) {
			t.Fatal(e)
		}
		if e := os.WriteFile(file, []byte(strings.Repeat("x", 93)), 0600); e != nil {
			t.Fatal(e)
		}
		if e := os.Chmod(file, mode); e != nil {
			t.Fatal(e)
		}
	}
	write(0440)
	if !g.Ready() {
		t.Fatal("R5_CRED_ROTATION_INITIAL")
	}
	if e := os.Chmod(file, 0444); e != nil {
		t.Fatal(e)
	}
	if g.Ready() {
		t.Fatal("R5_CRED_ROTATION_MODE_CACHED")
	}
	write(0440)
	if !g.Ready() {
		t.Fatal("R5_CRED_ROTATION_RESTORED")
	}
	if e := os.Remove(file); e != nil {
		t.Fatal(e)
	}
	if g.Ready() {
		t.Fatal("R5_CRED_ROTATION_REMOVAL_CACHED")
	}
	t.Log("synthetic file93B; euid-owned regular file; no actual credential read")
}
