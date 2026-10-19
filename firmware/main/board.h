#ifndef BOARD_H
#define BOARD_H

#include <stdbool.h>

// board_init brings up the QSPI panel, touch and LVGL (landscape 640x172) and starts the LVGL task.
void board_init(void);
// LVGL is not thread-safe: hold the lock around every lv_* call made outside the LVGL task.
bool board_lock(int timeout_ms);
void board_unlock(void);

#endif
