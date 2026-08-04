//go:build ladybug && cgo

package ladybug

/*
#include <stdint.h>
#include <stdlib.h>

char* lbug_get_version(void);
uint64_t lbug_get_storage_version(void);
void lbug_destroy_string(char* value);
*/
import "C"

func nativeVersion() (string, uint64) {
	value := C.lbug_get_version()
	if value == nil {
		return "unknown", uint64(C.lbug_get_storage_version())
	}
	version := C.GoString(value)
	C.lbug_destroy_string(value)
	return version, uint64(C.lbug_get_storage_version())
}
