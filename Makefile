.PHONY: build test

build:
	go build -o mailbridge ./cmd/mailbridge/

test:
	go test ./... -count=1 -race
