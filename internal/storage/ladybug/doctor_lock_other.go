//go:build ladybug && cgo && !linux && !darwin

package ladybug

func externalStorageLocks(string) ([]int, bool, error) {
	return nil, false, nil
}
