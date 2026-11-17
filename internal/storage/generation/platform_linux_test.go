//go:build linux

package generation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A filesystem that cannot fallocate -- NFS before 4.2, 9p, most FUSE
// filesystems -- answers EOPNOTSUPP, and the store used to refuse to open on it:
// the reserve could not be created. The reserve is what lets a full disk still
// publish, so it has to be real blocks wherever the kernel cannot hand them out
// in one call.
//
// The kernel offers no way to ask for such a filesystem from a test, so this one
// runs on a directory the caller points at, and is skipped otherwise:
//
//	KIVGRAPH_NO_FALLOCATE_DIR=/mnt/ntfs go test ./internal/storage/generation -run WithoutFallocate
func TestPreallocateOnAFilesystemWithoutFallocate(t *testing.T) {
	directory := os.Getenv("KIVGRAPH_NO_FALLOCATE_DIR")
	if directory == "" {
		t.Skip("set KIVGRAPH_NO_FALLOCATE_DIR to a directory on a filesystem that rejects fallocate")
	}
	probe, err := os.CreateTemp(directory, "probe-*")
	if err != nil {
		t.Fatalf("create the probe: %v", err)
	}
	defer os.Remove(probe.Name())
	defer probe.Close()
	if err := syscall.Fallocate(int(probe.Fd()), 0, 0, 4096); !unsupportedFallocate(err) {
		t.Skipf("fallocate on %s answers %v, so it is not the filesystem this test is about", directory, err)
	}

	const size = 3<<20 + 17
	path := filepath.Join(directory, "reserve")
	defer os.Remove(path)
	if err := preallocate(path, size); err != nil {
		t.Fatalf("preallocate() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != size {
		t.Fatalf("reserve is %d bytes, want %d", info.Size(), size)
	}
	if blocks := info.Sys().(*syscall.Stat_t).Blocks * 512; blocks < size {
		t.Fatalf("reserve holds %d bytes of blocks, want at least %d: a sparse file reserves nothing", blocks, size)
	}
}

func TestUnsupportedFallocateIsOnlyTheKernelSayingItCannot(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
		want bool
	}{
		{"operation not supported", syscall.EOPNOTSUPP, true},
		{"not implemented", syscall.ENOSYS, true},
		{"wrapped", fmt.Errorf("fallocate: %w", syscall.EOPNOTSUPP), true},
		// A full disk is the one answer that must stay an error: the reserve is
		// not there and writing zeros would only fill what is left.
		{"no space", syscall.ENOSPC, false},
		{"over the file size limit", syscall.EFBIG, false},
		{"bad argument", syscall.EINVAL, false},
		{"no error", nil, false},
		{"unrelated", errors.New("boom"), false},
	} {
		if got := unsupportedFallocate(testCase.err); got != testCase.want {
			t.Errorf("%s: unsupportedFallocate(%v) = %v, want %v", testCase.name, testCase.err, got, testCase.want)
		}
	}
}
