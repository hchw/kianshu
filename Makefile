.PHONY: dev dev-backend dev-frontend build test lint clean swagger

KS_ENC_KEY ?= dev-only-32-byte-secret-key-0000
export KS_ENC_KEY

swagger:
	swag init -g cmd/server/swagger.go -o docs --ot go,json,yaml

dev:
	@trap 'kill 0' EXIT; \
	go run ./cmd/server/main.go & \
	cd web && npm run dev & \
	wait

dev-backend:
	go run ./cmd/server

dev-frontend:
	cd web && npm run dev

build:
	GOTOOLCHAIN=go1.25.6 go build -o server ./cmd/server
	cd web && npm run build

test:
	GOTOOLCHAIN=go1.25.6 go test ./...
	cd web && npm test

lint:
	GOTOOLCHAIN=go1.25.6 go vet ./...
	cd web && npm run lint

clean:
	rm -f server kianshu.db
