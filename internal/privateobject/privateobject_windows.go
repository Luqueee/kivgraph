//go:build windows

package privateobject

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// definition builds the SDDL that names who may reach the object.
//
// The account is named by SID rather than by a well-known alias, so the rule
// says what it means on a machine whose account names are not the English
// ones. `P` denies inheritance, so nothing broader upstream is folded in --
// which is the whole point: an inherited ACL is a property of where the object
// was put, and this is meant to be a property of the object.
func definition() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("read this process's user: %w", err)
	}
	return "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;" + user.User.Sid.String() + ")", nil
}

// Attributes answers the SECURITY_ATTRIBUTES for an object about to be
// created, for the calls that take one.
func Attributes() (*windows.SecurityAttributes, error) {
	sddl, err := definition()
	if err != nil {
		return nil, err
	}
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, fmt.Errorf("build the security descriptor: %w", err)
	}
	attributes := &windows.SecurityAttributes{SecurityDescriptor: descriptor}
	attributes.Length = uint32(unsafe.Sizeof(*attributes))
	return attributes, nil
}

// Narrow applies the same rule to an object that already exists, for the ones
// created by something that took no attributes -- a listener, for instance.
//
// PROTECTED_DACL_SECURITY_INFORMATION is what makes the `P` stick: without it
// the DACL is replaced and then inheritance puts back whatever the parent
// directory says, which is the state this is here to stop depending on.
func Narrow(path string) error {
	sddl, err := definition()
	if err != nil {
		return err
	}
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("build the security descriptor: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read the security descriptor: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("narrow %q: %w", path, err)
	}
	return nil
}
