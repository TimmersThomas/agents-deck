#include <string.h>

#include "cards_core.h"
#include "test.h"

static linebuf_t lb;

// feed pushes s and collects completed lines separated by '|'.
static void feed(const char *s, char *out, size_t cap) {
	out[0] = 0;
	for (; *s; s++) {
		size_t len;
		if (linebuf_push(&lb, *s, &len)) {
			CHECK(strlen(lb.buf) == len);
			if (out[0]) strncat(out, "|", cap - strlen(out) - 1);
			strncat(out, lb.buf, cap - strlen(out) - 1);
		}
	}
}

static void test_lines(void) {
	char out[256];
	linebuf_init(&lb);
	feed("a\nbb\n", out, sizeof out);
	CHECK(strcmp(out, "a|bb") == 0);
	feed("\r\n\n\nc\r\n", out, sizeof out);
	CHECK(strcmp(out, "c") == 0);
}

static void test_partial_line_waits(void) {
	char out[256];
	linebuf_init(&lb);
	feed("{\"v\"", out, sizeof out);
	CHECK(out[0] == 0);
	feed(":1}\n", out, sizeof out);
	CHECK(strcmp(out, "{\"v\":1}") == 0);
}

static void test_resync_newline_prefix(void) {
	char out[256];
	linebuf_init(&lb);
	feed("half a li", out, sizeof out);
	feed("\n{\"ok\":1}\n", out, sizeof out);
	CHECK(strcmp(out, "half a li|{\"ok\":1}") == 0 || strcmp(out, "{\"ok\":1}") == 0);
}

static void test_overlong_line_is_dropped_whole(void) {
	linebuf_init(&lb);
	size_t len;
	for (int i = 0; i < LINE_MAX_BYTES + 500; i++) CHECK(!linebuf_push(&lb, 'x', &len));
	CHECK(!linebuf_push(&lb, '\n', &len));
	char out[64];
	feed("ok\n", out, sizeof out);
	CHECK(strcmp(out, "ok") == 0);
}

static void test_longest_accepted_line(void) {
	linebuf_init(&lb);
	size_t len = 0;
	bool done = false;
	for (int i = 0; i < LINE_MAX_BYTES - 1; i++) CHECK(!linebuf_push(&lb, 'y', &len));
	done = linebuf_push(&lb, '\n', &len);
	CHECK(done && len == LINE_MAX_BYTES - 1 && lb.buf[len] == 0);
}

void run_linebuf_tests(void) {
	test_lines();
	test_partial_line_waits();
	test_resync_newline_prefix();
	test_overlong_line_is_dropped_whole();
	test_longest_accepted_line();
}
