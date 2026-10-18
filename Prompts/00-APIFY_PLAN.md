# APIFY_PLAN.md — handoff pro Director session

**Cíl:** Reálně používat Apify platformu ~2 týdny tak, aby (a) něco přibylo do stacku, (b) vznikl veřejný artefakt (vlastní Actor ve Store) a (c) byl materiál do cover letteru pro Apify (Praha) — konkrétní zjištění a tření, ne „chtěl bych".

**Dva legitimní use casy:**
1. Leady pro olin.now/business (navazuje na LEADS_PLAN.md / leadscout).
2. Monitoring pro promo bota (navazuje na OPENCLAW_PROMO_PLAN.md — Reddit, App Store/Play recenze, HITL přes Telegram).

**Výstup, který Director sbírá celou dobu:** `apify/FINDINGS.md` — každé tření, limit, cena, překvapení, co fungovalo/nefungovalo. Datovaný log. To je zdroj pro cover letter.

---

## Fáze 0 — Setup (den 1, ~1 h)

- [ ] Registrace: https://console.apify.com/sign-up (free tier, bez karty). Token: Settings → Integrations → `APIFY_TOKEN`.
- [ ] Plugin do Claude Code:
  ```
  /plugin marketplace add apify/apify-claude-code-plugin
  /plugin install apify@apify
  /plugin validate .
  ```
  Přidá MCP server `apify` (https://mcp.apify.com/), routing agenta `apify` a skilly: `apify-actor-development`, `apify-actorization`, `apify-generate-output-schema`, `apify-sdk-integration`, `apify-ultimate-scraper`. Skilly nejsou user-invocable — vždy jít přes agenta `apify`.
- [ ] Auth má tři cesty:
  - **MCP (OAuth)** — první `run-actor` / `get-dataset-items` otevře browser. `search-actors`, `fetch-actor-details`, `search-apify-docs` fungují bez auth.
  - **CLI** — `npm install -g apify-cli`, `apify login`, nebo headless `export APIFY_TOKEN=...`.
  - **SDK** — `apify-client` (volání Actorů z vlastní appky) jen s `APIFY_TOKEN`. Pozor: `apify` (SDK pro psaní Actorů) ≠ `apify-client`.
- [ ] Spark/SSH bez browseru: buď jednou OAuth na notebooku (refresh token se uloží), nebo jet CLI/SDK cestou s tokenem. **Zapsat do FINDINGS, jak to dřelo.**
- [ ] Ověřit, že `apify` agent odpoví na `find me 5 well-rated coffee shops in Prague and export to CSV` — smoke test celé cesty.
- [ ] Přidat Apify sekci do Director hub konfigurace (jaký sub-session má Apify plugin, kde je token, kam se logují FINDINGS).

Sub-session: `apify-setup`. Výstup: funkční plugin + první záznam ve FINDINGS.

---

## Fáze 1 — Leady pro olin.now/business (den 2–4)

Úkol pro sub-session `apify-leads`:

> Najdi 50 firem v Praze a Brně v segmentech agentury, game studia, e-shopy, firmy s terénními pracovníky. Pro každou: název, web, e-mail/telefon, počet zaměstnanců (odhad), poslední aktivita na webu. Výstup CSV + JSON do `leads/apify/`.

Postup:
- [ ] `search-actors` → vybrat Google Maps scraper + Website Content Crawler (nebo co agent doporučí; zapsat proč).
- [ ] Přes `fetch-actor-details` zjistit input schema a **cenu** (pay-per-result vs. compute units). Odhadnout cenu před spuštěním a pak porovnat se skutečností → FINDINGS.
- [ ] Spustit přes MCP `run-actor`, výsledek přes `get-dataset-items`. Sledovat: jak dlouho trvá start Actoru, jak MCP zvládá velký dataset v kontextu (ořezává? stránkuje?), kolik tokenů to stojí v Claude Max.
- [ ] Stejný run přes `apify-client` z Go/Python skriptu (SDK cesta) a porovnat ergonomii. Pro Go: oficiální Go klient Apify nenabízí — použít REST API přímo, zapsat chybějící Go SDK jako finding.
- [ ] Normalizovat výstup do formátu LEADS projektu (Registr smluv + Apify data → jeden lead model).
- [ ] Spočítat: cena za 50 leadů, podíl použitelných kontaktů, duplicity.

Co to přidá do stacku: hotová scraping vrstva pro leadscout bez vlastních headless browserů; ověřený lead model.

---

## Fáze 1b — TerraReality: převzaté inzeráty z V4 portálů (den 3–5, paralelně s 1)

Cíl: cold start TerraReality — nájemní nabídka pro celou V4 z existujících Store Actorů, zobrazovaná jako metasearch s flagem `third_party`. **Tato fáze je zároveň hlavní use case pro cover letter** (multi-portál, 4 země, 3 měny, Standby API vs. batch).

Sub-session: `apify-terra`. Výstup: `terrareality/ingest/` (Go), denní pipeline, FINDINGS záznamy.

### Zdrojové Actory (nic nestavět, jen zapojit)
| Země | Actor | Pozn. |
|---|---|---|
| CZ + SK | `pringlesbitch/cz-reality-aggregator` | 8 portálů (Sreality, Bezrealitky CZ, iDNES, RealityMIX, Reality.cz, Nehnutelnosti.sk, TopReality.sk, Reality.sk), dedup, price history, new-listing flag; **Standby HTTP API** i batch |
| CZ + SK (záloha) | `conversational_kermis/realtysync-aggregator` | Sreality + Bezrealitky CZ/SK + iDNES; porovnat kvalitu a cenu s předchozím |
| PL | `automation-lab/otodom-property-scraper` (alt. `dadhalfdev/otodom-scraper`) | bere reálnou filtrovanou URL z Otodomu |
| HU | `bakos_bence/ingatlan-com` (alt. `unfenced-group/ingatlan-scraper`) | HUF, plocha, pokoje, okresy |

- [ ] `fetch-actor-details` na každý → input schema, pricing model, odhad ceny za denní run (Praha pronájmy ~ stovky inzerátů; Bratislava, Varšava, Budapešť totéž). Zapsat odhad vs. skutečnost.
- [ ] Jeden smoke run přes MCP, pak produkčně přes REST API z Go (bez oficiálního Go klienta — finding).
- [ ] U CZ/SK aggregatoru vyzkoušet **Standby endpoint** (`/search?portals=…&transaction=rent&region=Praha`) jako live dotaz z appky vs. batch dataset — rozhodnout, co je pro TerraReality lepší, a zapsat proč.

### Normalizační schéma (TerraReality listing model)
```
Listing {
  id            // interní UUID
  source        // "sreality" | "bezrealitky_cz" | "idnes" | "realitymix" | "reality_cz" | "nehnutelnosti_sk" | "topreality_sk" | "reality_sk" | "otodom" | "ingatlan"
  source_id     // ID na zdrojovém portálu (stabilní klíč pro dedup)
  source_url    // proklik na originál — vždy zobrazit
  third_party   // true pro převzaté, false pro vlastní
  country       // CZ | SK | PL | HU
  city, district, lat, lng
  transaction   // rent
  category      // flat | house | room | commercial
  layout        // "2+kk" / "2 pokoje" → normalizovat na rooms:int + kitchen:bool
  area_m2
  price_amount, price_currency   // CZK | EUR | PLN | HUF, původní
  price_eur                      // přepočet denním kurzem ČNB/ECB, pro cross-border řazení
  price_includes_utilities       // bool|null, pokud zdroj uvádí
  available_from                 // date|null
  fetched_at, last_seen_at
  active                         // false po TTL (viz níže)
}
```
- Dedup klíč: `(source, source_id)`; cross-portál dedup (stejný byt na Sreality i iDNES) v1 neřešit, v2 přes (lat,lng,area,price) fuzzy match — zapsat jako známý dluh.
- Měny: přepočet denně, ukládat kurz a datum.
- TTL: inzerát, který se 2 dny po sobě neobjeví ve zdroji → `active=false`, nikdy nemazat (historie cen).

### Metasearch pravidla (právní minimum — dodržovat tvrdě)
- Ukládat a zobrazovat **jen fakta**: cena, lokalita, m², dispozice, patro, kategorie, datum. **Žádné fotky, žádné popisky, žádná jména a telefony inzerentů** (Bezrealitky = soukromé osoby, GDPR). Pokud Actor tato pole vrací, zahodit je už v ingestu, ne až v UI.
- Vždy viditelný zdroj + proklik na originál; kontakt vede na zdrojový portál, ne na TerraReality.
- Nikdy neoslovovat inzerenty z převzatých dat.
- Flag `third_party` v UI jako „Převzato z <portál>" s logem textově, ne obrázkem.
- Frekvence: 1× denně per země, žádné agresivní crawlování; respektovat, co Actor deklaruje („nebypassuje CAPTCHA/auth").
- Do FINDINGS zapsat ToS jednotlivých portálů (zákaz automatizovaného stahování, databázová práva EU sui generis) — vědomé riziko, metasearch vzor ho snižuje, neruší. Před veřejným launchem konzultovat.

### Produktová poznámka
Převzaté inzeráty plní regál pro nájemce, nepřivádějí pronajímatele. Use case je „prohlížej celou V4 na jednom místě" + vlastní správa pro registrované. Měřit: kolik návštěv z převzatých inzerátů vede k registraci pronajímatele (CTA „Spravuješ tento byt? Převezmi ho").

### Cena / rozpočet
- [ ] Spočítat denní náklad za 4 země při pay-per-result; porovnat s free tierem. Pokud to přesáhne ~X $/měsíc, snížit na CZ+SK a Praha-only. Zapsat.

---

## Fáze 2 — Promo monitoring (den 5–7)

Úkol pro sub-session `apify-monitor`:

- [ ] Reddit scraper Actor na klíčová slova (názvy apek, „flutter game", relevantní subreddity) + App Store / Google Play reviews scraper pro publikované apky.
- [ ] Nastavit jako **scheduled run** v Apify Console (denně) a **webhook** na Actor run finished → tvůj endpoint (Cloudflare Worker nebo Go handler na Sparku) → Telegram HITL kanál.
- [ ] Deduplikace: ukládat viděné ID do KV store nebo do vlastní DB; notifikovat jen nové.
- [ ] Zapsat: jak snadné/těžké je scheduling + webhook, co dělá retry, jak vypadá payload, co stojí denní běh. Porovnat s tím, co bys napsal sám na Sparku (cron + scraper) — kdy se Apify vyplatí a kdy ne.

Co to přidá do stacku: „cron + scraper + webhook" jako služba, kterou neprovozuješ; vstup do promo bota.

---

## Fáze 3 — Vlastní Actor ve Store (týden 2) — nejdůležitější

Úkol pro sub-session `apify-actor`:

- [ ] Actorizovat Registr smluv / veřejné zakázky downloader z LEADS projektu. Skill `apify-actorization` umí JS/TS, Python a CLI projekty. Go není první třída → cesta **CLI actorization** (Dockerfile s Go binárkou, input z `INPUT.json`, výstup push do datasetu přes API). Zapsat do FINDINGS, jak to pro Go drhne — to je cenný feedback pro ně.
  - Alternativa, pokud CLI cesta bolí: tenký Python wrapper, který volá Go binárku.
- [ ] Input schema: IČO / název firmy / CPV kód / datum od–do / limit. Output schema přes `apify-generate-output-schema`.
- [ ] README podle Apify guidelines (skill má reference). Název: **Czech Public Contracts Scraper** (Registr smluv + Věstník veřejných zakázek).
- [ ] Standby mód: Actor běží jako web server → vystavit jako API i jako **MCP endpoint** (`https://<user>--<actor>.apify.actor/mcp`). Pak ho napojit zpátky do Directoru jako MCP tool — uzavře to kruh.
- [ ] Publikovat do Store, nastavit pay-per-result cenu (OSVČ monetizace). Zapsat proces review/publikace.
- [ ] Bonus: druhý Actor z matriky-fetch (Czech archive records) — stejný vzor, jiná doména.

Co to přidá do stacku: vlastní Actor = kontejner s input schématem + dataset. Mapuje se 1:1 na job queue v AiStack — zvážit, jestli Actor spec nepoužít jako formát jobů i doma.

---

## Fáze 4 — Cover letter (po týdnu 2)

Ze `FINDINGS.md` vytáhnout:
1. **Co jsem postavil:** Actor ve Store (link), MCP endpoint, napojení do vlastního agent setupu.
2. **Co jsem zjistil:** 3–5 konkrétních třecích bodů (Go/CLI actorization, headless OAuth, cena vs. odhad, chování MCP s velkými datasety, cokoli dalšího).
3. **Co bych změnil:** 1–2 návrhy, ideálně s odkazem na issue nebo PR v apify repo.

Formát: stejný CV template jako jinde (Roboto Mono, QR na GitHub/Store). Cover letter max 1 strana, věcně, bez „rád bych".

---

## Pravidla pro Director

- Každá sub-session loguje do `apify/FINDINGS.md` (append-only, datované).
- Před každým placeným runem odhadnout cenu a nechat schválit (HITL).
- Token nikdy do repa; `.env` + `.gitignore`.
- Priorita při nedostatku času: Fáze 3 (vlastní Actor) > Fáze 1b (TerraReality, nejlepší findings) > Fáze 1 > Fáze 2.
- Sekce Fáze 1b je samostatně přenositelná do TerraReality `02-plan.md` — Director ji tam zkopíruje jako „Ingest z 3rd-party portálů".
- Issues / feedback pro Apify: otevřít issue v `apify/apify-claude-code-plugin` nebo mail support@apify.com; každý odeslaný feedback poznamenat do FINDINGS (dá se citovat).

## Odkazy

- Plugin: https://github.com/apify/apify-claude-code-plugin
- Docs pro LLM: https://docs.apify.com/llms.txt (plná verze llms-full.txt)
- Console: https://console.apify.com · Store: https://apify.com/store
- MCP: https://mcp.apify.com/ · integrace: https://docs.apify.com/platform/integrations/mcp
