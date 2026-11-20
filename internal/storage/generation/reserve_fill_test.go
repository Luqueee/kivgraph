package generation

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The reserve is the one file whose size is a contract, so the boundaries of the
// 1 MiB chunk it is written in are the cases: none, one byte, one short of a
// chunk, exactly one, one over, and several with a remainder.
func TestWriteZerosWritesExactlyTheSizeAsked(t *testing.T) {
	const chunk = 1 << 20
	for _, size := range []int64{0, 1, chunk - 1, chunk, chunk + 1, 3*chunk + 17} {
		path := filepath.Join(t.TempDir(), "reserve")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeZeros(file, size); err != nil {
			t.Fatalf("writeZeros(%d) error = %v", size, err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(content)) != size || !bytes.Equal(content, make([]byte, size)) {
			t.Fatalf("writeZeros(%d) left %d bytes, want %d zero bytes", size, len(content), size)
		}
	}
}

func TestWriteZerosReportsAWriteThatFails(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "reserve"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writeZeros(file, 1); err == nil {
		t.Fatal("writing to a closed file reported no error: a reserve that was not written would be taken for one")
	}
}
