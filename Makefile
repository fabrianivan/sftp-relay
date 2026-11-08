# Common development tasks. Windows users: see scripts/build-windows.bat.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: run build test lint demo macos clean

run:            ## start the desktop app
	go run .

build:          ## build ./dist/sftp-relay for this machine
	go build -trimpath -ldflags "$(LDFLAGS)" -o dist/sftp-relay .

test:           ## unit + integration tests with the race detector
	go test -race -count=1 ./...

lint:
	test -z "$$(gofmt -l .)"
	go vet ./...

demo:           ## two throwaway local SFTP servers + the app pointed at them
	@echo "Starting demo servers on :2201/:2202 (user demo / password demo)"
	@go run ./tools/devserver & \
	  sleep 2; SFTP_RELAY_SOURCE_PASSWORD=demo SFTP_RELAY_TARGET_PASSWORD=demo \
	  go run . -config tools/devserver/config.json; kill %1

macos:          ## .app bundle + DMG in dist/
	scripts/build-macos.sh

clean:
	rm -rf dist tools/devserver/demo-data
