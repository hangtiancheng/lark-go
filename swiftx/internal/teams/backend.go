package teams

import (
	"os"
	"runtime"
)

// detectBackend: use a pane backend only when already inside a tmux / iTerm2
// session; otherwise fall back to in-process. Detection relies on environment
// variables — tmux and iTerm2 automatically set TMUX / ITERM_SESSION_ID for
// processes inside a session, requiring no manual configuration.
// DetectBackend is exported for external packages: the Agent tool auto-creates
// a Team when the specified one does not exist, and needs to pick a backend
// based on the current environment.
func DetectBackend() TeamMode { return detectBackend() }

func detectBackend() TeamMode {
	// Windows guard: tmux pane spawn executes POSIX commands via pwsh which
	// fails, so always use in-process on Windows.
	if runtime.GOOS == "windows" {
		return ModeInProcess
	}
	return detectBackendFromEnv()
}

// detectBackendFromEnv determines the backend solely from environment variables,
// extracted for unit testing (independent of the host platform).
func detectBackendFromEnv() TeamMode {
	if os.Getenv("TMUX") != "" {
		return ModeTmux
	}
	if os.Getenv("ITERM_SESSION_ID") != "" {
		return ModeITerm
	}
	return ModeInProcess
}
