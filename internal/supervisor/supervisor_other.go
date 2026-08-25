//go:build !linux && !darwin

package supervisor

// The distribution targets are linux/amd64 and darwin/arm64. Anywhere else the
// absence is declared and named rather than answered with a zero value: a caller
// that cannot install a supervisor has to be able to tell an operator that the
// daemon will have no owner here, which is exactly the fact that decides whether
// registering clients against it is safe.

func install(spec Spec) (Report, error) {
	return unsupported(spec)
}

func remove(spec Spec) (Report, error) {
	return unsupported(spec)
}

func status(spec Spec) (Report, error) {
	report, _ := unsupported(spec)
	// Status is a question, not a request: on a platform with no supervisor the
	// honest answer is "unsupported", and that is not an error.
	return report, nil
}

func unsupported(spec Spec) (Report, error) {
	label, err := spec.Label()
	if err != nil {
		return Report{}, err
	}
	return Report{
		State:  StateUnsupported,
		Label:  label,
		Detail: "no supervisor is supported here: the daemon has to be started by hand",
	}, ErrUnsupportedPlatform
}
