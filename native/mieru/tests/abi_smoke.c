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
