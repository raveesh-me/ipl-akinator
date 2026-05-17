.PHONY: run build tidy clean

run:
	go run ./cmd/akinator

build:
	go build -o bin/akinator ./cmd/akinator

tidy:
	go mod tidy

clean:
	rm -rf bin feedback.jsonl
