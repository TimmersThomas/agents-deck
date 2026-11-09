// Display, touch and LVGL port: the Waveshare 10_LVGL_V9_Test example, split out of its app_main.
#include "board.h"

#include <string.h>

#include "driver/gpio.h"
#include "driver/spi_master.h"
#include "esp_err.h"
#include "esp_heap_caps.h"
#include "esp_lcd_axs15231b.h"
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"
#include "esp_lcd_panel_vendor.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "i2c_bsp.h"
#include "lcd_bl_pwm_bsp.h"
#include "lvgl.h"
#include "user_config.h"

static const char *TAG = "board";

static SemaphoreHandle_t lvgl_mux = NULL;
static SemaphoreHandle_t flush_done_semaphore = NULL;
static uint16_t *trans_buf[2];

#define LCD_BIT_PER_PIXEL 16
#define BYTES_PER_PIXEL (LV_COLOR_FORMAT_GET_SIZE(LV_COLOR_FORMAT_RGB565))
#define BUFF_SIZE (EXAMPLE_LCD_H_RES * EXAMPLE_LCD_V_RES * BYTES_PER_PIXEL)

#define LVGL_TICK_PERIOD_MS 5
#define LVGL_TASK_MAX_DELAY_MS 500
#define LVGL_TASK_MIN_DELAY_MS 10
#define LVGL_TASK_STACK_SIZE (8 * 1024)
#define LVGL_TASK_PRIORITY 2

// The panel is 172x640 portrait and LVGL renders 640x172 landscape. A flush rotates the frame
// 90 degrees clockwise (and byte-swaps it for the panel) in chunks of CHUNK_ROWS physical rows,
// straight from the render buffer into two small DMA buffers, so a chunk is rotated while the
// previous one is still on the wire.
#define LW EXAMPLE_LCD_V_RES
#define LH EXAMPLE_LCD_H_RES
#define CHUNK_ROWS 64
#define INTERNAL_HEADROOM (96 * 1024)

// 1: upright with the USB cable on the right; 0: upright with the cable on the left (the Waveshare demo).
// It flips the picture and the touch axes together.
#define BOARD_FLIP_180 1

static const axs15231b_lcd_init_cmd_t lcd_init_cmds[] = {
	{0x11, (uint8_t[]){0x00}, 0, 100},
	{0x29, (uint8_t[]){0x00}, 0, 100},
};

static bool notify_lvgl_flush_ready(esp_lcd_panel_io_handle_t panel_io, esp_lcd_panel_io_event_data_t *edata, void *user_ctx) {
	BaseType_t high_task_awoken = pdFALSE;
	xSemaphoreGiveFromISR(flush_done_semaphore, &high_task_awoken);
	return false;
}

#if BOARD_FLIP_180
// rotate_chunk fills dst (LH x CHUNK_ROWS) with physical rows py0..py0+CHUNK_ROWS-1:
// logical (lx, ly) goes to physical column LH-1-ly, physical row lx.
static void rotate_chunk(const uint16_t *src, uint16_t *dst, int py0) {
	for (int ly = 0; ly < LH; ly++) {
		const uint16_t *s = src + ly * LW + py0;
		uint16_t *d = dst + (LH - 1 - ly);
		for (int r = 0; r < CHUNK_ROWS; r++) {
			*d = __builtin_bswap16(s[r]);
			d += LH;
		}
	}
}
#else
// rotate_chunk fills dst (LH x CHUNK_ROWS) with physical rows py0..py0+CHUNK_ROWS-1 (LVGL's
// rotation 90): logical (lx, ly) goes to physical column ly, physical row LW-1-lx.
static void rotate_chunk(const uint16_t *src, uint16_t *dst, int py0) {
	for (int ly = 0; ly < LH; ly++) {
		const uint16_t *s = src + ly * LW + (LW - 1 - py0);
		uint16_t *d = dst + ly;
		for (int r = 0; r < CHUNK_ROWS; r++) {
			*d = __builtin_bswap16(s[-r]);
			d += LH;
		}
	}
}
#endif

static void lvgl_flush_cb(lv_display_t *disp, const lv_area_t *area, uint8_t *color_p) {
	esp_lcd_panel_handle_t panel_handle = (esp_lcd_panel_handle_t)lv_display_get_user_data(disp);
	const uint16_t *src = (const uint16_t *)color_p;
	for (int c = 0; c < LW / CHUNK_ROWS; c++) {
		xSemaphoreTake(flush_done_semaphore, portMAX_DELAY); // a DMA buffer is free again
		uint16_t *buf = trans_buf[c & 1];
		rotate_chunk(src, buf, c * CHUNK_ROWS);
		esp_lcd_panel_draw_bitmap(panel_handle, 0, c * CHUNK_ROWS, LH, (c + 1) * CHUNK_ROWS, buf);
	}
	for (int i = 0; i < 2; i++) xSemaphoreTake(flush_done_semaphore, portMAX_DELAY); // all sent
	for (int i = 0; i < 2; i++) xSemaphoreGive(flush_done_semaphore);
	lv_display_flush_ready(disp);
}

static volatile board_touch_t last_touch = {.min_x = INT16_MAX, .max_x = INT16_MIN, .min_y = INT16_MAX, .max_y = INT16_MIN};

board_touch_t board_last_touch(void) {
	return last_touch;
}

static void record_touch(int16_t raw_x, int16_t raw_y, int16_t x, int16_t y, bool pressed) {
	board_touch_t t = last_touch;
	if (pressed && !t.pressed) {
		t.down_raw_x = raw_x;
		t.down_raw_y = raw_y;
	}
	if (pressed) {
		t.raw_x = raw_x, t.raw_y = raw_y, t.x = x, t.y = y;
		if (raw_x < t.min_x) t.min_x = raw_x;
		if (raw_x > t.max_x) t.max_x = raw_x;
		if (raw_y < t.min_y) t.min_y = raw_y;
		if (raw_y > t.max_y) t.max_y = raw_y;
	}
	t.pressed = pressed;
	last_touch = t;
}

// The touch controller reports in panel pixels, as the Waveshare example's portrait branch shows: the
// native panel point is (rawY, 640 - rawX), raw X being the long axis. LVGL rotates pointer input
// itself for a rotated display, so this feeds native coordinates; the 180 degree variant of the
// picture needs the point turned 180 degrees as well because LVGL assumes its own rotation.
static void touch_read_cb(lv_indev_t *indev, lv_indev_data_t *indevData) {
	uint8_t read_touchpad_cmd[11] = {0xb5, 0xab, 0xa5, 0x5a, 0x0, 0x0, 0x0, 0x0e, 0x0, 0x0, 0x0};
	uint8_t buff[32] = {0};
	ESP_ERROR_CHECK_WITHOUT_ABORT(i2c_master_write_read_dev(disp_touch_dev_handle, read_touchpad_cmd, 11, buff, 32));
	uint16_t rawX = (((uint16_t)buff[2] & 0x0f) << 8) | (uint16_t)buff[3];
	uint16_t rawY = (((uint16_t)buff[4] & 0x0f) << 8) | (uint16_t)buff[5];
	if (buff[1] > 0 && buff[1] < 5) {
		if (rawX > LW) rawX = LW;
		if (rawY > LH) rawY = LH;
		int px = rawY;
		int py = LW - rawX;
#if BOARD_FLIP_180
		px = LH - px;
		py = LW - py;
#endif
		indevData->state = LV_INDEV_STATE_PRESSED;
		indevData->point.x = px;
		indevData->point.y = py;
		record_touch((int16_t)rawX, (int16_t)rawY, (int16_t)px, (int16_t)py, true);
	} else {
		indevData->state = LV_INDEV_STATE_RELEASED;
		record_touch(0, 0, 0, 0, false);
	}
}

static void increase_lvgl_tick(void *arg) {
	lv_tick_inc(LVGL_TICK_PERIOD_MS);
}

bool board_lock(int timeout_ms) {
	assert(lvgl_mux && "board_init must be called first");
	const TickType_t timeout_ticks = (timeout_ms == -1) ? portMAX_DELAY : pdMS_TO_TICKS(timeout_ms);
	return xSemaphoreTake(lvgl_mux, timeout_ticks) == pdTRUE;
}

void board_unlock(void) {
	assert(lvgl_mux && "board_init must be called first");
	xSemaphoreGive(lvgl_mux);
}

static void lvgl_port_task(void *arg) {
	uint32_t task_delay_ms = LVGL_TASK_MAX_DELAY_MS;
	for (;;) {
		if (board_lock(-1)) {
			task_delay_ms = lv_timer_handler();
			board_unlock();
		}
		if (task_delay_ms > LVGL_TASK_MAX_DELAY_MS) {
			task_delay_ms = LVGL_TASK_MAX_DELAY_MS;
		} else if (task_delay_ms < LVGL_TASK_MIN_DELAY_MS) {
			task_delay_ms = LVGL_TASK_MIN_DELAY_MS;
		}
		vTaskDelay(pdMS_TO_TICKS(task_delay_ms));
	}
}

static esp_lcd_panel_handle_t panel_init(void) {
	spi_bus_config_t buscfg = {};
	buscfg.sclk_io_num = EXAMPLE_PIN_NUM_LCD_PCLK;
	buscfg.data0_io_num = EXAMPLE_PIN_NUM_LCD_DATA0;
	buscfg.data1_io_num = EXAMPLE_PIN_NUM_LCD_DATA1;
	buscfg.data2_io_num = EXAMPLE_PIN_NUM_LCD_DATA2;
	buscfg.data3_io_num = EXAMPLE_PIN_NUM_LCD_DATA3;
	buscfg.max_transfer_sz = LVGL_DMA_BUFF_LEN;
	ESP_ERROR_CHECK(spi_bus_initialize(LCD_HOST, &buscfg, SPI_DMA_CH_AUTO));

	esp_lcd_panel_io_handle_t panel_io = NULL;
	esp_lcd_panel_handle_t panel = NULL;
	esp_lcd_panel_io_spi_config_t io_config = {};
	io_config.cs_gpio_num = EXAMPLE_PIN_NUM_LCD_CS;
	io_config.dc_gpio_num = -1;
	io_config.spi_mode = 3;
	io_config.pclk_hz = 40 * 1000 * 1000;
	io_config.trans_queue_depth = 10;
	io_config.on_color_trans_done = notify_lvgl_flush_ready;
	io_config.lcd_cmd_bits = 32;
	io_config.lcd_param_bits = 8;
	io_config.flags.quad_mode = true;
	ESP_ERROR_CHECK(esp_lcd_new_panel_io_spi(LCD_HOST, &io_config, &panel_io));

	axs15231b_vendor_config_t vendor_config = {};
	vendor_config.flags.use_qspi_interface = 1;
	vendor_config.init_cmds = lcd_init_cmds;
	vendor_config.init_cmds_size = sizeof(lcd_init_cmds) / sizeof(lcd_init_cmds[0]);

	esp_lcd_panel_dev_config_t panel_config = {};
	panel_config.reset_gpio_num = -1;
	panel_config.rgb_ele_order = LCD_RGB_ELEMENT_ORDER_RGB;
	panel_config.bits_per_pixel = LCD_BIT_PER_PIXEL;
	panel_config.vendor_config = &vendor_config;
	ESP_ERROR_CHECK(esp_lcd_new_panel_axs15231b(panel_io, &panel_config, &panel));

	ESP_ERROR_CHECK(gpio_set_level(EXAMPLE_PIN_NUM_LCD_RST, 1));
	vTaskDelay(pdMS_TO_TICKS(30));
	ESP_ERROR_CHECK(gpio_set_level(EXAMPLE_PIN_NUM_LCD_RST, 0));
	vTaskDelay(pdMS_TO_TICKS(250));
	ESP_ERROR_CHECK(gpio_set_level(EXAMPLE_PIN_NUM_LCD_RST, 1));
	vTaskDelay(pdMS_TO_TICKS(30));
	ESP_ERROR_CHECK(esp_lcd_panel_init(panel));
	return panel;
}

static void lvgl_init(esp_lcd_panel_handle_t panel) {
	lv_init();
	lv_display_t *disp = lv_display_create(EXAMPLE_LCD_H_RES, EXAMPLE_LCD_V_RES);
	lv_display_set_flush_cb(disp, lvgl_flush_cb);

	for (int i = 0; i < 2; i++) {
		trans_buf[i] = (uint16_t *)heap_caps_malloc(LVGL_DMA_BUFF_LEN, MALLOC_CAP_DMA);
		assert(trans_buf[i]);
	}
	// Rendering into internal RAM is much faster than into PSRAM, so use one internal frame buffer
	// when it leaves room for the tasks; otherwise fall back to two PSRAM buffers.
	uint8_t *buffer_1 = NULL, *buffer_2 = NULL;
	if (heap_caps_get_free_size(MALLOC_CAP_INTERNAL) > BUFF_SIZE + INTERNAL_HEADROOM) {
		buffer_1 = (uint8_t *)heap_caps_malloc(BUFF_SIZE, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
	}
	if (buffer_1) {
		ESP_LOGI(TAG, "draw buffer in internal RAM");
	} else {
		ESP_LOGI(TAG, "draw buffers in PSRAM");
		buffer_1 = (uint8_t *)heap_caps_malloc(BUFF_SIZE, MALLOC_CAP_SPIRAM);
		assert(buffer_1);
		buffer_2 = (uint8_t *)heap_caps_malloc(BUFF_SIZE, MALLOC_CAP_SPIRAM);
		assert(buffer_2);
	}
	lv_display_set_buffers(disp, buffer_1, buffer_2, BUFF_SIZE, LV_DISPLAY_RENDER_MODE_FULL);
	lv_display_set_user_data(disp, panel);
	lv_display_set_rotation(disp, LV_DISPLAY_ROTATION_90);

	lv_indev_t *touch_indev = lv_indev_create();
	lv_indev_set_type(touch_indev, LV_INDEV_TYPE_POINTER);
	lv_indev_set_read_cb(touch_indev, touch_read_cb);
}

void board_init(void) {
	lcd_bl_pwm_bsp_init(LCD_PWM_MODE_255);
	flush_done_semaphore = xSemaphoreCreateCounting(2, 2);
	assert(flush_done_semaphore);
	touch_i2c_master_Init();

	gpio_config_t gpio_conf = {};
	gpio_conf.intr_type = GPIO_INTR_DISABLE;
	gpio_conf.mode = GPIO_MODE_OUTPUT;
	gpio_conf.pin_bit_mask = ((uint64_t)0x01 << EXAMPLE_PIN_NUM_LCD_RST);
	gpio_conf.pull_down_en = GPIO_PULLDOWN_DISABLE;
	gpio_conf.pull_up_en = GPIO_PULLUP_ENABLE;
	ESP_ERROR_CHECK_WITHOUT_ABORT(gpio_config(&gpio_conf));

	ESP_LOGI(TAG, "init panel");
	esp_lcd_panel_handle_t panel = panel_init();
	ESP_LOGI(TAG, "init lvgl");
	lvgl_init(panel);

	esp_timer_create_args_t tick_args = {};
	tick_args.callback = &increase_lvgl_tick;
	tick_args.name = "lvgl_tick";
	esp_timer_handle_t tick_timer = NULL;
	ESP_ERROR_CHECK(esp_timer_create(&tick_args, &tick_timer));
	ESP_ERROR_CHECK(esp_timer_start_periodic(tick_timer, LVGL_TICK_PERIOD_MS * 1000));

	lvgl_mux = xSemaphoreCreateMutex();
	assert(lvgl_mux);
	xTaskCreatePinnedToCore(lvgl_port_task, "LVGL", LVGL_TASK_STACK_SIZE, NULL, LVGL_TASK_PRIORITY, NULL, 0);
}
