# ApifySandbox

Plán: `Prompts/00-APIFY_PLAN.md`. Log zjištění (zdroj pro cover letter): `apify/FINDINGS.md`.

## Stav (2026-10-17)

- [x] Fáze 0 — registrace (FREE, $5/měsíc), token v `.env`, plugin + MCP OAuth. Smoke test přes agenta `apify` nefunguje (subagent nemá MCP nástroje), přes MCP přímo ano
- [~] Fáze 1 — leady: `cmd/apify-run` (REST runner s cenou run vs. odhad), `cmd/leads-normalize` (Google Maps + kontakty + LeadsRAG CSV → `leads/apify/leads.{csv,json}`), vstupy `inputs/maps-leads-{praha,brno}.json`. 49 míst → filtr kategorií → 39 leadů, 95 % s kontaktem, 74 % s IČO (web/ARES), velikost z RES. Vyřazené v `leads/apify/dropped.csv`
- [~] Fáze 1b — TerraReality: `terrareality/ingest` (model, normalizace, FX, TTL) + `cmd/terra-ingest`. Denně jen Praha: Apify Task `terra-praha-rent-daily` (`inputs/terra-praha-task.json`, ~$0.09/run). Schedule čeká na uživatele (`inputs/terra-praha-schedule.json`). Ingest: `bin/terra-ingest -task nice_dev/sreality-listings-scraper=mEdAEfC1LaEfK94Rm` (idempotentní, bez TTL pro delta runy) Přenos do repa `terrareality` udělá session terrareality.cz.
- [~] Fáze 2 — monitoring: `cmd/apify-webhook` (secret v hlavičce, dedup runů i položek, JSONL fronta pro LLM dávku, notifikace přes příkaz). Nenasazeno.
- [~] Fáze 3 — `actors/czech-public-contracts`: Go Actor (batch + Standby `/search` + `/mcp`), lokálně ověřený nad reálnými daty a v Dockeru. Nepublikováno.
- [ ] Fáze 4 — cover letter

## Apify sekce (místo „Director hub config“)

| co | kde |
|---|---|
| token | `.env` (chmod 600) v deploy adresáři stroje, kde běží handler. Nikdy ne git, logy, FINDINGS, zprávy |
| REST klient Go | `pkg/apifyrest` (oficiální Go klient neexistuje) |
| webhook handler | JODA, `127.0.0.1:8098`, `POST /apify/webhook`, `GET /healthz`. Veřejně přes nový cloudflared hostname (schvaluje uživatel) |
| LLM dávky | SPARK `http://192.168.88.66:8080/v1`, model `openclaw-default`, jen 13–07. Fronta `data/monitor/queue.jsonl` |
| Telegram | `ssh spark ~/deploy/WorldLibraryProject/deploy/spark/notify.sh "<html>"`, jen notifikace bez HITL tlačítek |
| ComfyUI | nesouvisí, nic se tam neposílá |
| FINDINGS | `apify/FINDINGS.md`, append-only |

Pravidla: před placeným runem odhad ceny + schválení. Před nasazením na SPARK/JODU (kontejner, tunel, timer) zpráva Directorovi a čekat na OK.

## Použití

```bash
make test
make build                         # bin/apify-run, bin/terra-ingest, bin/leads-normalize, bin/apify-webhook
set -a; . ./.env; set +a

# Fáze 1 (placené — nejdřív odhad a schválení)
bin/apify-run -actor compass/crawler-google-places -input inputs/maps-leads-praha.json -max-items 28 -estimate-usd <odhad> -out raw/maps.jsonl
bin/leads-normalize -places raw/maps.jsonl -registry ../LeadsRAG/export/leads_agentury.csv -registry ../LeadsRAG/export/leads_teren.csv

# Fáze 1b — offline nad staženým výstupem
bin/terra-ingest -state data/terra/state.json -raw pringlesbitch/cz-reality-aggregator=raw/cz.jsonl

# Fáze 3 — Actor lokálně (zdarma, veřejná data)
make actor-run                     # vstup: actors/czech-public-contracts/storage/key_value_stores/default/INPUT.json
APIFY_META_ORIGIN=STANDBY ACTOR_STANDBY_PORT=4321 go run ./actors/czech-public-contracts   # z adresáře actoru
```
