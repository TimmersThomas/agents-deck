#include <stdio.h>
#include <string.h>

#include "cards_core.h"

static int hexval(char c) {
	if (c >= '0' && c <= '9') return c - '0';
	if (c >= 'a' && c <= 'f') return c - 'a' + 10;
	if (c >= 'A' && c <= 'F') return c - 'A' + 10;
	return -1;
}

static bool read_u4(const char *s, int len, int i, uint32_t *out) {
	if (i + 4 > len) return false;
	uint32_t v = 0;
	for (int k = 0; k < 4; k++) {
		int h = hexval(s[i + k]);
		if (h < 0) return false;
		v = (v << 4) | (uint32_t)h;
	}
	*out = v;
	return true;
}

static size_t put_utf8(uint32_t cp, char *d) {
	if (cp == 0) cp = 0xFFFD;
	if (cp < 0x80) {
		d[0] = (char)cp;
		return 1;
	}
	if (cp < 0x800) {
		d[0] = (char)(0xC0 | (cp >> 6));
		d[1] = (char)(0x80 | (cp & 0x3F));
		return 2;
	}
	if (cp < 0x10000) {
		d[0] = (char)(0xE0 | (cp >> 12));
		d[1] = (char)(0x80 | ((cp >> 6) & 0x3F));
		d[2] = (char)(0x80 | (cp & 0x3F));
		return 3;
	}
	d[0] = (char)(0xF0 | (cp >> 18));
	d[1] = (char)(0x80 | ((cp >> 12) & 0x3F));
	d[2] = (char)(0x80 | ((cp >> 6) & 0x3F));
	d[3] = (char)(0x80 | (cp & 0x3F));
	return 4;
}

static size_t seq_len(unsigned char lead) {
	if (lead < 0x80) return 1;
	if (lead >= 0xC2 && lead <= 0xDF) return 2;
	if (lead >= 0xE0 && lead <= 0xEF) return 3;
	if (lead >= 0xF0 && lead <= 0xF4) return 4;
	return 0;
}

// raw copies one UTF-8 sequence from s[*i..] into tmp, or U+FFFD for malformed bytes.
static size_t raw(const char *s, int len, int *i, char *tmp) {
	size_t l = seq_len((unsigned char)s[*i]);
	bool ok = l > 0 && *i + (int)l <= len;
	for (size_t k = 1; ok && k < l; k++) ok = ((unsigned char)s[*i + k] & 0xC0) == 0x80;
	if (!ok) {
		(*i)++;
		return put_utf8(0xFFFD, tmp);
	}
	memcpy(tmp, s + *i, l);
	*i += (int)l;
	return l;
}

static bool escape(const char *s, int len, int *i, uint32_t *cp) {
	if (*i + 1 >= len) return false;
	char e = s[*i + 1];
	*i += 2;
	switch (e) {
	case '"': *cp = '"'; return true;
	case '\\': *cp = '\\'; return true;
	case '/': *cp = '/'; return true;
	case 'b': *cp = '\b'; return true;
	case 'f': *cp = '\f'; return true;
	case 'n': *cp = '\n'; return true;
	case 'r': *cp = '\r'; return true;
	case 't': *cp = '\t'; return true;
	case 'u': break;
	default: return false;
	}
	if (!read_u4(s, len, *i, cp)) return false;
	*i += 4;
	if (*cp >= 0xD800 && *cp <= 0xDBFF) {
		uint32_t lo;
		if (*i + 6 <= len && s[*i] == '\\' && s[*i + 1] == 'u' && read_u4(s, len, *i + 2, &lo) &&
		    lo >= 0xDC00 && lo <= 0xDFFF) {
			*cp = 0x10000 + ((*cp - 0xD800) << 10) + (lo - 0xDC00);
			*i += 6;
		} else {
			*cp = 0xFFFD;
		}
	} else if (*cp >= 0xDC00 && *cp <= 0xDFFF) {
		*cp = 0xFFFD;
	}
	return true;
}

bool json_unescape(const char *s, int len, char *out, size_t cap, bool *truncated) {
	size_t o = 0;
	int i = 0;
	*truncated = false;
	if (cap == 0) return false;
	while (i < len) {
		char tmp[4];
		size_t n;
		if (s[i] != '\\') {
			n = raw(s, len, &i, tmp);
		} else {
			uint32_t cp;
			if (!escape(s, len, &i, &cp)) return false;
			n = put_utf8(cp, tmp);
		}
		if (o + n >= cap) {
			*truncated = true;
			break;
		}
		memcpy(out + o, tmp, n);
		o += n;
	}
	out[o] = '\0';
	return true;
}

void age_format(int64_t age_ms, char *out, size_t n) {
	long long s = age_ms < 0 ? 0 : (long long)(age_ms / 1000);
	if (s < 60) snprintf(out, n, "%llds", s);
	else if (s < 3600) snprintf(out, n, "%lldm", s / 60);
	else if (s < 86400) snprintf(out, n, "%lldh", s / 3600);
	else snprintf(out, n, "%lldd", s / 86400);
}

int64_t card_age_ms(const frame_t *f, const card_t *c, int64_t since_rx_ms) {
	if (since_rx_ms < 0) since_rx_ms = 0;
	int64_t a = f->now_ms - c->since_ms + since_rx_ms;
	return a < 0 ? 0 : a;
}

const char *status_label(card_status_t s) {
	switch (s) {
	case STATUS_WORKING: return "working";
	case STATUS_WAITING: return "needs input";
	case STATUS_IDLE: return "idle";
	case STATUS_ERROR: return "error";
	case STATUS_DONE: return "done";
	}
	return "?";
}

static bool safe_id_char(char c) {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-' ||
	       c == '_' || c == '.' || c == ':';
}

int action_format(char *out, size_t n, const char *id) {
	size_t len = id ? strlen(id) : 0;
	if (len == 0 || len >= CARD_ID_MAX) return -1;
	for (size_t i = 0; i < len; i++)
		if (!safe_id_char(id[i])) return -1;
	int w = snprintf(out, n, "{\"v\":1,\"t\":\"action\",\"a\":\"focus\",\"id\":\"%s\"}\n", id);
	return (w < 0 || (size_t)w >= n) ? -1 : w;
}
