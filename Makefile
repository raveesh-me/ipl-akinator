.PHONY: run build tidy clean

# Auto-load .env if present. The leading dash makes the include optional.
-include .env
export

run:
	go run ./cmd/akinator

build:
	go build -o bin/akinator ./cmd/akinator

tidy:
	go mod tidy

clean:
	rm -rf bin feedback.jsonl
