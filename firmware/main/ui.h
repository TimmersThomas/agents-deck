#ifndef UI_H
#define UI_H

#include <stdint.h>

#include "cards_core.h"

typedef void (*ui_tap_cb_t)(const char *card_id);

// All ui_* functions must be called with the LVGL lock held (board_lock).
void ui_init(ui_tap_cb_t on_tap);
// ui_show_frame shows f; rx_local_ms is the local time (ms) at which it arrived.
void ui_show_frame(const frame_t *f, int64_t rx_local_ms);
void ui_set_stats(uint32_t frames, uint32_t rejected);

#endif
