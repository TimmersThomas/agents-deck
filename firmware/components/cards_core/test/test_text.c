#include <string.h>

#include "cards_core.h"
#include "test.h"

static void test_age_format(void) {
	char b[16];
	age_format(-5, b, sizeof b);
	CHECK(strcmp(b, "0s") == 0);
	age_format(0, b, sizeof b);
	CHECK(strcmp(b, "0s") == 0);
	age_format(59999, b, sizeof b);
	CHECK(strcmp(b, "59s") == 0);
	age_format(60000, b, sizeof b);
	CHECK(strcmp(b, "1m") == 0);
	age_format(3599999, b, sizeof b);
	CHECK(strcmp(b, "59m") == 0);
	age_format(3600000, b, sizeof b);
	CHECK(strcmp(b, "1h") == 0);
	age_format(86399999, b, sizeof b);
	CHECK(strcmp(b, "23h") == 0);
	age_format(86400000, b, sizeof b);
	CHECK(strcmp(b, "1d") == 0);
	age_format(3 * 86400000LL, b, sizeof b);
	CHECK(strcmp(b, "3d") == 0);
	age_format(5000, b, 3);
	CHECK(strcmp(b, "5s") == 0);
}

static void test_card_age(void) {
	frame_t f = {.now_ms = 1000000};
	card_t c = {.since_ms = 940000};
	CHECK(card_age_ms(&f, &c, 0) == 60000);
	CHECK(card_age_ms(&f, &c, 5000) == 65000);
	CHECK(card_age_ms(&f, &c, -7) == 60000);
	c.since_ms = 1000100;
	CHECK(card_age_ms(&f, &c, 0) == 0);
}

static void test_status_label(void) {
	CHECK(strcmp(status_label(STATUS_WORKING), "working") == 0);
	CHECK(strcmp(status_label(STATUS_WAITING), "needs input") == 0);
	CHECK(strcmp(status_label(STATUS_IDLE), "idle") == 0);
	CHECK(strcmp(status_label(STATUS_ERROR), "error") == 0);
	CHECK(strcmp(status_label(STATUS_DONE), "done") == 0);
	CHECK(strcmp(status_label((card_status_t)42), "?") == 0);
}

static void test_action_format(void) {
	char b[160];
	int n = action_format(b, sizeof b, "0000BBBB-0000-4000-8000-000000000001");
	CHECK(n > 0 && (size_t)n == strlen(b));
	CHECK(strcmp(b, "{\"v\":1,\"t\":\"action\",\"a\":\"focus\",\"id\":\"0000BBBB-0000-4000-8000-000000000001\"}\n") == 0);
	CHECK(action_format(b, sizeof b, NULL) == -1);
	CHECK(action_format(b, sizeof b, "") == -1);
	CHECK(action_format(b, sizeof b, "a\"b") == -1);
	CHECK(action_format(b, sizeof b, "a b") == -1);
	CHECK(action_format(b, sizeof b, "a\nb") == -1);
	CHECK(action_format(b, sizeof b, "a\\b") == -1);
	char longid[80];
	memset(longid, 'a', 64);
	longid[64] = 0;
	CHECK(action_format(b, sizeof b, longid) > 0);
	memset(longid, 'a', 65);
	longid[65] = 0;
	CHECK(action_format(b, sizeof b, longid) == -1);
	CHECK(action_format(b, 20, "abc") == -1);
}

static void test_unescape_direct(void) {
	char o[16];
	bool t = true;
	CHECK(json_unescape("abc", 3, o, sizeof o, &t) && strcmp(o, "abc") == 0 && !t);
	CHECK(json_unescape("abcdef", 6, o, 4, &t) && strcmp(o, "abc") == 0 && t);
	CHECK(!json_unescape("a\\x", 3, o, sizeof o, &t));
	CHECK(!json_unescape("a\\u12", 5, o, sizeof o, &t));
	CHECK(json_unescape("\\u0000", 6, o, sizeof o, &t) && strcmp(o, "\xEF\xBF\xBD") == 0);
	CHECK(json_unescape("\xFF\xC3", 2, o, sizeof o, &t) && strcmp(o, "\xEF\xBF\xBD\xEF\xBF\xBD") == 0);
	CHECK(json_unescape("\xC3\xA9\xC3\xA9", 4, o, 4, &t) && strcmp(o, "\xC3\xA9") == 0 && t);
}

void run_text_tests(void) {
	test_age_format();
	test_card_age();
	test_status_label();
	test_action_format();
	test_unescape_direct();
}
