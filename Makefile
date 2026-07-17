.PHONY: tidy test web-install web-build sync-web build run dev-api import-sms clean

API_TOKEN ?= dev-token-change-me
INGEST_TOKEN ?= $(API_TOKEN)
ADMIN_PASSWORD ?=
PORT ?= 8080
DATABASE_PATH ?= ./data/cashpulse.db

tidy:
	go mod tidy

test:
	go test ./...

web-install:
	cd web && npm install

web-build:
	cd web && npm run build

# Copy Vite build into the Go embed directory.
sync-web: web-build
	rm -rf cmd/server/dist
	mkdir -p cmd/server/dist
	cp -R web/dist/. cmd/server/dist/

build: sync-web tidy
	go build -o bin/cashpulse ./cmd/server

run: build
	API_TOKEN=$(API_TOKEN) INGEST_TOKEN=$(INGEST_TOKEN) ADMIN_PASSWORD=$(ADMIN_PASSWORD) PORT=$(PORT) DATABASE_PATH=$(DATABASE_PATH) ./bin/cashpulse

# API only (no need for frontend rebuild). Use with `cd web && npm run dev` for UI.
dev-api: tidy
	mkdir -p cmd/server/dist
	@test -f cmd/server/dist/index.html || echo '<!doctype html><title>CashPulse</title><p>Build web UI with make sync-web</p>' > cmd/server/dist/index.html
	API_TOKEN=$(API_TOKEN) PORT=$(PORT) DATABASE_PATH=$(DATABASE_PATH) go run ./cmd/server

# Import historical PSBC export into SQLite.
# Usage: make import-sms FILE=tmp/95580短信完整导出.txt
FILE ?= tmp/95580短信完整导出.txt
import-sms: tidy
	go run ./cmd/import -file "$(FILE)" -db "$(DATABASE_PATH)"

clean:
	rm -rf bin data web/dist cmd/server/dist
