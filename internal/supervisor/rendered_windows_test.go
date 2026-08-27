//go:build windows

package supervisor

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
)

// renderedUnit is what an install would write, which here is the encoded form:
// the definition reaches disk as UTF-16 because that is what its declaration
// says and what `schtasks /XML` reads. A test comparing the text would pass
// against a file the scheduler cannot load.
func renderedUnit(t *testing.T, spec Spec) []byte {
	t.Helper()
	return encode(unit(spec))
}

// The negatives first, and the first of them is the one that would produce a
// definition the scheduler rejects rather than a daemon that misbehaves.

// A state directory is a path a user chose, so it can hold the characters XML
// reserves. Rendering one unescaped produces a document that either fails to
// parse or -- worse -- parses into a different task.
func TestADefinitionStaysWellFormedForAwkwardPaths(t *testing.T) {
	for name, directory := range map[string]string{
		"an ampersand":   `C:\graphs\r&d`,
		"a quote":        `C:\graphs\say"what`,
		"angle brackets": `C:\graphs\<tmp>`,
		"an apostrophe":  `C:\graphs\o'brien`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := parseDefinition(unit(testSpec(directory))); err != nil {
				t.Fatalf("the rendered definition does not parse: %v", err)
			}
		})
	}
}

// Four settings are load-bearing and each has a default that breaks a daemon
// quietly. They are asserted by value because the failure they prevent -- a
// server that vanishes on the third day, or two servers on one state
// directory -- looks like something else entirely when it happens.
func TestTheDefinitionOverridesTheDefaultsThatWouldBreakADaemon(t *testing.T) {
	definition := unit(testSpec(`C:\state`))
	for setting, want := range map[string]string{
		"ExecutionTimeLimit":         "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
		"MultipleInstancesPolicy":    "<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>",
		"DisallowStartIfOnBatteries": "<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>",
		"StopIfGoingOnBatteries":     "<StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>",
	} {
		if !strings.Contains(definition, want) {
			t.Fatalf("%s: definition does not carry %q, so the scheduler's default applies", setting, want)
		}
	}
}

// The decision this backend implements is that the daemon is the user's and
// needs no elevation, which is what makes the installer unelevated too.
func TestTheTaskAsksForNoPrivilege(t *testing.T) {
	definition := unit(testSpec(`C:\state`))
	if !strings.Contains(definition, "<RunLevel>LeastPrivilege</RunLevel>") {
		t.Fatal("the task asks for elevation, which the installer would then have to ask for as well")
	}
	if strings.Contains(definition, "HighestAvailable") {
		t.Fatal("the task asks for the highest available privilege")
	}
}

// The scheduler hands Arguments to the process as one string, so a path with a
// space has to arrive quoted or the daemon is started with two arguments where
// it expected one.
func TestArgumentsWithSpacesAreQuoted(t *testing.T) {
	spec := testSpec(`C:\state`)
	spec.ConfigPath = `C:\Users\Ada Lovelace\config.yaml`
	definition := unit(spec)
	if !strings.Contains(definition, `&#34;C:\Users\Ada Lovelace\config.yaml&#34;`) {
		t.Fatalf("definition does not quote a configuration path holding a space:\n%s", definition)
	}
}

func TestTheEncodedDefinitionDeclaresWhatItIs(t *testing.T) {
	encoded := encode(unit(testSpec(`C:\state`)))
	if len(encoded) < 2 || encoded[0] != 0xFF || encoded[1] != 0xFE {
		t.Fatal("the definition has no UTF-16 byte order mark, which is what schtasks reads first")
	}
	if len(encoded)%2 != 0 {
		t.Fatal("the definition is an odd number of bytes, so it is not UTF-16")
	}
}

// parseDefinition reads the definition the way the scheduler will.
//
// The declaration says UTF-16 because that is what reaches disk, while the
// string in hand is Go's UTF-8 for the same document, and the standard decoder
// refuses a declared encoding it was given no reader for. The reader passes
// the bytes through, which is correct precisely because the two differ in
// encoding and not in content -- and it is the encode step, tested separately,
// that makes the file match its own declaration.
func parseDefinition(definition string) error {
	decoder := xml.NewDecoder(strings.NewReader(definition))
	decoder.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
	for {
		_, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
