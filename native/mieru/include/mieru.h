#ifndef TELEGRAMITA_MIERU_H
#define TELEGRAMITA_MIERU_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

enum {
    MIERU_ERROR_INVALID = -1,
    MIERU_ERROR_CLOSED = -2,
    MIERU_ERROR_TIMEOUT = -3,
    MIERU_ERROR_NETWORK = -4
};

uint64_t MieruClientNew(void);
int64_t MieruConfigNormalize(const char *data, int64_t length, void *output, int64_t capacity);
int64_t MieruClientDiagnostic(uint64_t client, void *output, int64_t capacity);
int64_t MieruConnectionDiagnostic(uint64_t connection, void *output, int64_t capacity);
uint64_t MieruProbeNew(void);
void MieruProbeCancel(uint64_t probe);
int64_t MieruProbeRun(uint64_t probe, const char *data, int64_t length, const char *host, uint16_t port, int64_t timeout_ms, void *output, int64_t capacity);
int32_t MieruClientStore(uint64_t client, const char *json, int64_t length);
int32_t MieruClientStart(uint64_t client);
void MieruClientStop(uint64_t client);
void MieruClientFree(uint64_t client);
uint64_t MieruConnectionNew(uint64_t client);
int32_t MieruConnectionDial(uint64_t connection, const char *host, uint16_t port, int64_t timeout_ms);
int64_t MieruConnectionRead(uint64_t connection, void *buffer, int64_t capacity, int64_t timeout_ms);
int64_t MieruConnectionWrite(uint64_t connection, const void *buffer, int64_t length, int64_t timeout_ms);
void MieruConnectionClose(uint64_t connection);

#ifdef __cplusplus
}
#endif

#endif
