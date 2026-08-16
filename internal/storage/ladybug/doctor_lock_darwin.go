//go:build ladybug && cgo && darwin

package ladybug

/*
#include <errno.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <libproc.h>
#include <sys/proc_info.h>

// kivgraph_database_holders reports the processes of the current user that
// hold the file identified by device and inode open, excluding the caller.
//
// macOS has no /proc/locks. The obvious substitute, fcntl(F_GETLK), cannot be
// used: POSIX releases every record lock a process holds on a file as soon as
// that process closes any descriptor for it, so probing a database this
// process owns would silently unlock the engine. Measured on macOS 26.6 with
// LadybugDB v0.13.1: an external observer saw F_WRLCK before a read-only probe
// and F_UNLCK after it. Enumerating descriptors touches nothing.
//
// The scan is limited to the effective uid. proc_pidinfo refuses another
// user's process without root, and a database created with mode 0600 cannot be
// opened by one anyway. A holder this user cannot inspect - a root process, a
// hardened one - is therefore invisible here; the check reports what it
// observed and never invents a holder.
//
// Returns the number of holders written to pids, -1 on a fatal error, or
// -2 when capacity was too small.
static int kivgraph_database_holders(uint32_t device, uint64_t inode,
                                      int32_t *pids, int capacity) {

	int size = proc_listpids(PROC_UID_ONLY, (uint32_t)geteuid(), NULL, 0);
	if (size <= 0) {
		return -1;
	}
	// Processes can appear between sizing and reading; ask for slack.
	int budget = size + 64 * (int)sizeof(int32_t);
	int32_t *candidates = (int32_t *)malloc((size_t)budget);
	if (candidates == NULL) {
		return -1;
	}
	size = proc_listpids(PROC_UID_ONLY, (uint32_t)geteuid(), candidates, budget);
	if (size <= 0) {
		free(candidates);
		return -1;
	}

	int found = 0;
	int self = (int)getpid();
	int total = size / (int)sizeof(int32_t);
	for (int index = 0; index < total; index++) {
		int pid = candidates[index];
		if (pid <= 0 || pid == self) {
			continue;
		}

		int used = proc_pidinfo(pid, PROC_PIDLISTFDS, 0, NULL, 0);
		if (used <= 0) {
			// A process that exited during the scan, or one this user cannot
			// inspect. Neither is a holder we can name.
			continue;
		}
		int room = used + 32 * (int)sizeof(struct proc_fdinfo);
		struct proc_fdinfo *descriptors = (struct proc_fdinfo *)malloc((size_t)room);
		if (descriptors == NULL) {
			free(candidates);
			return -1;
		}
		used = proc_pidinfo(pid, PROC_PIDLISTFDS, 0, descriptors, room);
		if (used <= 0) {
			free(descriptors);
			continue;
		}

		int descriptorCount = used / (int)sizeof(struct proc_fdinfo);
		int matched = 0;
		for (int entry = 0; entry < descriptorCount && !matched; entry++) {
			if (descriptors[entry].proc_fdtype != PROX_FDTYPE_VNODE) {
				continue;
			}
			struct vnode_fdinfo vnode;
			int read = proc_pidfdinfo(pid, descriptors[entry].proc_fd,
			                          PROC_PIDFDVNODEINFO, &vnode, sizeof(vnode));
			if (read < (int)sizeof(vnode)) {
				continue;
			}
			if ((uint32_t)vnode.pvi.vi_stat.vst_dev == device &&
			    (uint64_t)vnode.pvi.vi_stat.vst_ino == inode) {
				matched = 1;
			}
		}
		free(descriptors);

		if (matched) {
			if (found >= capacity) {
				free(candidates);
				return -2;
			}
			pids[found++] = pid;
		}
	}

	free(candidates);
	return found;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"syscall"
)

// maxDatabaseHolders bounds the answer. A database held open by more than this
// many processes of one user is already the pathology the check reports.
const maxDatabaseHolders = 64

// externalStorageLocks reports the processes other than this one that hold the
// database open. On macOS "holds it open" is the observable form of "holds the
// engine lock": LadybugDB takes an exclusive fcntl lock for the lifetime of an
// open database, so an external descriptor is the actionable signal, and the
// lock table itself cannot be read without risking the lock.
//
// A holder running as another user, which needs root, is not visible. The
// check reports what it observed and never invents a holder it did not see.
func externalStorageLocks(path string) ([]int, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, true, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, true, errors.New("database stat has no Darwin inode data")
	}

	pids := make([]C.int32_t, maxDatabaseHolders)
	found := C.kivgraph_database_holders(
		C.uint32_t(uint32(stat.Dev)),
		C.uint64_t(stat.Ino),
		&pids[0],
		C.int(maxDatabaseHolders),
	)
	switch {
	case found == -1:
		return nil, true, errors.New("enumerate database holders: libproc refused the process list")
	case found == -2:
		return nil, true, fmt.Errorf("more than %d processes hold the database open", maxDatabaseHolders)
	}

	holders := make([]int, 0, int(found))
	for index := 0; index < int(found); index++ {
		holders = append(holders, int(pids[index]))
	}
	sort.Ints(holders)
	return holders, true, nil
}
