package main

/*
#include <stdint.h>
#include <stddef.h>
static size_t mieru_host_length(const char *s) {
    size_t n = 0;
    if (s == NULL) return 0;
    while (n < 256 && s[n] != 0) n++;
    return n;
}
*/
import "C"

import (
	"unsafe"

	"telegramita/mieru/bridge"
)

var registry = bridge.NewRegistry()

//export MieruClientNew
func MieruClientNew() C.uint64_t {
	return C.uint64_t(registry.NewClient())
}

//export MieruClientStore
func MieruClientStore(handle C.uint64_t, data *C.char, length C.int64_t) C.int32_t {
	if data == nil || length <= 0 || length > bridge.MaxConfigSize {
		return C.int32_t(bridge.Invalid)
	}
	config := C.GoBytes(unsafe.Pointer(data), C.int(length))
	defer clear(config)
	return C.int32_t(registry.Store(uint64(handle), config))
}

//export MieruClientStart
func MieruClientStart(handle C.uint64_t) C.int32_t {
	return C.int32_t(registry.Start(uint64(handle)))
}

//export MieruClientStop
func MieruClientStop(handle C.uint64_t) {
	registry.Stop(uint64(handle))
}

//export MieruClientFree
func MieruClientFree(handle C.uint64_t) {
	registry.Free(uint64(handle))
}

//export MieruConnectionNew
func MieruConnectionNew(client C.uint64_t) C.uint64_t {
	return C.uint64_t(registry.NewConnection(uint64(client)))
}

//export MieruConnectionDial
func MieruConnectionDial(handle C.uint64_t, host *C.char, port C.uint16_t, timeout C.int64_t) C.int32_t {
	length := C.mieru_host_length(host)
	if length == 0 || length > 255 {
		return C.int32_t(bridge.Invalid)
	}
	return C.int32_t(registry.Dial(uint64(handle), C.GoStringN(host, C.int(length)), uint16(port), int64(timeout)))
}

//export MieruConnectionRead
func MieruConnectionRead(handle C.uint64_t, buffer unsafe.Pointer, capacity C.int64_t, timeout C.int64_t) C.int64_t {
	if buffer == nil || capacity <= 0 || capacity > bridge.MaxBufferSize {
		return C.int64_t(bridge.Invalid)
	}
	return C.int64_t(registry.Read(uint64(handle), unsafe.Slice((*byte)(buffer), int(capacity)), int64(timeout)))
}

//export MieruConnectionWrite
func MieruConnectionWrite(handle C.uint64_t, buffer unsafe.Pointer, length C.int64_t, timeout C.int64_t) C.int64_t {
	if buffer == nil || length <= 0 || length > bridge.MaxBufferSize {
		return C.int64_t(bridge.Invalid)
	}
	return C.int64_t(registry.Write(uint64(handle), unsafe.Slice((*byte)(buffer), int(length)), int64(timeout)))
}

//export MieruConnectionClose
func MieruConnectionClose(handle C.uint64_t) {
	registry.Close(uint64(handle))
}

func main() {}
