package generation

import "os"

// writeZeros gives file size bytes of real blocks by writing them, which is the
// reservation on a platform or filesystem that cannot hand blocks out without
// writing them: a sparse file reserves nothing, and the reserve exists so a full
// disk can still publish.
func writeZeros(file *os.File, size int64) error {
	buffer := make([]byte, 1<<20)
	for remaining := size; remaining > 0; {
		chunk := int64(len(buffer))
		if remaining < chunk {
			chunk = remaining
		}
		if _, err := file.Write(buffer[:chunk]); err != nil {
			return err
		}
		remaining -= chunk
	}
	return nil
}
