package testpath

import "testing"

func TestIsReadsTheConventionsOfEveryIndexedLanguage(t *testing.T) {
	for path, want := range map[string]bool{
		"internal/a_test.go": true, "a.go": false,
		"src/widget.test.ts": true, "src/widget.spec.tsx": true, "src/widget.ts": false,
		"pkg/test_model.py": true, "pkg/model_test.py": true, "pkg/conftest.py": true, "pkg/model.py": false,
		"src/test/java/app/AppTest.java": true, "src/main/java/app/App.java": false, "app/ServiceTests.cs": true,
		// A .NET solution that keeps its projects under `test/`, with file names
		// that say nothing: the layout a rule read off the templates missed.
		"test/App.UnitTests/GuardAgainstNull.cs": true, "src/App/Guard.cs": false,
		"lib/models_test.dart": true, "test/models.dart": true,
		"tests/integration.rs": true, "src/__tests__/x.ts": true, "src/contest.ts": false,
		"testdata/fixture.go": false, "src/latest/value.go": false,
		`src\widget.test.ts`: true,
	} {
		if got := Is(path); got != want {
			t.Errorf("Is(%q) = %v, want %v", path, got, want)
		}
	}
}
