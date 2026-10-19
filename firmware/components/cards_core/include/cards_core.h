// Pure C core of the display firmware: no ESP-IDF dependencies, unit-tested on the host.
#ifndef CARDS_CORE_H
#define CARDS_CORE_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define CARDS_MAX 16
#define CARD_ID_MAX 65
#define CARD_TITLE_MAX 193
#define LINE_MAX_BYTES 12288

typedef enum {
	STATUS_WORKING,
	STATUS_WAITING,
	STATUS_IDLE,
	STATUS_ERROR,
	STATUS_DONE,
} card_status_t;

typedef struct {
	char id[CARD_ID_MAX];
	char title[CARD_TITLE_MAX];
	card_status_t status;
	int64_t since_ms;
} card_t;

typedef struct {
	int64_t now_ms;
	int total;
	int count;
	card_t cards[CARDS_MAX];
} frame_t;

typedef enum {
	FRAME_OK = 0,
	FRAME_BAD_JSON,
	FRAME_BAD_VERSION,
	FRAME_NOT_STATE,
	FRAME_BAD_FIELD,
	FRAME_TOO_MANY,
} frame_result_t;

// frame_parse parses one state frame line. out is written only on FRAME_OK.
// It uses static scratch memory and is not re-entrant: call it from one task.
frame_result_t frame_parse(const char *line, size_t len, frame_t *out);
const char *frame_result_name(frame_result_t r);

// linebuf assembles bytes into lines of at most LINE_MAX_BYTES-1 bytes.
typedef struct {
	char buf[LINE_MAX_BYTES];
	size_t len;
	bool overflow;
	bool ready;
} linebuf_t;

void linebuf_init(linebuf_t *lb);
// linebuf_push feeds one byte. It returns true when a complete non-empty line is in
// lb->buf (NUL-terminated, CR/LF stripped, *len bytes). Longer lines are dropped whole.
bool linebuf_push(linebuf_t *lb, char c, size_t *len);

// json_unescape decodes a JSON string body into UTF-8 in out (NUL-terminated). Text that
// does not fit is cut on a character boundary and *truncated is set. It returns false on
// an invalid escape. Malformed UTF-8 becomes U+FFFD.
bool json_unescape(const char *s, int len, char *out, size_t cap, bool *truncated);

void age_format(int64_t age_ms, char *out, size_t n);
int64_t card_age_ms(const frame_t *f, const card_t *c, int64_t since_rx_ms);
const char *status_label(card_status_t s);

// action_format writes the focus action line for a card id (with trailing newline) and
// returns its length, or -1 for an empty, overlong or unsafe id or a too small buffer.
int action_format(char *out, size_t n, const char *id);

#endif
