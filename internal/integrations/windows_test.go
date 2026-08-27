package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Windows is driven through the GOOS option rather than through a build tag,
// so these run everywhere the suite does. That is deliberate: the branch they
// cover decides where a user's configuration is written, and one that is only
// executed on the platform CI does not have is one that stays wrong.

func windowsManager(t *testing.T, home string) Manager {
	t.Helper()
	// LOCALAPPDATA is pinned to a directory the test owns, because the Claude
	// Desktop path now looks there for an MSIX package. Left alone, a suite run
	// on a Windows machine that actually has Claude Desktop installed would
	// resolve the real package and these fixtures would be asserting against
	// the host.
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	manager, err := New(Options{
		HomeDir:    home,
		ProjectDir: t.TempDir(),
		Executable: filepath.Join(t.TempDir(), "kivgraph.exe"),
		GOOS:       "windows",
		Endpoint:   Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "a-token"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return manager
}

// The negatives first. A platform this does not know must still be refused,
// and the refusal has to name what it was handed or an operator cannot tell a
// typo from an unsupported machine.
func TestAnUnknownPlatformIsStillRefusedAndNamed(t *testing.T) {
	_, err := New(Options{
		HomeDir:    t.TempDir(),
		ProjectDir: t.TempDir(),
		Executable: filepath.Join(t.TempDir(), "kivgraph"),
		GOOS:       "plan9",
		Endpoint:   Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "a-token"},
	})
	if err == nil {
		t.Fatal("New(plan9) = nil error, want the platform refused")
	}
	if got := err.Error(); !contains(got, "plan9") {
		t.Fatalf("New(plan9) error = %q, want it to name what it was given", got)
	}
}

// Claude Desktop is the only target whose location differs by platform, and on
// Windows it is not under the home directory at all.
func TestClaudeDesktopIsFoundWhereWindowsKeepsIt(t *testing.T) {
	home := t.TempDir()
	roaming := filepath.Join(t.TempDir(), "Roaming")
	t.Setenv("APPDATA", roaming)
	manager := windowsManager(t, home)

	path, _, _, err := manager.mcpPath(TargetClaudeDesktop, ScopeUser)
	if err != nil {
		t.Fatalf("configTarget() error = %v", err)
	}
	want := filepath.Join(roaming, "Claude", "claude_desktop_config.json")
	if path != want {
		t.Fatalf("configTarget() = %q, want %q", path, want)
	}
}

// APPDATA is redirected on a machine with roaming profiles, so a path built
// out of the home directory would be one nothing reads. The fallback exists
// for the machine that has not been redirected, and is asserted so that the
// two cannot be confused.
func TestTheRoamingDirectoryPrefersTheEnvironmentAndFallsBackToTheProfile(t *testing.T) {
	home := t.TempDir()
	manager := windowsManager(t, home)

	t.Setenv("APPDATA", "")
	fallback, _, _, err := manager.mcpPath(TargetClaudeDesktop, ScopeUser)
	if err != nil {
		t.Fatalf("configTarget() error = %v", err)
	}
	if want := filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json"); fallback != want {
		t.Fatalf("with no APPDATA: configTarget() = %q, want %q", fallback, want)
	}

	redirected := filepath.Join(t.TempDir(), "elsewhere")
	t.Setenv("APPDATA", redirected)
	moved, _, _, err := manager.mcpPath(TargetClaudeDesktop, ScopeUser)
	if err != nil {
		t.Fatalf("configTarget() error = %v", err)
	}
	if moved == fallback {
		t.Fatal("configTarget() ignored APPDATA, so a redirected profile would be written past")
	}
}

// Every other client keeps its configuration under the home directory on all
// three platforms, so opening Windows must not have moved them.
func TestTheHomeRelativeClientsAreUnmovedByThePlatform(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	windows, err := New(Options{
		HomeDir:    home,
		ProjectDir: project,
		Executable: filepath.Join(t.TempDir(), "kivgraph.exe"),
		GOOS:       "windows",
		Endpoint:   Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "a-token"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	linux, err := New(Options{
		HomeDir:    home,
		ProjectDir: project,
		Executable: filepath.Join(t.TempDir(), "kivgraph"),
		GOOS:       "linux",
		Endpoint:   Endpoint{URL: "http://127.0.0.1:7788/mcp", Token: "a-token"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, target := range []Target{TargetClaudeCode, TargetCodex, TargetOpenCode, TargetOhMyPi} {
		for _, scope := range []Scope{ScopeUser, ScopeProject} {
			onWindows, _, _, windowsErr := windows.mcpPath(target, scope)
			onLinux, _, _, linuxErr := linux.mcpPath(target, scope)
			if (windowsErr == nil) != (linuxErr == nil) {
				t.Fatalf("%s/%s: windows err = %v, linux err = %v, want them to agree", target, scope, windowsErr, linuxErr)
			}
			if windowsErr == nil && onWindows != onLinux {
				t.Fatalf("%s/%s: windows = %q, linux = %q, want the same home-relative path", target, scope, onWindows, onLinux)
			}
		}
	}
}

// A stdio registration names the binary the client must launch, and on Windows
// the binary has an extension. An entry without it is one the client cannot
// start -- the same defect `stop` had one layer down, in the file a user reads
// rather than in a process table.
//
// The HTTP registration names a URL instead, which is why this asks for the
// stdio one: asserting the executable against a manager holding an endpoint
// would assert nothing and pass.
func TestTheRegistrationNamesTheBinaryAsItIsStored(t *testing.T) {
	home := t.TempDir()
	executable := filepath.Join(t.TempDir(), "kivgraph.exe")
	if err := os.WriteFile(executable, []byte("x"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	manager, err := New(Options{
		HomeDir:    home,
		ProjectDir: t.TempDir(),
		Executable: executable,
		GOOS:       "windows",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := manager.InstallMCP(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallMCP() error = %v", err)
	}
	written, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("read the registration: %v", err)
	}
	if !contains(string(written), "kivgraph.exe") {
		t.Fatalf("registration = %s, want it to name the executable as stored", written)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// The OpenCode shim is JavaScript, and a Windows path is mostly backslashes.
// JavaScript does not refuse an escape it does not know -- it drops the
// backslash -- so a path pasted into a string literal is corrupted quietly and
// the shim fails later, at the moment the editor calls a binary that is not
// there. The escapes that do something are the ones worth naming.
func TestThePluginDoesNotCorruptAPathWithBackslashes(t *testing.T) {
	for name, executable := range map[string]string{
		"a backspace escape": `C:\bin\kivgraph.exe`,
		"a newline escape":   `C:\next\kivgraph.exe`,
		"a tab escape":       `C:\tools\kivgraph.exe`,
		"an unknown escape":  `C:\opt\kivgraph.exe`,
	} {
		t.Run(name, func(t *testing.T) {
			// The manager is built directly rather than through New, which
			// would absolutise a Windows path against the working directory
			// of whatever platform is running the test. What is under test is
			// how pluginBody puts a path into JavaScript, not how New decides
			// what the path is.
			manager := Manager{executable: executable, goos: "windows"}
			body := string(manager.pluginBody())
			if contains(body, executablePlaceholder) {
				t.Fatal("the plugin still carries its placeholder")
			}
			// The encoded form is what a JavaScript parser turns back into the
			// path. Asserting the raw path would pass on the corruption.
			encoded, err := json.Marshal(executable)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if !contains(body, string(encoded)) {
				t.Fatalf("plugin does not carry %s; it would name a different file", encoded)
			}
		})
	}
}

// Claude Desktop on Windows ships as an MSIX package, and MSIX redirects a
// packaged application's writes into its own container: it asks for
// `%APPDATA%\Claude` and Windows gives it
// `%LOCALAPPDATA%\Packages\Claude_<hash>\LocalCache\Roaming\Claude`. So a
// configuration written to the documented path is one the application never
// reads, and `mcp install --target claude-desktop` reported success while
// changing nothing that loads an MCP server.
//
// Measured on a host running Claude Desktop 1.37937.3.0 from the Store:
// `%APPDATA%\Claude` absent, `%LOCALAPPDATA%\AnthropicClaude` absent, and the
// live `claude_desktop_config.json` inside the package. Both markers this
// manager had were wrong, and so was the path it wrote to.
//
// What these cannot check is that Windows still redirects. That is a property
// of Windows, and the reason the paths above carry the version they were
// observed under.

func claudeDesktopPath(t *testing.T, manager Manager) string {
	t.Helper()
	path, _, _, err := manager.mcpPath(TargetClaudeDesktop, ScopeUser)
	if err != nil {
		t.Fatalf("mcpPath() error = %v", err)
	}
	return path
}

// An installed package that has already run: its data directory holds the file
// the application is reading now, so that is the file to edit.
func TestClaudeDesktopIsWrittenInsideItsPackage(t *testing.T) {
	home := t.TempDir()
	roaming := filepath.Join(t.TempDir(), "Roaming")
	t.Setenv("APPDATA", roaming)
	manager := windowsManager(t, home)
	data := filepath.Join(home, "AppData", "Local", "Packages",
		"Claude_pzs8sxrjxfjjc", "LocalCache", "Roaming", "Claude")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatalf("create package data: %v", err)
	}

	got := claudeDesktopPath(t, manager)
	if want := filepath.Join(data, "claude_desktop_config.json"); got != want {
		t.Fatalf("mcpPath() = %q, want the packaged path %q", got, want)
	}
	if got == filepath.Join(roaming, "Claude", "claude_desktop_config.json") {
		t.Fatal("mcpPath() returned the documented path, which a packaged install never reads")
	}
}

// Installed but never run: the package exists and its data directory does not.
// The configuration still belongs inside it, because that is where the
// application looks the first time it starts.
func TestClaudeDesktopPackageThatHasNeverRunIsStillThePlace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("APPDATA", filepath.Join(t.TempDir(), "Roaming"))
	manager := windowsManager(t, home)
	pkg := filepath.Join(home, "AppData", "Local", "Packages", "Claude_pzs8sxrjxfjjc")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatalf("create package: %v", err)
	}

	want := filepath.Join(pkg, "LocalCache", "Roaming", "Claude", "claude_desktop_config.json")
	if got := claudeDesktopPath(t, manager); got != want {
		t.Fatalf("mcpPath() = %q, want %q", got, want)
	}
}

// Two packages, one of them already running. The choice has to be the one the
// application is using, and the same one every time.
func TestClaudeDesktopPrefersThePackageThatHasRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("APPDATA", filepath.Join(t.TempDir(), "Roaming"))
	manager := windowsManager(t, home)
	packages := filepath.Join(home, "AppData", "Local", "Packages")
	// Named so the one that has run sorts *second*, which is what makes this a
	// test of the preference rather than of the glob's order.
	if err := os.MkdirAll(filepath.Join(packages, "Claude_aaaaaaaaaaaaa"), 0o755); err != nil {
		t.Fatalf("create package: %v", err)
	}
	running := filepath.Join(packages, "Claude_zzzzzzzzzzzzz")
	if err := os.MkdirAll(filepath.Join(running, "LocalCache", "Roaming", "Claude"), 0o755); err != nil {
		t.Fatalf("create package data: %v", err)
	}

	want := filepath.Join(running, "LocalCache", "Roaming", "Claude", "claude_desktop_config.json")
	for attempt := range 3 {
		if got := claudeDesktopPath(t, manager); got != want {
			t.Fatalf("attempt %d: mcpPath() = %q, want the package that has run %q", attempt, got, want)
		}
	}
}

// Detection is the other half. On the measured host neither older marker
// exists, so a packaged install was reported as no install at all and the
// target was never offered.
func TestClaudeDesktopIsDetectedFromItsPackageAlone(t *testing.T) {
	home := t.TempDir()
	roaming := filepath.Join(t.TempDir(), "Roaming")
	t.Setenv("APPDATA", roaming)
	manager := windowsManager(t, home)
	local := filepath.Join(home, "AppData", "Local")
	if err := os.MkdirAll(filepath.Join(local, "Packages", "Claude_pzs8sxrjxfjjc"), 0o755); err != nil {
		t.Fatalf("create package: %v", err)
	}
	for _, absent := range []string{filepath.Join(roaming, "Claude"), filepath.Join(local, "AnthropicClaude")} {
		if _, err := os.Stat(absent); !os.IsNotExist(err) {
			t.Fatalf("stat %q = %v, want it absent for this case to mean anything", absent, err)
		}
	}

	detections, err := manager.DetectMCPTargets(ScopeUser)
	if err != nil {
		t.Fatalf("DetectMCPTargets() error = %v", err)
	}
	detected := false
	for _, detection := range detections {
		if detection.Target == TargetClaudeDesktop {
			detected = detection.Detected
		}
	}
	if !detected {
		t.Fatal("a packaged Claude Desktop was not detected, so the target is never offered")
	}
}
