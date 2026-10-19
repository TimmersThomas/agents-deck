#include "serial_link.h"

#include "cards_core.h"
#include "driver/usb_serial_jtag.h"
#include "esp_err.h"

#define RX_CHUNK 128
#define WRITE_TIMEOUT_MS 50

static QueueHandle_t frame_q;
static volatile uint32_t n_frames, n_rejected, n_bytes;

static void on_line(const char *line, size_t len) {
	static frame_t f;
	if (frame_parse(line, len, &f) == FRAME_OK) {
		xQueueOverwrite(frame_q, &f);
		n_frames++;
	} else {
		n_rejected++;
	}
}

static void rx_task(void *arg) {
	static linebuf_t lb;
	uint8_t chunk[RX_CHUNK];
	linebuf_init(&lb);
	for (;;) {
		int n = usb_serial_jtag_read_bytes(chunk, sizeof chunk, pdMS_TO_TICKS(100));
		if (n <= 0) continue;
		n_bytes += (uint32_t)n;
		for (int i = 0; i < n; i++) {
			size_t len;
			if (linebuf_push(&lb, (char)chunk[i], &len)) on_line(lb.buf, len);
		}
	}
}

QueueHandle_t serial_link_start(void) {
	frame_q = xQueueCreate(1, sizeof(frame_t));
	assert(frame_q);
	usb_serial_jtag_driver_config_t cfg = USB_SERIAL_JTAG_DRIVER_CONFIG_DEFAULT();
	cfg.rx_buffer_size = 2048;
	cfg.tx_buffer_size = 512;
	ESP_ERROR_CHECK(usb_serial_jtag_driver_install(&cfg));
	xTaskCreatePinnedToCore(rx_task, "serial_rx", 4096, NULL, 3, NULL, 1);
	return frame_q;
}

serial_link_stats_t serial_link_stats(void) {
	serial_link_stats_t s = {.frames = n_frames, .rejected = n_rejected, .bytes = n_bytes};
	return s;
}

bool serial_send_focus(const char *id) {
	char buf[160];
	buf[0] = '\n'; // ends any log text the console left half-written, so the bridge sees a clean line
	int n = action_format(buf + 1, sizeof buf - 1, id);
	if (n < 0) return false;
	return usb_serial_jtag_write_bytes(buf, (size_t)n + 1, pdMS_TO_TICKS(WRITE_TIMEOUT_MS)) == n + 1;
}
