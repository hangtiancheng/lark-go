package teams

import "testing"

// useTempHome redirects the user home directory to a temporary directory so
// that team configuration lands in the test's own sandbox.
//
// The team directory is <home>/.swiftx/teams; without redirection, a single
// test run leaves behind squad, squad-2, etc. in the real home directory, and
// the next run hits "team already exists".
//
// os.UserHomeDir reads USERPROFILE on Windows and HOME on other platforms;
// setting both makes this cross-platform.
func useTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}
