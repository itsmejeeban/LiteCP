.PHONY: build test run

build:
	go build -trimpath -o bin/litecp .

test:
	go test ./...

run:
	LITECP_DATA_DIR=./.data LITECP_BIND=127.0.0.1:9090 go run .
