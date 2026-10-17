package exports

import "testing"

// Sys is how the export touches the file system.
type Sys = sysOps

// Inject lets change replace steps of sys until the test ends. A test using
// it must not run in parallel.
func Inject(t *testing.T, change func(*Sys)) {
	t.Helper()
	was := sys
	change(&sys)
	t.Cleanup(func() { sys = was })
}
