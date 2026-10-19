#include <errno.h>
#include <stdlib.h>
#include <string.h>

#define JSMN_STATIC
#define JSMN_STRICT
#include "jsmn.h"

#include "cards_core.h"

#define MAX_TOKENS 256

static jsmntok_t toks[MAX_TOKENS];
static frame_t scratch;

typedef struct {
	const char *js;
	int n;
} doc_t;

// skip returns the index after token i and all its descendants.
static int skip(const jsmntok_t *t, int i, int n) {
	int pending = 1;
	while (pending > 0 && i < n) {
		pending += t[i].size - 1;
		i++;
	}
	return i;
}

static bool str_is(const doc_t *d, const jsmntok_t *t, const char *s) {
	size_t len = strlen(s);
	return t->type == JSMN_STRING && (size_t)(t->end - t->start) == len && memcmp(d->js + t->start, s, len) == 0;
}

static bool to_i64(const doc_t *d, const jsmntok_t *t, int64_t *out) {
	if (t->type != JSMN_PRIMITIVE) return false;
	int n = t->end - t->start;
	if (n <= 0 || n > 20) return false;
	char b[24];
	memcpy(b, d->js + t->start, (size_t)n);
	b[n] = '\0';
	if (!(b[0] == '-' || (b[0] >= '0' && b[0] <= '9'))) return false;
	char *end;
	errno = 0;
	long long v = strtoll(b, &end, 10);
	if (errno || *end) return false;
	*out = v;
	return true;
}

static card_status_t to_status(const doc_t *d, const jsmntok_t *t) {
	if (str_is(d, t, "working")) return STATUS_WORKING;
	if (str_is(d, t, "waiting")) return STATUS_WAITING;
	if (str_is(d, t, "error")) return STATUS_ERROR;
	if (str_is(d, t, "done")) return STATUS_DONE;
	return STATUS_IDLE;
}

static frame_result_t read_card(const doc_t *d, const jsmntok_t *t, int i, card_t *c, int *next) {
	if (t[i].type != JSMN_OBJECT) return FRAME_BAD_FIELD;
	memset(c, 0, sizeof *c);
	c->status = STATUS_IDLE;
	bool has_id = false, has_since = false;
	int j = i + 1;
	for (int k = 0; k < t[i].size; k++) {
		if (j + 1 >= d->n || t[j].type != JSMN_STRING) return FRAME_BAD_JSON;
		const jsmntok_t *val = &t[j + 1];
		bool cut = false;
		if (str_is(d, &t[j], "id")) {
			if (val->type != JSMN_STRING ||
			    !json_unescape(d->js + val->start, val->end - val->start, c->id, sizeof c->id, &cut) || cut ||
			    c->id[0] == '\0')
				return FRAME_BAD_FIELD;
			has_id = true;
		} else if (str_is(d, &t[j], "title")) {
			if (val->type != JSMN_STRING ||
			    !json_unescape(d->js + val->start, val->end - val->start, c->title, sizeof c->title, &cut))
				return FRAME_BAD_FIELD;
		} else if (str_is(d, &t[j], "status")) {
			c->status = to_status(d, val);
		} else if (str_is(d, &t[j], "since_ms")) {
			if (!to_i64(d, val, &c->since_ms)) return FRAME_BAD_FIELD;
			has_since = true;
		}
		j = skip(t, j + 1, d->n);
	}
	*next = j;
	return (has_id && has_since) ? FRAME_OK : FRAME_BAD_FIELD;
}

static frame_result_t read_cards(const doc_t *d, const jsmntok_t *t, int i, frame_t *f) {
	if (t[i].type != JSMN_ARRAY) return FRAME_BAD_FIELD;
	if (t[i].size > CARDS_MAX) return FRAME_TOO_MANY;
	int j = i + 1;
	for (int m = 0; m < t[i].size; m++) {
		frame_result_t r = read_card(d, t, j, &f->cards[m], &j);
		if (r != FRAME_OK) return r;
	}
	f->count = t[i].size;
	return FRAME_OK;
}

typedef struct {
	bool v, t, now, cards;
	int64_t total;
} seen_t;

static frame_result_t read_top(const doc_t *d, const jsmntok_t *t, seen_t *s) {
	int j = 1;
	for (int k = 0; k < t[0].size; k++) {
		if (j + 1 >= d->n || t[j].type != JSMN_STRING) return FRAME_BAD_JSON;
		const jsmntok_t *val = &t[j + 1];
		int64_t v;
		if (str_is(d, &t[j], "v")) {
			if (!to_i64(d, val, &v) || v != 1) return FRAME_BAD_VERSION;
			s->v = true;
		} else if (str_is(d, &t[j], "t")) {
			if (!str_is(d, val, "state")) return FRAME_NOT_STATE;
			s->t = true;
		} else if (str_is(d, &t[j], "now_ms")) {
			if (!to_i64(d, val, &scratch.now_ms)) return FRAME_BAD_FIELD;
			s->now = true;
		} else if (str_is(d, &t[j], "total")) {
			if (!to_i64(d, val, &s->total) || s->total < 0 || s->total > 1000000) return FRAME_BAD_FIELD;
		} else if (str_is(d, &t[j], "cards")) {
			frame_result_t r = read_cards(d, t, j + 1, &scratch);
			if (r != FRAME_OK) return r;
			s->cards = true;
		}
		j = skip(t, j + 1, d->n);
	}
	return FRAME_OK;
}

frame_result_t frame_parse(const char *line, size_t len, frame_t *out) {
	if (len == 0 || len > LINE_MAX_BYTES) return FRAME_BAD_JSON;
	jsmn_parser p;
	jsmn_init(&p);
	int n = jsmn_parse(&p, line, len, toks, MAX_TOKENS);
	if (n < 1 || toks[0].type != JSMN_OBJECT) return FRAME_BAD_JSON;
	if (toks[0].end != (int)len) return FRAME_BAD_JSON;
	doc_t d = {.js = line, .n = n};
	memset(&scratch, 0, sizeof scratch);
	seen_t seen = {.total = -1};
	frame_result_t r = read_top(&d, toks, &seen);
	if (r != FRAME_OK) return r;
	if (!seen.v) return FRAME_BAD_VERSION;
	if (!seen.t) return FRAME_NOT_STATE;
	if (!seen.now || !seen.cards) return FRAME_BAD_FIELD;
	scratch.total = seen.total < scratch.count ? scratch.count : (int)seen.total;
	*out = scratch;
	return FRAME_OK;
}

const char *frame_result_name(frame_result_t r) {
	switch (r) {
	case FRAME_OK: return "ok";
	case FRAME_BAD_JSON: return "bad json";
	case FRAME_BAD_VERSION: return "bad version";
	case FRAME_NOT_STATE: return "not a state frame";
	case FRAME_BAD_FIELD: return "bad field";
	case FRAME_TOO_MANY: return "too many cards";
	}
	return "unknown";
}
