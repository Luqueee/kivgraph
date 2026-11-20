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
		// The capital is the word boundary: these end in "test" and are not tests.
		"src/main/java/app/Latest.java": false, "src/App/Contest.cs": false, "src/App/Greatests.cs": false,
		"src/main/java/app/Test.java": true, "src/App/Tests.cs": true,
		// Maven's integration tests, and the `it` that is something else.
		"src/it/java/app/ServiceIT.java": true, "module/src/it/settings.xml": true,
		"locales/it/messages.go": false, "src/item/it.go": false, "it/src/main.go": false,
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
