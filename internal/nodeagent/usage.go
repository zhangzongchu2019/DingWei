package nodeagent

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// diskUsage walks metadata only. Directory handles pin each ancestor, and O_PATH
// never reads file contents or blocks on FIFOs. Each seat has its own inode set:
// hard links within a seat count once, aliases across seats count in both.
type diskUsage struct {
	Allocated int64 `json:"allocated_bytes"`
	Apparent  int64 `json:"apparent_bytes"`
}

func (u diskUsage) charged() int64 { return max(u.Allocated, u.Apparent) }

const oPath = 0x200000 // Linux O_PATH (nodeagent already requires Linux/XFS/cgroups).
func measureUsage(ctx context.Context, root, id string) (diskUsage, error) {
	return measureUsageSeen(ctx, root, id, map[[2]uint64]bool{})
}

// Capacity's filesystem accounting needs the physical union, unlike per-seat
// quota accounting. Share only inode metadata, never file contents, across seats.
func measureUsageSeen(ctx context.Context, root, id string, seen map[[2]uint64]bool) (diskUsage, error) {
	var usage diskUsage
	if !validSeatID(id) {
		return usage, ErrParams
	}
	if err := checkUsageMounts(root, id); err != nil {
		return usage, err
	}
	rootfd, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return usage, err
	}
	defer syscall.Close(rootfd)
	fd, err := syscall.Openat(rootfd, id, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return usage, err
	}
	var st syscall.Stat_t
	if err = syscall.Fstat(fd, &st); err != nil {
		syscall.Close(fd)
		return usage, err
	}
	dev := st.Dev
	count := 0
	var walk func(int, int) error
	walk = func(fd, depth int) error {
		f := os.NewFile(uintptr(fd), "usage-dir")
		defer f.Close()
		if depth > 128 {
			return fmt.Errorf("usage depth exceeded")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var ds syscall.Stat_t
		if err := syscall.Fstat(fd, &ds); err != nil {
			return err
		}
		add := func(s syscall.Stat_t) error {
			if s.Dev != dev {
				return fmt.Errorf("usage nested mount")
			}
			key := [2]uint64{uint64(s.Dev), s.Ino}
			if seen[key] {
				return nil
			}
			seen[key] = true
			count++
			if count > 1000000 || len(seen) > 1000000 {
				return fmt.Errorf("usage entry budget exceeded")
			}
			if s.Blocks < 0 || s.Size < 0 || s.Blocks > math.MaxInt64/512 || usage.Allocated > math.MaxInt64-s.Blocks*512 || usage.Apparent > math.MaxInt64-s.Size {
				return fmt.Errorf("usage overflow")
			}
			usage.Allocated += s.Blocks * 512
			usage.Apparent += s.Size
			return nil
		}
		if err := add(ds); err != nil {
			return err
		}
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			names, err := f.Readdirnames(128)
			for _, name := range names {
				if err := ctx.Err(); err != nil {
					return err
				}
				child, err := syscall.Openat(fd, name, oPath|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
				if err != nil {
					return err
				}
				var cs syscall.Stat_t
				err = syscall.Fstat(child, &cs)
				if err == nil && cs.Mode&syscall.S_IFMT == syscall.S_IFDIR {
					// Re-open through the pinned handle, not a mutable seat pathname.
					sub, e := syscall.Openat(child, ".", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
					syscall.Close(child)
					if e != nil {
						return e
					}
					if cs.Dev != dev {
						syscall.Close(sub)
						return fmt.Errorf("usage nested mount")
					}
					if e = walk(sub, depth+1); e != nil {
						return e
					}
				} else {
					syscall.Close(child)
					if err != nil {
						return err
					}
					if err = add(cs); err != nil {
						return err
					}
				}
			}
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
		}
	}
	err = walk(fd, 0)
	if err == nil {
		err = checkUsageMounts(root, id)
	}
	return usage, err
}

type fsSpace struct {
	Total, Free      int64
	Files, FreeFiles uint64
}

func readSpace(root string) (fsSpace, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		return fsSpace{}, err
	}
	if st.Bsize <= 0 || st.Blocks > uint64(math.MaxInt64/int64(st.Bsize)) || st.Bavail > uint64(math.MaxInt64/int64(st.Bsize)) {
		return fsSpace{}, fmt.Errorf("statfs overflow")
	}
	return fsSpace{int64(st.Blocks) * st.Bsize, int64(st.Bavail) * st.Bsize, st.Files, st.Ffree}, nil
}

// A same-device bind mount is invisible to st_dev. Check the kernel mount table
// as well; only metadata is read. Host-root races are outside the trust boundary.
func rejectNestedMount(raw []byte, root, id string) error {
	target := filepath.Clean(filepath.Join(root, id))
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		mount := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(fields[4])
		mount = filepath.Clean(mount)
		if mount == target || strings.HasPrefix(mount, target+string(os.PathSeparator)) {
			return fmt.Errorf("usage nested mount")
		}
	}
	return nil
}
func checkUsageMounts(root, id string) error {
	raw, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	return rejectNestedMount(raw, root, id)
}
