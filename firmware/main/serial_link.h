#ifndef SERIAL_LINK_H
#define SERIAL_LINK_H

#include <stdbool.h>
#include <stdint.h>

#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"

typedef struct {
	uint32_t frames;   // valid state frames received
	uint32_t rejected; // lines that were not valid state frames
	uint32_t bytes;    // bytes received
} serial_link_stats_t;

// serial_link_start installs the USB-Serial-JTAG driver and the rx task. The returned queue holds
// the newest parsed frame (length 1, items of frame_t, overwritten by each new frame).
QueueHandle_t serial_link_start(void);
serial_link_stats_t serial_link_stats(void);
// serial_send_focus writes the focus action for a card id; false when the id is unsafe or the write failed.
bool serial_send_focus(const char *id);

#endif
