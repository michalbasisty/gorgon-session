BIN_DIR := bin
APP_NAME := gorgon-session

ifeq ($(OS),Windows_NT)
	BIN_EXT := .exe
else
	BIN_EXT :=
endif

BIN_PATH := $(BIN_DIR)/$(APP_NAME)$(BIN_EXT)

.PHONY: build test clean

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_PATH) ./cmd/gorgon

test:
	go test ./...

clean:
	rm -f $(BIN_DIR)/$(APP_NAME) $(BIN_DIR)/$(APP_NAME).exe
