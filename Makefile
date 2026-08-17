# CyberRange OS — developer convenience targets.
.PHONY: help api-build api-test api-run seed web-install web-build web-dev up down kali fmt

help:
	@echo "Targets: api-build api-test api-run seed web-install web-build web-dev up down kali"

api-build:
	cd apps/api && go build ./...

api-test:
	cd apps/api && go test ./...

api-run:
	cd apps/api && go run ./cmd/api

seed:
	cd apps/api && go run ./cmd/seed

web-install:
	cd apps/web && npm install

web-build:
	cd apps/web && npm run build

web-dev:
	cd apps/web && npm run dev

kali:
	docker build -t cyberrange/kali-attacker:latest infra/kali

up:
	docker compose -f infra/docker-compose.yml up -d --build

down:
	docker compose -f infra/docker-compose.yml down

fmt:
	cd apps/api && go fmt ./...
