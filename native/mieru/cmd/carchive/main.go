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
	"encoding/json"
	"time"
	"unsafe"

	"telegramita/mieru/bridge"
)

var registry = bridge.NewRegistry()

func writeJSON(data []byte, output unsafe.Pointer, capacity C.int64_t) C.int64_t {
	if len(data) > 4*bridge.MaxConfigSize || output == nil || int64(capacity) < int64(len(data)) {
		return C.int64_t(bridge.Invalid)
	}
	copy(unsafe.Slice((*byte)(output), len(data)), data)
	return C.int64_t(len(data))
}

//export MieruConfigNormalize
func MieruConfigNormalize(data *C.char, length C.int64_t, output unsafe.Pointer, capacity C.int64_t) C.int64_t {
	if data == nil || length <= 0 || length > bridge.MaxConfigSize {
		return C.int64_t(bridge.Invalid)
	}
	input := C.GoBytes(unsafe.Pointer(data), C.int(length))
	defer clear(input)
	normalized, err := bridge.NormalizeConfiguration(input)
	defer clear(normalized)
	var response []byte
	if err != nil {
		response, _ = json.Marshal(map[string]interface{}{"error": bridge.Diagnostic{Stage: "configuration", Code: "configuration", Message: err.Error()}})
	} else {
		response, _ = json.Marshal(map[string]interface{}{"configuration": json.RawMessage(normalized)})
	}
	defer clear(response)
	return writeJSON(response, output, capacity)
}

//export MieruClientDiagnostic
func MieruClientDiagnostic(handle C.uint64_t, output unsafe.Pointer, capacity C.int64_t) C.int64_t {
	return writeJSON(registry.Diagnostic(uint64(handle)), output, capacity)
}

//export MieruConnectionDiagnostic
func MieruConnectionDiagnostic(handle C.uint64_t, output unsafe.Pointer, capacity C.int64_t) C.int64_t {
	return writeJSON(registry.ConnectionDiagnostic(uint64(handle)), output, capacity)
}

//export MieruProbeNew
func MieruProbeNew() C.uint64_t { return C.uint64_t(registry.NewProbe()) }

//export MieruProbeCancel
func MieruProbeCancel(handle C.uint64_t) { registry.CancelProbe(uint64(handle)) }

//export MieruProbeRun
func MieruProbeRun(handle C.uint64_t, data *C.char, length C.int64_t, host *C.char, port C.uint16_t, timeout C.int64_t, output unsafe.Pointer, capacity C.int64_t) C.int64_t {
	defer registry.CancelProbe(uint64(handle))
	if data == nil || length <= 0 || length > bridge.MaxConfigSize {
		return C.int64_t(bridge.Invalid)
	}
	n := C.mieru_host_length(host)
	if n == 0 || n > 255 || timeout <= 0 || timeout > 30000 {
		return C.int64_t(bridge.Invalid)
	}
	input := C.GoBytes(unsafe.Pointer(data), C.int(length))
	defer clear(input)
	result := registry.RunProbe(uint64(handle), input, C.GoStringN(host, C.int(n)), uint16(port), time.Duration(timeout)*time.Millisecond)
	response, _ := json.Marshal(result)
	return writeJSON(response, output, capacity)
}

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
