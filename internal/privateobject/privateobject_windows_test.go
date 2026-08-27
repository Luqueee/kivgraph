//go:build windows

package privateobject_test

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Luqueee/kivgraph/internal/privateobject"
)

// Coverage of this package is 76.0% of statements on Windows. Everything not
// covered is an error return from a call that cannot fail here: reading the
// token of the process doing the reading, parsing an SDDL this package built
// itself, and taking the DACL out of a descriptor parsed one line above.
// Reaching them would need either an operating system that refuses a call it
// cannot refuse, or a seam in production code that exists only for a test.
//
// The refusals first. A function whose job is to restrict something must fail
// loudly when it cannot: silently succeeding on a path that does not exist is
// how a caller ends up believing an object is private that nobody narrowed.

func TestNarrowRefusesWhatItCannotReach(t *testing.T) {
	for name, path := range map[string]string{
		"an empty path":  "",
		"an absent path": filepath.Join(t.TempDir(), "not-here"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := privateobject.Narrow(path); err == nil {
				t.Fatalf("Narrow(%q) = nil, want a refusal", path)
			}
		})
	}
}

// The claim this package makes is not "the DACL is right" but "the DACL is
// this object's own": an inherited one is a property of the directory the
// object was put in, and the whole point is that the guarantee stops depending
// on that.
func TestNarrowReplacesInheritanceRatherThanAddingToIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("token"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := privateobject.Narrow(path); err != nil {
		t.Fatalf("Narrow() error = %v", err)
	}

	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo() error = %v", err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		t.Fatalf("Control() error = %v", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("the DACL is not protected, so it is still inherited from wherever the file was put")
	}

	// And the file is still the file: narrowing must not disturb what it
	// guards, or a token would be unreadable by the process that minted it.
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "token" {
		t.Fatalf("after Narrow(): contents = %q, %v, want them untouched", contents, err)
	}
}

func TestAttributesCarryAProtectedDescriptor(t *testing.T) {
	attributes, err := privateobject.Attributes()
	if err != nil {
		t.Fatalf("Attributes() error = %v", err)
	}
	if attributes == nil || attributes.SecurityDescriptor == nil {
		t.Fatal("Attributes() returned nothing to attach to an object")
	}
	if attributes.Length == 0 {
		t.Fatal("Attributes().Length is zero, which Windows reads as a malformed structure")
	}
	control, _, err := attributes.SecurityDescriptor.Control()
	if err != nil {
		t.Fatalf("Control() error = %v", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("the descriptor would let the object inherit whatever its namespace offers")
	}
}

// The account this process runs as is one of the three the object allows, and
// no fourth is.
//
// It walks the ACEs and compares SIDs rather than reading the descriptor's
// string, because Windows canonicalises that string on the way out: an
// explicit SID comes back as a well-known alias when one fits, so the first
// version of this test asserted the SID it wrote and was handed "LA". The
// notation is Windows's business; the identities are the contract.
func TestTheObjectAllowsTheOwnerAndNobodyUnexpected(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("GetTokenUser() error = %v", err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatalf("CreateWellKnownSid(SYSTEM) error = %v", err)
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatalf("CreateWellKnownSid(Administrators) error = %v", err)
	}

	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := privateobject.Narrow(path); err != nil {
		t.Fatalf("Narrow() error = %v", err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo() error = %v", err)
	}
	list, _, err := descriptor.DACL()
	if err != nil {
		t.Fatalf("DACL() error = %v", err)
	}
	if list == nil {
		t.Fatal("the DACL is absent, which grants everyone everything")
	}

	allowed := make([]string, 0, list.AceCount)
	sawOwner := false
	for index := uint32(0); index < uint32(list.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(list, index, &ace); err != nil {
			t.Fatalf("GetAce(%d) error = %v", index, err)
		}
		sid := (*windows.SID)(unsafe.Pointer(uintptr(unsafe.Pointer(ace)) + unsafe.Offsetof(ace.SidStart)))
		allowed = append(allowed, sid.String())
		switch {
		case sid.Equals(user.User.Sid):
			sawOwner = true
		case sid.Equals(system), sid.Equals(administrators):
		default:
			t.Fatalf("the object allows %q, which is none of this user, SYSTEM or the administrators", sid)
		}
	}
	if !sawOwner {
		t.Fatalf("allowed = %v, want the account that created the object among them", allowed)
	}
}
