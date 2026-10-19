#include "cards_core.h"

void linebuf_init(linebuf_t *lb) {
	lb->len = 0;
	lb->overflow = false;
	lb->ready = false;
}

bool linebuf_push(linebuf_t *lb, char c, size_t *len) {
	if (lb->ready) {
		lb->len = 0;
		lb->ready = false;
	}
	if (c != '\n') {
		if (lb->overflow) return false;
		if (lb->len >= LINE_MAX_BYTES - 1) {
			lb->overflow = true;
			return false;
		}
		lb->buf[lb->len++] = c;
		return false;
	}
	bool dropped = lb->overflow;
	lb->overflow = false;
	if (lb->len > 0 && lb->buf[lb->len - 1] == '\r') lb->len--;
	if (dropped || lb->len == 0) {
		lb->len = 0;
		return false;
	}
	lb->buf[lb->len] = '\0';
	*len = lb->len;
	lb->ready = true;
	return true;
}
