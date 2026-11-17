# ApifySandbox

<!-- Max 80 řádků. Co se nevejde, jde do docs/ a sem jeden odkaz. Veřejné repo: žádné LAN IP, domovské cesty, ID. -->

## Účel
Pískoviště pro Apify: leady, ingest realit (TerraReality), monitoring přes webhook,
vlastní Go Actory (veřejné zakázky, RÚIAN, stavební ohlášky) a tradebot. Plán:
`Prompts/00-APIFY_PLAN.md`, log zjištění `apify/FINDINGS.md`.

## Build a test
```
make test     # go vet + go test v kořeni, ruian/ a ve všech actors/*
make build    # bin/apify-run, bin/terra-ingest, bin/leads-normalize, bin/apify-webhook, …
make ruian-online   # živé testy proti ČÚZK (zdarma)
```
Gate před pushem: `make test`. Token je v `.env` (`set -a; . ./.env; set +a`).

## Mapa
- `cmd/`: CLI nástroje (apify-run, apify-webhook, leads-normalize, terra-ingest, tradebot-*, ruian-codebooks)
- `pkg/apifyrest/`: REST klient Apify v Go
- `actors/`: samostatné Go Actory, každý s vlastním `go.mod`
- `ruian/`: sdílený balíček RÚIAN (vlastní `go.mod`, do Actorů se vendoruje `make ruian-vendor`)
- `leads/`, `terrareality/`, `monitor/`, `tradebot/`: doménová logika
- `inputs/`: vstupy Apify runů a tasků
- `deploy/joda/`: systemd user unity pro terra-ingest
- `Prompts/`: plány fází

## Invarianty
- Před placeným runem odhad ceny a schválení (`apify-run -estimate-usd`).
- Před nasazením na SPARK/JODU zpráva Directorovi a čekat na OK.
- `apify/FINDINGS.md` je append-only.
- Token jen v `.env`, nikdy v gitu, logu ani FINDINGS.

## Kam dál
README (stav fází, Apify sekce, použití), `Prompts/`
