package config

import (
	"os"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// Many tests here initialise a configuration at a temporary path, and that
// writes under the user's state directory. See testsupport.IsolateHome.
func TestMain(m *testing.M) { os.Exit(testsupport.IsolateHome(m)) }
