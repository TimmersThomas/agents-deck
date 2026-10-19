#include "test.h"

int failures;

int main(void) {
	run_frame_tests();
	run_linebuf_tests();
	run_text_tests();
	if (failures) {
		printf("%d check(s) failed\n", failures);
		return 1;
	}
	printf("all checks passed\n");
	return 0;
}
