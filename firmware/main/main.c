#include "board.h"
#include "cards_core.h"
#include "esp_timer.h"
#include "serial_link.h"
#include "ui.h"

static void on_tap(const char *card_id) {
	serial_send_focus(card_id);
}

void app_main(void) {
	board_init();
	if (board_lock(-1)) {
		ui_init(on_tap);
		board_unlock();
	}
	QueueHandle_t q = serial_link_start();
	static frame_t f;
	for (;;) {
		bool got = xQueueReceive(q, &f, pdMS_TO_TICKS(1000)) == pdTRUE;
		serial_link_stats_t s = serial_link_stats();
		if (board_lock(-1)) {
			if (got) ui_show_frame(&f, esp_timer_get_time() / 1000);
			ui_set_stats(s.frames, s.rejected);
			board_unlock();
		}
	}
}
