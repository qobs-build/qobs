#include <stdio.h>

#define str(a) str2(a)
#define str2(a) #a

int main(void) {
    puts(str(PLATFORM_STRING));
    return 0;
}
