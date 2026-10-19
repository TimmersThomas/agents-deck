#include <stdlib.h>
#include <string.h>

#include "cards_core.h"
#include "test.h"

static const char GOOD[] =
	"{\"v\":1,\"t\":\"state\",\"now_ms\":1792399124168,\"total\":2,\"cards\":["
	"{\"id\":\"0000BBBB-0000-4000-8000-000000000004\",\"title\":\"DEV-24-delta\",\"status\":\"waiting\",\"since_ms\":1792317660000},"
	"{\"id\":\"0000BBBB-0000-4000-8000-000000000001\",\"title\":\"a\\u003cb\\\"c\",\"status\":\"working\",\"since_ms\":5}]}";

static frame_result_t parse(const char *s, frame_t *f) { return frame_parse(s, strlen(s), f); }

static void test_good_frame(void) {
	frame_t f;
	CHECK(parse(GOOD, &f) == FRAME_OK);
	CHECK(f.now_ms == 1792399124168LL);
	CHECK(f.total == 2 && f.count == 2);
	CHECK(strcmp(f.cards[0].id, "0000BBBB-0000-4000-8000-000000000004") == 0);
	CHECK(strcmp(f.cards[0].title, "DEV-24-delta") == 0);
	CHECK(f.cards[0].status == STATUS_WAITING);
	CHECK(f.cards[0].since_ms == 1792317660000LL);
	CHECK(strcmp(f.cards[1].title, "a<b\"c") == 0);
	CHECK(f.cards[1].status == STATUS_WORKING);
}

static void test_all_statuses_and_unknown(void) {
	frame_t f;
	const char *names[] = {"working", "waiting", "idle", "error", "done", "from-the-future"};
	card_status_t want[] = {STATUS_WORKING, STATUS_WAITING, STATUS_IDLE, STATUS_ERROR, STATUS_DONE, STATUS_IDLE};
	for (int i = 0; i < 6; i++) {
		char line[256];
		snprintf(line, sizeof line,
			"{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":1,\"cards\":[{\"id\":\"a\",\"title\":\"t\",\"status\":\"%s\",\"since_ms\":1}]}",
			names[i]);
		CHECK(parse(line, &f) == FRAME_OK);
		CHECK(f.cards[0].status == want[i]);
	}
}

static void test_empty_and_total(void) {
	frame_t f;
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":9,\"total\":0,\"cards\":[]}", &f) == FRAME_OK);
	CHECK(f.count == 0 && f.total == 0 && f.now_ms == 9);
	// total smaller than the cards sent is raised to the count
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":9,\"total\":0,\"cards\":[{\"id\":\"a\",\"title\":\"\",\"status\":\"idle\",\"since_ms\":1}]}", &f) == FRAME_OK);
	CHECK(f.total == 1);
	// total larger than the cards sent is kept (the cap was applied by the bridge)
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":9,\"total\":40,\"cards\":[]}", &f) == FRAME_OK);
	CHECK(f.total == 40);
}

static void test_escapes_and_unicode(void) {
	frame_t f;
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":1,\"cards\":[{\"id\":\"a\",\"title\":\"\\u00e9\\ud83d\\ude00\\n\\/\",\"status\":\"idle\",\"since_ms\":1}]}", &f) == FRAME_OK);
	CHECK(strcmp(f.cards[0].title, "\xC3\xA9\xF0\x9F\x98\x80\n/") == 0);
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":1,\"cards\":[{\"id\":\"a\",\"title\":\"x\\ud83dy\",\"status\":\"idle\",\"since_ms\":1}]}", &f) == FRAME_OK);
	CHECK(strcmp(f.cards[0].title, "x\xEF\xBF\xBD" "y") == 0);
}

static void test_title_truncation(void) {
	frame_t f;
	char line[1024], title[400];
	memset(title, 'x', 300);
	title[300] = 0;
	snprintf(line, sizeof line, "{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":1,\"cards\":[{\"id\":\"a\",\"title\":\"%s\",\"status\":\"idle\",\"since_ms\":1}]}", title);
	CHECK(parse(line, &f) == FRAME_OK);
	CHECK(strlen(f.cards[0].title) == CARD_TITLE_MAX - 1);

	// a 2-byte character must not be cut in half
	char *p = title;
	*p++ = 'a';
	for (int i = 0; i < 100; i++) {
		*p++ = '\xC3';
		*p++ = '\xA9';
	}
	*p = 0;
	snprintf(line, sizeof line, "{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":1,\"cards\":[{\"id\":\"a\",\"title\":\"%s\",\"status\":\"idle\",\"since_ms\":1}]}", title);
	CHECK(parse(line, &f) == FRAME_OK);
	size_t n = strlen(f.cards[0].title);
	CHECK(n == 1 + 2 * 95);
	CHECK((unsigned char)f.cards[0].title[n - 2] == 0xC3 && (unsigned char)f.cards[0].title[n - 1] == 0xA9);
}

static void test_rejects(void) {
	frame_t f;
	CHECK(parse("not json", &f) == FRAME_BAD_JSON);
	CHECK(parse("", &f) == FRAME_BAD_JSON);
	CHECK(parse("[1,2]", &f) == FRAME_BAD_JSON);
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":0,\"cards\":[]}garbage", &f) == FRAME_BAD_JSON);
	CHECK(parse("I (1234) example: boot", &f) == FRAME_BAD_JSON);
	CHECK(parse("{\"v\":2,\"t\":\"state\",\"now_ms\":1,\"total\":0,\"cards\":[]}", &f) == FRAME_BAD_VERSION);
	CHECK(parse("{\"t\":\"state\",\"now_ms\":1,\"total\":0,\"cards\":[]}", &f) == FRAME_BAD_VERSION);
	CHECK(parse("{\"v\":1.5,\"t\":\"state\",\"now_ms\":1,\"total\":0,\"cards\":[]}", &f) == FRAME_BAD_VERSION);
	CHECK(parse("{\"v\":1,\"t\":\"action\",\"a\":\"focus\",\"id\":\"x\"}", &f) == FRAME_NOT_STATE);
	CHECK(parse("{\"v\":1,\"now_ms\":1,\"total\":0,\"cards\":[]}", &f) == FRAME_NOT_STATE);
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"total\":0,\"cards\":[]}", &f) == FRAME_BAD_FIELD);
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":0}", &f) == FRAME_BAD_FIELD);
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":\"1\",\"total\":0,\"cards\":[]}", &f) == FRAME_BAD_FIELD);
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":0,\"cards\":{}}", &f) == FRAME_BAD_FIELD);
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":1,\"cards\":[{\"title\":\"x\",\"status\":\"idle\",\"since_ms\":1}]}", &f) == FRAME_BAD_FIELD);
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":1,\"cards\":[{\"id\":\"a\",\"title\":\"x\",\"status\":\"idle\"}]}", &f) == FRAME_BAD_FIELD);
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":1,\"cards\":[{\"id\":\"\",\"title\":\"x\",\"status\":\"idle\",\"since_ms\":1}]}", &f) == FRAME_BAD_FIELD);
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":1,\"cards\":[5]}", &f) == FRAME_BAD_FIELD);
}

static void test_id_too_long_is_rejected_not_truncated(void) {
	frame_t f;
	char line[512], id[100];
	memset(id, 'a', 65);
	id[65] = 0;
	snprintf(line, sizeof line, "{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":1,\"cards\":[{\"id\":\"%s\",\"title\":\"x\",\"status\":\"idle\",\"since_ms\":1}]}", id);
	CHECK(parse(line, &f) == FRAME_BAD_FIELD);
	id[64] = 0;
	snprintf(line, sizeof line, "{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":1,\"cards\":[{\"id\":\"%s\",\"title\":\"x\",\"status\":\"idle\",\"since_ms\":1}]}", id);
	CHECK(parse(line, &f) == FRAME_OK && strlen(f.cards[0].id) == 64);
}

static void test_too_many_cards(void) {
	static char line[4096];
	frame_t f;
	int n = snprintf(line, sizeof line, "{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":17,\"cards\":[");
	for (int i = 0; i < 17; i++)
		n += snprintf(line + n, sizeof line - n, "%s{\"id\":\"c%d\",\"title\":\"t\",\"status\":\"idle\",\"since_ms\":1}", i ? "," : "", i);
	snprintf(line + n, sizeof line - n, "]}");
	CHECK(parse(line, &f) == FRAME_TOO_MANY);
}

static void test_unknown_fields_are_ignored(void) {
	frame_t f;
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"x\":{\"a\":[1,2,{\"b\":null}]},\"now_ms\":1,\"total\":1,\"cards\":[{\"id\":\"a\",\"extra\":[1,{\"c\":2}],\"title\":\"t\",\"status\":\"idle\",\"since_ms\":1}]}", &f) == FRAME_OK);
	CHECK(f.count == 1 && strcmp(f.cards[0].title, "t") == 0);
}

static void test_out_is_untouched_on_failure(void) {
	frame_t f;
	memset(&f, 0x5A, sizeof f);
	CHECK(parse("{\"v\":1,\"t\":\"state\",\"now_ms\":1,\"total\":1,\"cards\":[{\"id\":\"a\"}]}", &f) == FRAME_BAD_FIELD);
	CHECK(((unsigned char *)&f)[0] == 0x5A && ((unsigned char *)&f)[sizeof f - 1] == 0x5A);
}

// The largest frame the bridge can send: 16 cards, 64-byte ids and 48-rune titles made of
// characters that JSON escapes to 6 bytes each (about 11.9 KB).
static void test_worst_case_frame(void) {
	static char line[LINE_MAX_BYTES];
	frame_t f;
	char id[64 * 6 + 1], title[48 * 6 + 1];
	id[0] = title[0] = 0;
	for (int i = 0; i < 64; i++) strcat(id, "\\u003c");
	for (int i = 0; i < 48; i++) strcat(title, "\\u003c");
	int n = snprintf(line, sizeof line, "{\"v\":1,\"t\":\"state\",\"now_ms\":1792399124168,\"total\":16,\"cards\":[");
	for (int i = 0; i < 16; i++)
		n += snprintf(line + n, sizeof line - n, "%s{\"id\":\"%s\",\"title\":\"%s\",\"status\":\"waiting\",\"since_ms\":1792399124168}", i ? "," : "", id, title);
	n += snprintf(line + n, sizeof line - n, "]}");
	CHECK(n > 10000 && n < LINE_MAX_BYTES);
	CHECK(frame_parse(line, (size_t)n, &f) == FRAME_OK);
	CHECK(f.count == 16 && strlen(f.cards[15].title) == 48 && f.cards[15].title[0] == '<' && strlen(f.cards[15].id) == 64);
}

static void test_result_names(void) {
	CHECK(strcmp(frame_result_name(FRAME_OK), "ok") == 0);
	CHECK(strcmp(frame_result_name(FRAME_TOO_MANY), "too many cards") == 0);
	CHECK(strcmp(frame_result_name((frame_result_t)99), "unknown") == 0);
}

void run_frame_tests(void) {
	test_good_frame();
	test_all_statuses_and_unknown();
	test_empty_and_total();
	test_escapes_and_unicode();
	test_title_truncation();
	test_rejects();
	test_id_too_long_is_rejected_not_truncated();
	test_too_many_cards();
	test_unknown_fields_are_ignored();
	test_out_is_untouched_on_failure();
	test_worst_case_frame();
	test_result_names();
}
