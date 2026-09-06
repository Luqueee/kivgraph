package config

import "testing"

func TestIsBuildConfigurationFileRecognisesSupportedNames(t *testing.T) {
	for path, want := range map[string]bool{
		"NuGet.config":       true,
		"packages.config":    true,
		"pyrightconfig.json": true,
		"app.config":         false,
		"web.config":         false,
	} {
		if got := IsBuildConfigurationFile(path); got != want {
			t.Fatalf("IsBuildConfigurationFile(%q) = %v, want %v", path, got, want)
		}
	}
}
