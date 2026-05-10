.PHONY: gen run web tidy lint clean

gen:
	buf generate

run:
	go run ./cmd/server

web:
	cd web && npm run dev

tidy:
	go mod tidy

lint:
	buf lint

clean:
	rm -rf gen web/src/lib/gen web/.svelte-kit web/build
