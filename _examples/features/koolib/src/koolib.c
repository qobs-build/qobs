#include <stdio.h>
#include "koolib.h"

void koolib() {
#ifdef KOOL
    puts("Kool");
#elif KOOLER
    puts("Kooler");
#endif
}
