BIN=claudster
INSTALL_PATH=/usr/local/bin/$(BIN)
DIST=dist

build:
	go build -o $(BIN) .

install: build
	cp $(BIN) $(INSTALL_PATH)
	codesign --sign - $(INSTALL_PATH) 2>/dev/null || true
	@echo "installed to $(INSTALL_PATH)"

uninstall:
	rm -f $(INSTALL_PATH)
	@echo "removed $(INSTALL_PATH)"

release:
	mkdir -p $(DIST)
	GOOS=darwin  GOARCH=arm64 go build -o $(DIST)/$(BIN)-darwin-arm64 .
	GOOS=darwin  GOARCH=amd64 go build -o $(DIST)/$(BIN)-darwin-amd64 .
	GOOS=linux   GOARCH=amd64 go build -o $(DIST)/$(BIN)-linux-amd64 .
	GOOS=linux   GOARCH=arm64 go build -o $(DIST)/$(BIN)-linux-arm64 .
	@echo ""
	@echo "binaries in $(DIST)/:"
	@ls -lh $(DIST)/

clean:
	rm -f $(BIN)
	rm -rf $(DIST)

.PHONY: build install uninstall release clean
