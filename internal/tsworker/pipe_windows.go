//go:build windows

package tsworker

import (
	"fmt"
	"os"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// pipeBufferBytes is what the kernel keeps for one direction of the pipe. It is
// a hint, not a limit: a frame larger than this is delivered in pieces.
const pipeBufferBytes = 64 * 1024

// pipeSequence keeps two sessions of one process from asking for the same name.
var pipeSequence atomic.Uint64

// interruptibleOutputPipe returns the parent's read end of the worker's stdout
// and the end the child writes to.
//
// It is a named pipe because an anonymous one cannot be interrupted here. Go
// cannot associate the handles os.Pipe returns on Windows with its completion
// port, so SetReadDeadline answers os.ErrNoDeadline and a blocked ReadFrame
// never comes back -- which is how the supervisor lost the ability to give up
// on a worker that had stopped answering. A pipe opened overlapped is
// pollable, and the name is what buys the overlapped handle.
//
// The name is also the cost, and it is a security cost rather than an
// aesthetic one. An anonymous pipe is reachable only through a handle somebody
// inherited; a named one is an object in a namespace every process on the
// machine can enumerate, and these frames carry the graph. So this narrows it
// back to what the anonymous pipe gave for free:
//
//   - FILE_FLAG_FIRST_PIPE_INSTANCE, so a process that guessed the name cannot
//     have created it first and be holding the end the child will connect to.
//   - PIPE_REJECT_REMOTE_CLIENTS, so the object is not reachable over SMB.
//   - A DACL naming this user, SYSTEM and the administrators, and nobody else.
//
// One instance is allowed, so the child is the only thing that can connect at
// all.
func interruptibleOutputPipe() (parent, child *os.File, err error) {
	name := fmt.Sprintf(`\\.\pipe\kivgraph-worker-%d-%d`, os.Getpid(), pipeSequence.Add(1))
	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, nil, err
	}
	attributes, err := privatePipeAttributes()
	if err != nil {
		return nil, nil, err
	}
	server, err := windows.CreateNamedPipe(namePointer,
		windows.PIPE_ACCESS_INBOUND|windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		1, pipeBufferBytes, pipeBufferBytes, 0, attributes)
	if err != nil {
		return nil, nil, fmt.Errorf("create the worker pipe: %w", err)
	}
	// The child connects by opening the name, which moves the instance out of
	// the listening state; the server needs no ConnectNamedPipe of its own
	// once that has happened. Its handle is the one thing the child inherits.
	inheritable := &windows.SecurityAttributes{InheritHandle: 1}
	inheritable.Length = uint32(unsafe.Sizeof(*inheritable))
	client, err := windows.CreateFile(namePointer, windows.GENERIC_WRITE, 0, inheritable,
		windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		_ = windows.CloseHandle(server)
		return nil, nil, fmt.Errorf("open the worker pipe: %w", err)
	}
	return os.NewFile(uintptr(server), name), os.NewFile(uintptr(client), name), nil
}

// privatePipeAttributes builds the descriptor that keeps the pipe the owner's.
//
// The user is named by SID rather than by a well-known alias so the rule says
// what it means on a machine whose account names are not the English ones.
func privatePipeAttributes() (*windows.SecurityAttributes, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read this process's user: %w", err)
	}
	// P denies inheritance, so nothing broader upstream is added to this.
	definition := "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;" + user.User.Sid.String() + ")"
	descriptor, err := windows.SecurityDescriptorFromString(definition)
	if err != nil {
		return nil, fmt.Errorf("build the worker pipe descriptor: %w", err)
	}
	attributes := &windows.SecurityAttributes{SecurityDescriptor: descriptor}
	attributes.Length = uint32(unsafe.Sizeof(*attributes))
	return attributes, nil
}
