//go:build ladybug && cgo && !linux

package ladybug

func externalStorageLocks(string) ([]int, bool, error) {
	return nil, false, nil
}
