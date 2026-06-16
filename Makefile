BINARY := git-who-reviewed
PKG := ./...

.PHONY: build test clean format fmt

build:
	go build -o $(BINARY) .

test:
	go test $(PKG)

format:
	gofmt -s -w .
	go mod tidy

# Alias for format.
fmt: format

clean:
	rm -f $(BINARY)
	go clean
