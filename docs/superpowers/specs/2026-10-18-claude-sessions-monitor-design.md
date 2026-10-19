# Claude sessions monitor on the Waveshare ESP32-S3 3.49" bar

## Goal
A read-only desk display that shows the state of every Claude Code session running in cmux
(one cmux workspace per ticket, created by `cw`). v2 adds touch controls. Inspired by
brushknight's agent dashboard and kindofdevice_'s UI, adapted to a 640x172 landscape bar.

## Architecture
```
cmux (events.jsonl / socket) --> bridge (Go, Mac) --USB CDC serial, JSON lines--> firmware (ESP32-S3 + LVGL)
                                      ^------------- action frames (v2) -----------------'
```

### bridge (Go)
- Source: tail of `~/.cmuxterm/events.jsonl`. Status comes from cmux's own `sidebar.metadata.updated` `set_status claude_code <Label>` events
  (Running, Running subagents, Waiting, Needs input, Idle, API error, Connection dropped); hook events only supply the title (cwd).
  Hook `Notification` is an idle ping 60 s after `Stop`, not a status signal. Stale cards are pruned after 12h.
- Enrichment (context %, cost, tokens, last prompt): hook payloads redact `tool_input`/`context`, so read
  these from the Claude transcript (`transcriptPath` in `~/.cmuxterm/claude-hook-sessions.json`) or Claude's
  status line. Which one is decided during the first spike.
- Card model: `{id (cmux surface id), title (worktree name, max 48 runes), status: working|waiting|idle|error|done, since_ms, context_pct?, cost?, last_prompt?}`.
- Emits full `state` frames only (coalesced to 250 ms); `patch` frames are deferred. Finds the board by USB VID/PID.
- v2 actions: `interrupt` -> `cmux send-key --surface <id> escape`; `focus`; `approve`/`deny` -> `cmux send`.

### firmware (ESP-IDF 5.5.3, LVGL 9.6)
- Source: `firmware/`, started from Waveshare's `10_LVGL_V9_Test` (display, touch and LVGL port kept as is, landscape via software rotation 90).
- Pure C library `cards_core` (frame parser on jsmn, line assembler, age formatting, action formatting) with host tests; the ESP-IDF side is glue: `board` (display/touch/LVGL), `serial_link` (USB-Serial-JTAG driver, rx task, tx for actions), `ui` (cards), `main`.
- UI (640x172 landscape): a horizontally swipeable row of cards, 3 visible (200x156 each, snap per card). Card = status-coloured left bar, title (3 lines, ellipsis), status text, age (`now_ms - since_ms` plus local time since the frame arrived; never the device clock). Waiting and error cards come first (bridge sorts), done cards are dimmed. Overlay "no bridge" after 35 s without a frame (heartbeat is 30 s), "no sessions" for an empty frame, `+N` when `total` exceeds the cards sent, a tiny `rx frames/rejected` counter.
- Tap on a card sends `{"v":1,"t":"action","a":"focus","id":"<card id>"}` over the same port; the bridge focuses the workspace's first pane in cmux.
- Console stays on UART with the USB port as secondary output; the app installs the USB-Serial-JTAG driver itself and ignores any line that is not a JSON state frame (ESP log lines reach the host too; the bridge rate-limits them).

### protocol
Versioned JSON lines (`{"v":1,"t":"state|action",...}`), schema shared by Go and firmware.
Wi-Fi later = swap serial for a WebSocket; frames unchanged.

## Testing
- Bridge: TDD against recorded `events.jsonl` fixtures and a fake serial port.
- Firmware UI: LVGL desktop simulator at 640x172 before flashing.
- End to end: replay a recorded event log through bridge to simulator.

## Known risks (checked 2026-10-18)
- Panel is 172x640 (confirmed in Waveshare's example source; the wiki's 172x320 is a typo). Display
  driver AXS15231B over QSPI, touch over I2C at 0x3B. Demo flashed and touch confirmed on the real board.
- 16 MB flash, 8 MB PSRAM, QMI8658 IMU and PCF85063 RTC are on board (auto-rotate and offline clock are possible later).
- The cmux socket denied access from this sandbox; the bridge must run from a cmux terminal or be allowed under Settings > Automation.
  `~/.cmuxterm/events.jsonl` is a readable fallback.
- Context %, cost and last prompt are not in cmux hook payloads (see Enrichment).

## Out of scope for v1
Touch controls, widgets (clock, weather), Wi-Fi transport, audio.
