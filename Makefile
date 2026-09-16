.PHONY: test demo fmt build

fmt:
	gofmt -w cmd internal

test:
	go test ./...

build:
	go build -o rpcdiff ./cmd/rpcdiff

demo:
	go run ./cmd/rpcdiff demo --output report.json --html report.html
