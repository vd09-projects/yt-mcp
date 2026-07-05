BIN := bin

.PHONY: build tidy clean

build:
	mkdir -p $(BIN)
	go build -o $(BIN)/yt-upload-mcp ./cmd/yt-upload-mcp
	go build -o $(BIN)/yt-authorize ./cmd/yt-authorize

tidy:
	go mod tidy

clean:
	rm -rf $(BIN)
