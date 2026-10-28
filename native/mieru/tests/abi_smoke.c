#include "mieru.h"
#include <stdio.h>
#include <string.h>

static int verify(int condition, const char *operation) {
    if (!condition) {
        fprintf(stderr, "Mieru C ABI smoke failed: %s\n", operation);
        return 0;
    }
    return 1;
}

int main(void) {
    uint64_t client = MieruClientNew();
    int passed = verify(client != 0, "create client");
    const char *invalid = "{\"unknown\":true}";
    passed &= verify(MieruClientStore(client, invalid, (int64_t)strlen(invalid)) == MIERU_ERROR_INVALID, "reject invalid config");
    char output[8192] = {0};
    int64_t count = MieruClientDiagnostic(client, output, sizeof(output) - 1);
    passed &= verify(count > 0 && strstr(output, "configuration") != NULL, "safe client diagnostic");
    const char *clash = "{\"name\":\"abi\",\"type\":\"mieru\",\"server\":\"localhost\",\"port\":443,\"username\":\"test\",\"password\":\"dummy\"}";
    memset(output, 0, sizeof(output));
    count = MieruConfigNormalize(clash, (int64_t)strlen(clash), output, sizeof(output) - 1);
    passed &= verify(count > 0 && strstr(output, "activeProfile") != NULL, "normalize Clash profile");
    uint64_t probe = MieruProbeNew();
    MieruProbeCancel(probe);
    memset(output, 0, sizeof(output));
    count = MieruProbeRun(probe, clash, (int64_t)strlen(clash), "127.0.0.1", 443, 1000, output, sizeof(output) - 1);
    passed &= verify(count > 0 && strstr(output, "cancelled") != NULL && strstr(output, "\"connectionEstablished\":false") != NULL, "cancelled probe cannot report success");
    passed &= verify(MieruConnectionNew(client) == 0, "reject connection before start");
    MieruClientStop(client);
    passed &= verify(MieruClientStart(client) == MIERU_ERROR_CLOSED, "reject restart after stop");
    MieruClientFree(client);
    MieruClientFree(client);
    unsigned char byte = 0;
    passed &= verify(MieruConnectionRead(0, &byte, 1, 1) == MIERU_ERROR_INVALID, "reject invalid connection");
    MieruConnectionClose(0);
    if (passed) {
        puts("Mieru C ABI smoke PASS");
    }
    return passed ? 0 : 1;
}
