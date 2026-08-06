package app

import (
	"context"
	"errors"
	"fmt"
)

// CloseFunc closes one application-owned resource. The context bounds work
// such as a worker's graceful SHUTDOWN; resources that do not need it can
// ignore it through Lifecycle.AddCloser.
type CloseFunc func(context.Context) error

// Resource is one close operation in shutdown order. Resources are closed in
// the order supplied to Shutdown: ingress first, then its producers and
// finally storage. That ordering is intentional; reverse-registration order
// would let new work arrive while its dependencies were already closing.
type Resource struct {
	Name  string
	Close CloseFunc
}

// Shutdown closes every resource, retaining all failures instead of stopping
// at the first one. A failed watcher must not leave the worker or database
// open, and a failed worker must not suppress connection cleanup.
func Shutdown(ctx context.Context, resources ...Resource) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var failures []error
	for index, resource := range resources {
		if resource.Close == nil {
			continue
		}
		name := resource.Name
		if name == "" {
			name = fmt.Sprintf("resource[%d]", index)
		}
		if err := resource.Close(ctx); err != nil {
			failures = append(failures, fmt.Errorf("close %s: %w", name, err))
		}
	}
	return errors.Join(failures...)
}
