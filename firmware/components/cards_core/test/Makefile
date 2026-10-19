CC ?= cc
BUILD ?= build
CFLAGS = -std=c11 -Wall -Wextra -Werror -O1 -g -fsanitize=address,undefined -fno-omit-frame-pointer \
	-I../include -I../src
SRCS = ../src/frame.c ../src/linebuf.c ../src/text.c
TESTS = test_main.c test_frame.c test_linebuf.c test_text.c

.PHONY: test clean
test: $(BUILD)/cards_core_test
	$(BUILD)/cards_core_test

$(BUILD)/cards_core_test: $(TESTS) $(SRCS) ../include/cards_core.h test.h
	mkdir -p $(BUILD)
	$(CC) $(CFLAGS) -o $@ $(TESTS) $(SRCS)

clean:
	rm -rf $(BUILD)
