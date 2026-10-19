#ifndef TEST_H
#define TEST_H

#include <stdio.h>

extern int failures;

#define CHECK(cond)                                                       \
	do {                                                                  \
		if (!(cond)) {                                                    \
			printf("FAIL %s:%d: %s\n", __FILE__, __LINE__, #cond);        \
			failures++;                                                   \
		}                                                                 \
	} while (0)

void run_frame_tests(void);
void run_linebuf_tests(void);
void run_text_tests(void);

#endif
