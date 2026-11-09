#ifndef BOARD_H
#define BOARD_H

#include <stdbool.h>
#include <stdint.h>

// board_init brings up the QSPI panel, touch and LVGL (landscape 640x172) and starts the LVGL task.
void board_init(void);
// LVGL is not thread-safe: hold the lock around every lv_* call made outside the LVGL task.
bool board_lock(int timeout_ms);
void board_unlock(void);

// Last touch sample, for diagnostics: raw panel coordinates and the logical (landscape) point LVGL got.
typedef struct {
	int16_t raw_x, raw_y, x, y;          // last sample and the logical point LVGL got
	int16_t down_raw_x, down_raw_y;      // first sample of the current or last press
	int16_t min_x, max_x, min_y, max_y;  // raw ranges seen since boot
	bool pressed;
} board_touch_t;
board_touch_t board_last_touch(void);

#endif
