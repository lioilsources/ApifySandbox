.PHONY: test build actor-test actor-image ruian-vendor ruian-online ruian-image ruian-codebooks notices-vendor notices-image
GO ?= go

test:
	$(GO) vet ./... && $(GO) test ./...
	cd actors/czech-public-contracts && $(GO) vet ./... && $(GO) test ./...
	cd ruian && $(GO) vet ./... && $(GO) test ./...
	cd actors/ruian-parcel-mcp && $(GO) vet ./... && $(GO) test ./...
	cd actors/czech-building-notices && $(GO) vet ./... && $(GO) test ./...

build:
	$(GO) build -o bin/ ./cmd/...

actor-image:
	docker build -t czech-public-contracts:dev actors/czech-public-contracts

# Local Actor run on real (free, public) Registr smluv data, input in storage/key_value_stores/default/INPUT.json
actor-run:
	cd actors/czech-public-contracts && $(GO) run .

# Cross-build the webhook for JODA (deploy only after Director OK)
webhook-linux:
	GOOS=linux GOARCH=$${GOARCH:-amd64} CGO_ENABLED=0 $(GO) build -o bin/apify-webhook-linux ./cmd/apify-webhook

# JODA (x86_64, systemd --user). Deploy only with Director OK; .env goes separately (chmod 600).
terra-ingest-joda:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o bin/linux-amd64/terra-ingest ./cmd/terra-ingest

deploy-joda: terra-ingest-joda
	ssh joda 'mkdir -p ~/deploy/ApifySandbox/bin ~/deploy/ApifySandbox/data/terra ~/.config/systemd/user'
	scp bin/linux-amd64/terra-ingest joda:deploy/ApifySandbox/bin/terra-ingest
	scp deploy/joda/terra-ingest.service deploy/joda/terra-ingest.timer joda:.config/systemd/user/
	ssh joda 'systemctl --user daemon-reload && systemctl --user enable --now terra-ingest.timer'

## tradebot (Prompts/01-TRADING_BOT_PLAN.md)
tradebot-data: ## backfill Binance history and append Kraken days (free, keyless)
	go run ./cmd/tradebot-data -backfill -update

tradebot-backtest: ## variant A on stored candles, equity curve to data/tradebot
	go run ./cmd/tradebot-backtest -equity data/tradebot/equity_A.csv

## RÚIAN (Prompts/03-RUIAN_ACTORS_PLAN.md)
ruian-vendor: notices-vendor ## copy the shared ruian package into Actors D and B (the Apify build sees only the Actor dir)
	cd actors/ruian-parcel-mcp && $(GO) mod tidy && $(GO) mod vendor

notices-vendor:
	cd actors/czech-building-notices && $(GO) mod tidy && $(GO) mod vendor

ruian-online: ## live checks against ČÚZK (free, keyless)
	cd ruian && $(GO) test -count=1 -tags online ./...

ruian-codebooks: ## refresh official ČÚZK codebooks, then run ruian-vendor
	$(GO) run ./cmd/ruian-codebooks

ruian-image: ruian-vendor
	docker build -t ruian-parcel-mcp:dev actors/ruian-parcel-mcp

notices-image: notices-vendor
	docker build -t czech-building-notices:dev actors/czech-building-notices
