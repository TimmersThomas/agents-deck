#include "ui.h"

#include <stdio.h>
#include <string.h>

#include "board.h"
#include "esp_timer.h"
#include "lvgl.h"

#define SCREEN_W 640
#define SCREEN_H 172
#define PAD 8
#define GAP 8
#define CARD_W 200
#define CARD_H (SCREEN_H - 2 * PAD)
#define BAR_W 6
#define CARD_PAD 10
#define TEXT_X 12
#define INNER_W (CARD_W - 2 * CARD_PAD - TEXT_X)
#define TITLE_LINES 3
#define STALE_MS 35000
#define REFRESH_MS 1000

#define COLOR_SCREEN 0x0B0F14
#define COLOR_CARD 0x1F2937
#define COLOR_CARD_PRESSED 0x374151
#define COLOR_CARD_DONE 0x141920
#define COLOR_TEXT 0xE5E7EB
#define COLOR_DIM 0x6B7280

typedef struct {
	lv_obj_t *card, *bar, *title, *status, *age;
} card_widget_t;

static card_widget_t w[CARDS_MAX];
static lv_obj_t *row, *overlay, *overlay_label, *empty_label, *more_label, *stats_label, *touch_label;
static ui_tap_cb_t tap_cb;
static frame_t shown;
static int64_t rx_ms = -1;

static lv_color_t status_color(card_status_t s) {
	switch (s) {
	case STATUS_WORKING: return lv_color_hex(0x3B82F6);
	case STATUS_WAITING: return lv_color_hex(0xF59E0B);
	case STATUS_ERROR: return lv_color_hex(0xEF4444);
	case STATUS_DONE: return lv_color_hex(0x4B5563);
	case STATUS_IDLE: break;
	}
	return lv_color_hex(0x6B7280);
}

static void set_hidden(lv_obj_t *o, bool hidden) {
	lv_obj_set_hidden(o, hidden);
}

static void set_text(lv_obj_t *l, const char *s) {
	if (strcmp(lv_label_get_text(l), s) != 0) lv_label_set_text(l, s);
}

static int64_t local_ms(void) {
	return esp_timer_get_time() / 1000;
}

static void card_clicked(lv_event_t *e) {
	int i = (int)(intptr_t)lv_event_get_user_data(e);
	if (i < shown.count && tap_cb) tap_cb(shown.cards[i].id);
}

static lv_obj_t *make_label(lv_obj_t *parent, const lv_font_t *font, uint32_t color) {
	lv_obj_t *l = lv_label_create(parent);
	lv_obj_set_style_text_font(l, font, 0);
	lv_obj_set_style_text_color(l, lv_color_hex(color), 0);
	return l;
}

static void make_card(int i) {
	card_widget_t *c = &w[i];
	c->card = lv_obj_create(row);
	lv_obj_set_size(c->card, CARD_W, CARD_H);
	lv_obj_set_scrollable(c->card, false);
	lv_obj_set_style_radius(c->card, 12, 0);
	lv_obj_set_style_bg_color(c->card, lv_color_hex(COLOR_CARD), 0);
	lv_obj_set_style_bg_color(c->card, lv_color_hex(COLOR_CARD_PRESSED), LV_STATE_PRESSED);
	lv_obj_set_style_border_width(c->card, 0, 0);
	lv_obj_set_style_pad_all(c->card, CARD_PAD, 0);
	lv_obj_add_event_cb(c->card, card_clicked, LV_EVENT_CLICKED, (void *)(intptr_t)i);

	c->bar = lv_obj_create(c->card);
	lv_obj_set_scrollable(c->bar, false);
	lv_obj_set_size(c->bar, BAR_W, CARD_H - 44);
	lv_obj_set_style_radius(c->bar, BAR_W / 2, 0);
	lv_obj_set_style_border_width(c->bar, 0, 0);
	lv_obj_set_style_pad_all(c->bar, 0, 0);
	lv_obj_align(c->bar, LV_ALIGN_LEFT_MID, -CARD_PAD + 2, 0);

	c->title = make_label(c->card, &lv_font_montserrat_16, COLOR_TEXT);
	lv_label_set_long_mode(c->title, LV_LABEL_LONG_MODE_DOTS);
	lv_obj_set_width(c->title, INNER_W);
	lv_obj_set_height(c->title, TITLE_LINES * lv_font_get_line_height(&lv_font_montserrat_16));
	lv_obj_align(c->title, LV_ALIGN_TOP_LEFT, TEXT_X, 0);

	c->status = make_label(c->card, &lv_font_montserrat_14, COLOR_DIM);
	lv_obj_align(c->status, LV_ALIGN_BOTTOM_LEFT, TEXT_X, 0);
	c->age = make_label(c->card, &lv_font_montserrat_14, COLOR_DIM);
	lv_obj_align(c->age, LV_ALIGN_BOTTOM_RIGHT, 0, 0);
	set_hidden(c->card, true);
}

static void fill_card(int i, const card_t *c, int64_t age_ms) {
	char age[16];
	age_format(age_ms, age, sizeof age);
	set_text(w[i].title, c->title);
	set_text(w[i].status, status_label(c->status));
	set_text(w[i].age, age);
	bool done = c->status == STATUS_DONE;
	lv_obj_set_style_bg_color(w[i].bar, status_color(c->status), 0);
	lv_obj_set_style_bg_color(w[i].card, lv_color_hex(done ? COLOR_CARD_DONE : COLOR_CARD), 0);
	lv_obj_set_style_text_color(w[i].title, lv_color_hex(done ? COLOR_DIM : COLOR_TEXT), 0);
	lv_obj_set_style_text_color(w[i].status, status_color(c->status), 0);
}

// refresh updates ages and the no-bridge / empty overlays from the stored frame.
static void refresh(void) {
	int64_t since_rx = rx_ms < 0 ? 0 : local_ms() - rx_ms;
	bool stale = rx_ms < 0 || since_rx > STALE_MS;
	set_text(overlay_label, rx_ms < 0 ? "waiting for bridge" : "no bridge");
	set_hidden(overlay, !stale);
	set_hidden(empty_label, stale || shown.count != 0);
	for (int i = 0; i < shown.count; i++) fill_card(i, &shown.cards[i], card_age_ms(&shown, &shown.cards[i], since_rx));
}

static void refresh_timer(lv_timer_t *t) {
	refresh();
}

static void touch_timer(lv_timer_t *t) {
	board_touch_t p = board_last_touch();
	char b[160];
	snprintf(b, sizeof b, "%s down %d,%d  last %d,%d -> %d,%d\nX %d..%d  Y %d..%d", p.pressed ? "DOWN" : "up", p.down_raw_x,
	         p.down_raw_y, p.raw_x, p.raw_y, p.x, p.y, p.min_x, p.max_x, p.min_y, p.max_y);
	set_text(touch_label, b);
}

static lv_obj_t *make_overlay(void) {
	lv_obj_t *o = lv_obj_create(lv_screen_active());
	lv_obj_set_size(o, SCREEN_W, SCREEN_H);
	lv_obj_set_scrollable(o, false);
	lv_obj_set_style_radius(o, 0, 0);
	lv_obj_set_style_border_width(o, 0, 0);
	lv_obj_set_style_bg_color(o, lv_color_hex(COLOR_SCREEN), 0);
	lv_obj_set_style_bg_opa(o, LV_OPA_80, 0);
	overlay_label = make_label(o, &lv_font_montserrat_20, COLOR_DIM);
	lv_obj_center(overlay_label);
	return o;
}

void ui_init(ui_tap_cb_t on_tap) {
	tap_cb = on_tap;
	lv_obj_t *scr = lv_screen_active();
	lv_obj_set_style_bg_color(scr, lv_color_hex(COLOR_SCREEN), 0);
	lv_obj_set_style_bg_opa(scr, LV_OPA_COVER, 0);

	row = lv_obj_create(scr);
	lv_obj_set_size(row, SCREEN_W, SCREEN_H);
	lv_obj_set_pos(row, 0, 0);
	lv_obj_set_style_bg_opa(row, LV_OPA_TRANSP, 0);
	lv_obj_set_style_border_width(row, 0, 0);
	lv_obj_set_style_radius(row, 0, 0);
	lv_obj_set_style_pad_all(row, PAD, 0);
	lv_obj_set_style_pad_column(row, GAP, 0);
	lv_obj_set_flex_flow(row, LV_FLEX_FLOW_ROW);
	lv_obj_set_scroll_dir(row, LV_DIR_HOR);
	lv_obj_set_scroll_snap_x(row, LV_SCROLL_SNAP_START);
	lv_obj_set_scrollbar_mode(row, LV_SCROLLBAR_MODE_OFF);
	for (int i = 0; i < CARDS_MAX; i++) make_card(i);

	empty_label = make_label(scr, &lv_font_montserrat_20, COLOR_DIM);
	lv_label_set_text(empty_label, "no sessions");
	lv_obj_center(empty_label);
	set_hidden(empty_label, true);

	more_label = make_label(scr, &lv_font_montserrat_12, COLOR_DIM);
	lv_obj_align(more_label, LV_ALIGN_TOP_RIGHT, -4, 0);
	set_hidden(more_label, true);

	stats_label = make_label(scr, &lv_font_montserrat_12, COLOR_DIM);
	lv_obj_align(stats_label, LV_ALIGN_BOTTOM_RIGHT, -4, 0);

	touch_label = make_label(scr, &lv_font_montserrat_12, 0xF472B6);
	lv_obj_align(touch_label, LV_ALIGN_TOP_LEFT, 4, 0);
	lv_timer_create(touch_timer, 50, NULL);

	overlay = make_overlay();
	lv_timer_create(refresh_timer, REFRESH_MS, NULL);
	refresh();
}

void ui_show_frame(const frame_t *f, int64_t rx_local_ms) {
	shown = *f;
	rx_ms = rx_local_ms;
	for (int i = 0; i < CARDS_MAX; i++) set_hidden(w[i].card, i >= f->count);
	if (f->total > f->count) {
		char b[16];
		snprintf(b, sizeof b, "+%d", f->total - f->count);
		set_text(more_label, b);
		set_hidden(more_label, false);
	} else {
		set_hidden(more_label, true);
	}
	refresh();
}

void ui_set_stats(uint32_t frames, uint32_t rejected) {
	char b[32];
	snprintf(b, sizeof b, "rx %u/%u", (unsigned)frames, (unsigned)rejected);
	set_text(stats_label, b);
}
