#include <stdio.h>

int main(void) {
#ifdef CATS
    puts("feature 'cats' is enabled");
#endif
#ifdef DOGS
    puts("feature 'dogs' is enabled");
#endif
#ifdef WHALES
    puts("feature 'whales' is enabled");
#endif
#ifdef CATS_AND_DOGS
    puts("both features 'cats' and 'dogs' are enabled");
#endif
#if !defined(CATS) && !defined(DOGS) && !defined(WHALES)
    puts("no features are enabled");
#endif
    return 0;
}
