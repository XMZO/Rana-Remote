.PHONY: build test lint web i18n-check e2e docker clean

build:
	go build -o rana ./cmd/rana
	go build -o rana-api ./cmd/rana-api

test:
	go test ./... -v -count=1

lint:
	go vet ./...

web:
	cd web && npm ci && npm run build

i18n-check:
	cd web && npm run i18n:check

e2e:
	cd web && npm run test:e2e

docker:
	docker build -t rana-remote .

clean:
	rm -f rana rana-api
