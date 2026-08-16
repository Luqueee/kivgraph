#define _GNU_SOURCE

#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <sys/uio.h>
#include <unistd.h>

static _Atomic uint64_t target_bytes;
static _Atomic int status_written;

static int is_target_fd(int fd) {
    const char *target = getenv("KIVGRAPH_ENOSPC_PATH");
    if (target == NULL || target[0] == '\0') {
        return 0;
    }

    char descriptor[64];
    int length = snprintf(descriptor, sizeof(descriptor), "/proc/self/fd/%d", fd);
    if (length < 0 || (size_t)length >= sizeof(descriptor)) {
        return 0;
    }

    char path[PATH_MAX];
    ssize_t path_length = readlink(descriptor, path, sizeof(path) - 1);
    if (path_length < 0) {
        return 0;
    }
    path[path_length] = '\0';
    return strcmp(path, target) == 0;
}

static uint64_t byte_limit(void) {
    const char *raw = getenv("KIVGRAPH_ENOSPC_AFTER_BYTES");
    if (raw == NULL || raw[0] == '\0') {
        return 0;
    }
    return strtoull(raw, NULL, 10);
}

static void mark_injected(void) {
    if (atomic_exchange(&status_written, 1) != 0) {
        return;
    }
    const char *path = getenv("KIVGRAPH_ENOSPC_STATUS");
    if (path == NULL || path[0] == '\0') {
        return;
    }
    int fd = (int)syscall(SYS_openat, AT_FDCWD, path, O_CREAT | O_TRUNC | O_WRONLY, 0600);
    if (fd < 0) {
        return;
    }
    const char *phase = getenv("KIVGRAPH_ENOSPC_PHASE");
    if (phase == NULL || phase[0] == '\0') {
        phase = "unknown";
    }
    char status[128];
    int status_length = snprintf(status, sizeof(status), "ENOSPC %s\n", phase);
    if (status_length > 0) {
        (void)syscall(SYS_write, fd, status, (size_t)status_length);
    }
    (void)syscall(SYS_close, fd);
}

static int should_fail(int fd, uint64_t count) {
    const char *armed = getenv("KIVGRAPH_ENOSPC_ARMED");
    if (armed == NULL || strcmp(armed, "1") != 0) {
        return 0;
    }
    if (!is_target_fd(fd)) {
        return 0;
    }
    uint64_t previous = atomic_fetch_add(&target_bytes, count);
    if (previous + count <= byte_limit()) {
        return 0;
    }
    mark_injected();
    errno = ENOSPC;
    return 1;
}

ssize_t write(int fd, const void *buffer, size_t count) {
    static ssize_t (*real_write)(int, const void *, size_t);
    if (real_write == NULL) {
        real_write = dlsym(RTLD_NEXT, "write");
    }
    if (should_fail(fd, count)) {
        return -1;
    }
    return real_write(fd, buffer, count);
}

ssize_t pwrite(int fd, const void *buffer, size_t count, off_t offset) {
    static ssize_t (*real_pwrite)(int, const void *, size_t, off_t);
    if (real_pwrite == NULL) {
        real_pwrite = dlsym(RTLD_NEXT, "pwrite");
    }
    if (should_fail(fd, count)) {
        return -1;
    }
    return real_pwrite(fd, buffer, count, offset);
}

ssize_t pwrite64(int fd, const void *buffer, size_t count, off64_t offset) {
    static ssize_t (*real_pwrite64)(int, const void *, size_t, off64_t);
    if (real_pwrite64 == NULL) {
        real_pwrite64 = dlsym(RTLD_NEXT, "pwrite64");
    }
    if (should_fail(fd, count)) {
        return -1;
    }
    return real_pwrite64(fd, buffer, count, offset);
}

ssize_t writev(int fd, const struct iovec *iov, int iov_count) {
    static ssize_t (*real_writev)(int, const struct iovec *, int);
    if (real_writev == NULL) {
        real_writev = dlsym(RTLD_NEXT, "writev");
    }
    uint64_t count = 0;
    for (int index = 0; index < iov_count; index++) {
        count += iov[index].iov_len;
    }
    if (should_fail(fd, count)) {
        return -1;
    }
    return real_writev(fd, iov, iov_count);
}

ssize_t pwritev(int fd, const struct iovec *iov, int iov_count, off_t offset) {
    static ssize_t (*real_pwritev)(int, const struct iovec *, int, off_t);
    if (real_pwritev == NULL) {
        real_pwritev = dlsym(RTLD_NEXT, "pwritev");
    }
    uint64_t count = 0;
    for (int index = 0; index < iov_count; index++) {
        count += iov[index].iov_len;
    }
    if (should_fail(fd, count)) {
        return -1;
    }
    return real_pwritev(fd, iov, iov_count, offset);
}

int ftruncate(int fd, off_t length) {
    static int (*real_ftruncate)(int, off_t);
    if (real_ftruncate == NULL) {
        real_ftruncate = dlsym(RTLD_NEXT, "ftruncate");
    }
    struct stat info;
    if (is_target_fd(fd) && fstat(fd, &info) == 0 && length > info.st_size &&
        should_fail(fd, (uint64_t)(length - info.st_size))) {
        return -1;
    }
    return real_ftruncate(fd, length);
}

int fallocate(int fd, int mode, off_t offset, off_t length) {
    static int (*real_fallocate)(int, int, off_t, off_t);
    if (real_fallocate == NULL) {
        real_fallocate = dlsym(RTLD_NEXT, "fallocate");
    }
    if (should_fail(fd, (uint64_t)length)) {
        return -1;
    }
    return real_fallocate(fd, mode, offset, length);
}

int posix_fallocate(int fd, off_t offset, off_t length) {
    static int (*real_posix_fallocate)(int, off_t, off_t);
    if (real_posix_fallocate == NULL) {
        real_posix_fallocate = dlsym(RTLD_NEXT, "posix_fallocate");
    }
    if (should_fail(fd, (uint64_t)length)) {
        return ENOSPC;
    }
    return real_posix_fallocate(fd, offset, length);
}
