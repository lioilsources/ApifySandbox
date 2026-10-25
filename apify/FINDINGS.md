# Apify FINDINGS

Append-only, datovaný log tření, limitů, cen a překvapení. Zdroj pro cover letter (Fáze 4).
Formát: `## YYYY-MM-DD — téma`, pod tím fakta. Odhad ceny vždy **před** runem, skutečnost **po** runu.
Žádné tokeny, žádná osobní data.

---

## 2026-10-17 — Stav a infra (setup před registrací)

- Na Macu chybí `apify` CLI, `APIFY_TOKEN` není nastavený a plugin `apify@apify` zatím není nainstalovaný (instaluje se přes `/plugin`, to musí udělat uživatel).
- Rozhodnutí podle Directoru:
  - Webhook handler (Fáze 2) poběží na JODĚ, ne na SPARKu. SPARK je GPU stroj a pod zátěží se vypíná (naposledy 16. 10.).
  - Veřejná URL bude nový hostname v cloudflared, ten musí schválit uživatel.
  - LLM obohacení (leady, třídění recenzí) jen dávkově v okně 13–07 přes `openclaw-default`, nikdy synchronně z webhooku.
  - Telegram jde přes `notify.sh` na SPARKu (Clown bot) a umí jen jednosměrné notifikace. HITL tlačítka tam nejdou, protože getUpdates už polluje OpenClaw. O vlastním schvalovacím botovi rozhodne uživatel.
- Headless auth pro SPARK/JODU: OAuth přes MCP potřebuje browser, takže na serverech bude jen `APIFY_TOKEN` v `.env` (chmod 600) a REST. Jak drhne OAuth na notebooku, doplním po registraci.

## 2026-10-17 — Go: oficiální klient neexistuje

- Apify má oficiální klienty jen pro JS a Python (`apify-client`), stejně tak SDK pro psaní Actorů (`apify`). Pro Go nic.
- Vlastní REST klient `pkg/apifyrest` má ~200 řádků:
  - umí start runu, waitForFinish long-poll (max 60 s na request), run-sync-get-dataset-items (limit 300 s), stránkování datasetu, push, KV store a Standby GET;
  - token posílá v hlavičce `Authorization: Bearer`, ne v `?token=`, protože docs i příklady v konzoli ukazují token v query stringu a ten pak končí v proxy/access lozích.
- Uvnitř Actoru je potřeba ručně replikovat runtime SDK (`actors/czech-public-contracts/internal/actor`):
  - env proměnné existují ve dvou generacích (`ACTOR_*` vs. starší `APIFY_*`), čtu obě;
  - INPUT z KV store, push do datasetu po dávkách se retry na 429/5xx;
  - lokální `./storage` layout kompatibilní s `apify run`;
  - Standby detekce přes `APIFY_META_ORIGIN=STANDBY` a port z `ACTOR_STANDBY_PORT`.
  - Návrh pro Apify: oficiální minimální Go runtime (env + input + push + charge). Totéž dělá každý, kdo actorizuje Go/Rust binárku.

## 2026-10-17 — Fáze 3: Czech Public Contracts Scraper (lokálně hotovo, nepublikováno)

- `actors/czech-public-contracts`:
  - Go, multi-stage Dockerfile, image 24 MB (golang:1.25-alpine → alpine + ca-certificates);
  - `.actor/actor.json`, `input_schema.json`, `dataset_schema.json` s view „Overview“.
- Data: open-data dumpy Registru smluv (`data.smlouvy.gov.cz`), parser převzatý z LeadsRAG `internal/rs`. Dumpy se parsují streamem během stahování, nic se neukládá na disk.
- Lokální běh nad reálnými daty: `keywords=[kampaň, video]`, 13.–16. 10., limit 5 → 5 smluv z prvního denního dumpu (5 MB) za ~1 s. V Dockeru stejně.
- Standby lokálně:
  - `GET /search` funguje, rozsah je omezený na 31 dní a 50 výsledků, protože každý dotaz streamuje dumpy a měsíční dump trvá minuty;
  - `POST /mcp` je minimální Streamable HTTP (stateless, JSON odpovědi), `initialize` / `tools/list` / `tools/call search_contracts` ověřené curl.
- Chyba, kterou jsem našel: `casZverejneni` je v pražském čase s offsetem. Při porovnání dne v UTC vypadly záznamy zveřejněné mezi půlnocí a 2:00 do předchozího dne. Opraveno na `Europe/Prague`; do binárky je přibalené `time/tzdata`, protože alpine image zoneinfo nemá. Pro Go Actory je to past: image bez tzdata, ale česká data s lokálními časy.
- GDPR:
  - strany bez IČO (fyzické osoby) se do výstupu vůbec nedostanou;
  - jméno schvalovatele parser zahazuje;
  - přílohy jsou jen URL na smlouvy.gov.cz.
- Věstník veřejných zakázek (CPV kódy) v0.1 **není**. Musím ověřit, jaké open-data rozhraní VVZ/NIPEZ nabízí, než ho začnu implementovat. Input proto zatím nemá CPV pole.
- Zbývá:
  - registrace a `apify login`, `apify push`;
  - Standby na platformě a ověření `https://<user>--czech-public-contracts.apify.actor/mcp`;
  - pay-per-result cena, review a publikace do Store, připojení MCP do Directoru.
  - Zapsat: jak dlouho trvá build Go image na platformě, jestli `webServerMcpPath` v actor.json Store skutečně čte.

## 2026-10-17 — Fáze 1b: TerraReality normalizace (offline)

- `terrareality/ingest`: whitelist model, takže fotky, popisky, jména a telefony nemají kam se uložit. Zahazují se už při dekódování a test to hlídá.
- Dispozice „2+kk“/„3+1“/„3 pokoje“/„2 szoba“/„garsoniéra“ se převádí na `rooms` + `kitchen`. Peníze ukládám jako minor units podle konvence TerraReality. Přepočet do EUR přes ECB daily (CZK/PLN/HUF), ukládám kurz i datum.
- TTL: 2 po sobě jdoucí runy bez výskytu vedou k `active=false`. Počítá se jen pro zdroje, které ten den opravdu běžely, takže výpadek PL Actoru nedeaktivuje Otodom. Historie cen je append-only.
- Názvy polí ve výstupu Store Actorů zatím neznám. Mapování v `normalize.go` jsou kandidátní klíče a musím je ověřit přes `fetch-actor-details` a smoke run.
- Kód je zatím tady v sandboxu. Do repa `terrareality` (goose migrace, sqlc, adaptér s mockem) ho přenese session terrareality.cz.

## 2026-10-17 — Registrace

- Účet „Ol1n / Personal“ je na Free plánu: **$5 kreditu měsíčně, 16 GB RAM** pro souběžné runy, bez karty. Onboarding „Get started“ má 4 kroky.
- Store má desítky tisíc Actorů v každé kategorii (Research 40k, Leads 17,6k). Výběr je tak spíš o důvěře (hodnocení, počet uživatelů) než o tom, jestli Actor vůbec existuje. Google Maps Scraper (compass) má 4,7★ z 1 818 hodnocení a 654k uživatelů.
- Rozpočet: $5/měsíc je tvrdý strop pro Fáze 1, 1b i 2 dohromady, takže každý placený run nejdřív odhadnu a nechám schválit.

## 2026-10-17 — Plugin a token

- Postup: `/plugin marketplace add apify/apify-claude-code-plugin`, pak `/reload-plugins` a `/plugin install apify@apify`. Instalace prošla, `/reload-plugins` ale hlásí **„0 plugin MCP servers“**. Agent `apify` a skilly se načtou hned, MCP nástroje (`search-actors` atd.) se v běžící session neobjeví. Je potřeba restart Claude Code, což plugin README výslovně neříká.
- Token jde ze schránky rovnou do `.env` (chmod 600) přes `pbpaste`, aniž by prošel chatem. Ověřil jsem ho neplaceným `GET /v2/users/me`: plán FREE, $5/měsíc, max 16 GB paměti na Actor, aktuální spotřeba $0.

## 2026-10-17 — Ceny a výběr Actorů (REST `GET /v2/acts/{id}`, zdarma)

- **3 ze 7 Actorů z plánu neexistují (404):** `conversational_kermis/realtysync-aggregator`, `automation-lab/otodom-property-scraper`, `unfenced-group/ingatlan-scraper`. Plán psaný s LLM pomocí obsahoval neexistující Store ID, takže je potřeba je před použitím ověřit. Alternativy najdu přes `search-actors`.
- Všechny použité Actory jsou pay-per-event. Ceny jsou **odstupňované podle plánu** (`eventTieredPricingUsd.FREE`), takže hodnota `eventPriceUsd` je `null`. Kdo čte jen základní cenu, nic neuvidí. To je past pro REST klienty.
- `compass/crawler-google-places` (42,9k uživatelů/30 d) na FREE:
  - místo $0.004, add-on kontakty z webu $0.002, add-on lead enrichment $0.10, filtry $0.001/místo;
  - i „Skip closed places“ je placený filtr;
  - add-on `scrapeContacts` nahrazuje Website Content Crawler z plánu, takže stačí jeden Actor místo dvou.
- `pringlesbitch/cz-reality-aggregator`:
  - $0.002/inzerát, Standby zapnutý;
  - jen **1 uživatel za 30 dní a 38 runů celkem**, takže důvěryhodnost je nízká;
  - input nemá inkrementální režim („jen nové“). Denní plný crawl Praha-pronájmy (stovky inzerátů) vyjde na ~$0.40–1/den, tedy **~$12–30/měsíc jen za Prahu**.
- `dadhalfdev/otodom-scraper` stojí $0.0018/výsledek, `bakos_bence/ingatlan-com` $0.002/výsledek + $0.005/start. Oba mají 1–4 uživatele/30 d.
- **Závěr pro Fázi 1b:** denní V4 metasearch se do Free plánu ($5/měsíc) nevejde ani pro samotnou Prahu. Na Free zbývá jednorázový smoke test (pár desítek inzerátů na zemi). Denní provoz znamená placený plán, nebo vlastní crawler jen pro Sreality. Cover letter: „pay-per-event je férové pro jednorázové dotazy, ale pro denní snapshot chybí inkrementální režim (delta od posledního runu)“.

## 2026-10-17 — MCP: OAuth a search-actors

- OAuth přes MCP: tool `authenticate` vrátí URL, uživatel ho otevře v browseru a callback na `localhost` doběhne. První URL ale propadla, protože uživatel ho mezi zprávami přehlédl a musel jsem vygenerovat nový. Po potvrzení se MCP nástroje objeví samy, bez restartu. Headless SPARK/JODA tuhle cestu použít nemůžou, tam zůstává token + REST.
- **Nekonzistence MCP vs. REST:** `fetch-actor-details` pro `pringlesbitch/cz-reality-aggregator` vrací `pricing.model: FREE`, prázdný popis i vývojáře a prázdné `outputSchema`. REST `GET /v2/acts/…` přitom vrací PAY_PER_EVENT $0.002/inzerát. Agent, který věří MCP, by run spustil s představou, že je zdarma. **Kandidát na issue** v `apify/apify-mcp-server`.
- `search-actors` vrací ceny rovnou pro tier uživatele (`userTier: FREE`) a je výrazně pohodlnější než REST.
- Náhrady za neexistující Actory (ceny FREE tier, uživatelé/měsíc):
  - Sreality:
    - `nice_dev/sreality-listings-scraper`: $0.00069/inzerát, filtr „Posted after“ umožňuje **inkrementální** denní run, ale má jen 4 uživatele;
    - `memo23/sreality-scraper`: $0.00099/inzerát + $0.009/start, 24 uživatelů, 5★;
    - oproti aggregatoru ($0.002) jsou 2–3× levnější.
  - Otodom: `trev0n/otodom-scraper` za $0.002/záznam, 41 uživatelů, filtr `postedDate`.
  - Ingatlan: `studio-amba/ingatlan-scraper` ($0.002 + $0.005/start, 7 uživatelů) vs. `bakos_bence/ingatlan-com` (4 uživatelé).
- Inkrementální run (jen nové od včerejška) se do Free plánu vejde pro Prahu: řádově 100–200 nových inzerátů denně × $0.0007 ≈ $2–4/měsíc. Rozbije ale TTL logiku: inzerát, který chybí v delta runu, nemusí být stažený. Deaktivaci by musel řešit občasný plný run nebo HEAD na `source_url`.

## 2026-10-17 — První placené runy (schváleno uživatelem: A+B+C, odhad ≈ $0.49)

### B — TerraReality smoke test (cesta: MCP `call-actor`, `waitSecs: 0` + `get-actor-run`)
| Actor | výsledek | čas | vyplněnost klíčových polí |
|---|---|---|---|
| `nice_dev/sreality-listings-scraper` (Praha, pronájem) | 30/30 | 5 s | cena, m², dispozice, GPS 30/30, patro 0, `publishedAt` 0 (bez `extractDetails`) |
| `trev0n/otodom-scraper` (Varšava, wynajem) | 30/30 | 7 s | cena, m² 30/30, **pokoje 0, GPS 0** (bez detailů) |
| `bakos_bence/ingatlan-com` (Budapešť, kiado) | 19/30 | 60 s | **cena 0/19**, pokoje 0, Cloudflare zablokoval další stránky. Pro TerraReality nepoužitelné |
| `studio-amba/ingatlan-scraper` | **nespuštěno** | – | 2× „Your remaining usage of $4.999774 … isn't enough for this run“, i s 1 GB a 10 min timeoutem |

- `nice_dev` má **vestavěné `onlyNew` + `stateKey`** (paměť mezi runy, už viděné inzeráty se neúčtují) a `privateSellerContacts: false`, takže GDPR i inkrementální režim jsou vyřešené přímo v Actoru. Pro denní Praha-only pipeline je to nejlepší kandidát.
- Hláška u `studio-amba` je zavádějící: zbývalo $4.99, run by stál ~$0.07. Apify nejspíš kontroluje nejhorší možnou cenu runu (Actor kupuje Bright Data), ale chybová zpráva to neřekne. **Kandidát na feedback.**
- `terra-ingest` nad reálnými daty: Sreality i Otodom normalizace proběhla 60/60, kurz ECB z 16. 10. Ingatlan byl celý přeskočen kvůli chybějící ceně. Mapování jsem musel doplnit o `propertyUrl` a `surface`.
- Moje chyba: `apify-run` zapisoval položky tak, jak je API vrací (pretty-printed přes víc řádků), takže výstup nebyl validní JSONL. Opraveno přes `json.Compact`.

### C — smoke test „5 coffee shops in Prague → CSV“
- **Přes agenta `apify` (plugin) to nešlo:** subagent dostal instrukce MCP serveru, ale ne jeho nástroje, takže OAuth přihlášení z hlavní session se do pluginového subagenta nepropíše. CLI nebylo nainstalované a `.env` jsem mu zakázal číst. Doporučená cesta z plánu („vždy jít přes agenta `apify`“) tedy po OAuth instalaci nefunguje. **Kandidát na issue** v `apify/apify-claude-code-plugin`.
- Přes MCP `call-actor` přímo: `maxTotalChargeUsd: 0.05` bylo odmítnuto hláškou **„Maximum cost per run is less than the allowed minimum of $0.50“**. Actor má minimální strop $0.50 i pro run za ~$0.02. Run pak trval 11 s a vrátil 5 kaváren; CSV jde rovnou z REST `?format=csv&fields=…`.

### A — leady (cesta: Go REST `apify-run`)
- Praha: 28 míst s add-onem kontakty z webu, runtime 1 min 40 s, odhad $0.168. Brno: 21 míst, 1 min 57 s, odhad $0.126.
- **`usageTotalUsd` se po skončení runu ještě mění:** Praha hlásila v okamžiku SUCCEEDED $0.1582, o pár minut později $0.1642, Brno $0.1142 → $0.1202. Doúčtují se nejspíš compute a platformní poplatky. Klient, který čte cenu hned po doběhnutí (jako můj `apify-run`), tak podhodnocuje zhruba o 4–5 %.
- Výsledek po `leads-normalize`:
  - 49 míst, 0 duplicit podle placeId/domény;
  - **kontakt má 94 %** (telefon 90 %, e-mail 59 %);
  - add-on kontaktů tedy nahrazuje druhý Actor z plánu.
- Kvalita: zhruba 6 ze 49 nejsou cílové firmy. Mezi ně patří indický SEO freelancer, herní muzeum/arcade, Czech Game Developers Association, sexshop, suvenýry ZOO Brno a jeden pravděpodobný duplikát se jménem napsaným jinak (MONTÁŽE S.K.). Dotaz „e-shop“ na Google Maps vrací kamenné prodejny. Na použitelnou kvalitu je potřeba filtrovat `categoryName`.
- **Propojení s Registrem smluv 0/49:** názvy na Google Maps obsahují marketingové přípony („… - úklidové služby“, „| Montáž klimatizace“), takže je `NameKey` nespáruje. Exporty LeadsRAG jsou navíc jen top-scored podmnožina. Spojovat se musí přes IČO, třeba z webu firmy (patička) nebo přes ARES podle názvu a adresy. To je další krok, ne úkol pro Apify.

### Souhrn nákladů
| run | odhad | skutečnost |
|---|---|---|
| A Praha + Brno (Go REST) | $0.294 | $0.284 |
| B Sreality / Otodom / Ingatlan (MCP) | $0.16 | $0.139 (Ingatlan bez cen) |
| C kavárny (MCP) | $0.03 | $0.020 |
| **účet celkem** | **$0.49** | **$0.444 z $5** |


## 2026-10-17 — Fáze 1: filtr kategorií + IČO přes web a ARES (zdarma)

- Filtr podle `categories` z Google Maps: pro každý segment seznam povolených kategorií, plus globálně zakázané kategorie (muzeum, sex shop, suvenýry, tabák, sdružení, hobbymarket, sportovní hala). **Vyřadil 10 ze 49**, všechny oprávněně. Seznam je v `leads/dropped.csv` s důvodem, aby šel zkontrolovat.
  - Kategorie z Google jsou u herních studií slabé: Gametoria a Icarus Games mají jen „Ústředí společnosti“. U téhle obecné kategorie proto vyžaduju „game/hry“ v názvu nebo na webu. Warhorse („Vývoj softwaru“) projde bez toho. První verze pravidla Warhorse omylem vyhazovala a chytil to až test.
- IČO: **29 z 39 leadů (74 %)**:
  - 15× z webu firmy, kde je IČO povinně v patičce nebo na stránce kontaktů, s kontrolou kontrolní číslice a ověřením v ARES;
  - 10× hledání v ARES podle očištěného názvu a PSČ z adresy Google;
  - 3× shoda normalizovaného názvu a obce;
  - 1× web s IČO jiné firmy, označený `web_jine_jmeno` k ruční kontrole (jobindustry.cz → INFOCUBE s.r.o.).
- Web jako zdroj IČO je přesnější než hledání podle jména: odhalí provozovatele značky (Expertúklid → Cleanplace s.r.o., PPC Profits → Digiadvert s.r.o.), kterého by hledání podle názvu nenašlo.
- Velikost firmy z ARES RES (`statistickeUdaje.kategoriePoctuPracovniku`), převedená číselníkem ČSÚ 579 z LeadsRAG:
  - 4 OSVČ, 4× „Bez zaměstnanců“, 4× „Neuvedeno“;
  - největší je Warhorse (250–499);
  - moje první tabulka napsaná zpaměti byla posunutá o jednu kategorii, chytil to až kód 130, který v ní chyběl.
- Spárování s Registrem smluv přes IČO: stále 0. Nalezená IČO nejsou v top exportech LeadsRAG (`export/leads_*.csv` je jen top-scored podmnožina). Plné spárování vyžaduje dotaz do LeadsRAG DB (207k organizací).
- Celé obohacení stojí $0 a trvá ~66 s na 39 leadů (rate limit ARES 150 ms/req + web fetch).

## 2026-10-17 — Fáze 1b: denní Praha přes Apify Task (schedule zatím ne)

- Uživatel schválil denní provoz (~$2–4/měsíc). Vytvořil jsem Apify Task `terra-praha-rent-daily` (ID `mEdAEfC1LaEfK94Rm`), vstup je v `inputs/terra-praha-task.json`:
  - Actor `nice_dev/sreality-listings-scraper`: Praha, pronájem bytů, `onlyNew` + `stateKey: terra-praha-rent`, `maxItems: 120`, `privateSellerContacts: false`, bez detailů;
  - task options: 512 MB, 10 min timeout, `maxTotalChargeUsd: 0.15`. Task tenhle strop přijme, u runu přes MCP měl Google Maps Actor minimum $0.50.
- **Schedule jsem nevytvořil:** `POST /v2/schedules` zablokoval permission classifier v Claude Code jako „Unauthorized Persistence“. Trvalý cron, který sám utrácí, musí potvrdit uživatel. Definice je připravená v `inputs/terra-praha-schedule.json` (06:10 Europe/Prague, exclusive).
- Ověřovací ruční run tasku:
  - 120 inzerátů, **30 přeskočeno jako „already delivered by a previous run“**, protože testovací run B běžel se stejným `stateKey`;
  - run trval 3 s.
- **Cena runu se doúčtovává později a jde o řád, ne o procenta:** v okamžiku SUCCEEDED `usageTotalUsd = $0.01` (jen start), o minutu později **$0.0928**, protože PPE události za inzeráty dorazí až po skončení. `waitForFinish` tak cenu neukáže. Cenu je potřeba číst se zpožděním, nebo počítat z počtu položek. Plán ~$0.093/den ≈ **$2.8/měsíc** je v rozpočtu.
- `terra-ingest -task actor=taskID`: vezme poslední úspěšný run tasku, ID runu si uloží vedle stavu (`state.json.runs`), takže druhé spuštění nic neudělá.
- **Opravená chyba v návrhu:** inkrementální run obsahuje jen nové inzeráty, takže TTL („2 runy chybí, inzerát je neaktivní“) by po dvou dnech deaktivoval skoro celou databázi. Zdroje z `-task` a `-incremental` se teď do TTL nepočítají. Deaktivace pro Prahu tím **zatím neexistuje**. Řešení na později: jednou týdně plný run bez `onlyNew` (~4–6 tis. inzerátů × $0.00069 ≈ $3–4, nad rozpočet Free), nebo HEAD na `source_url` jen u aktivních (zdarma, ale 1 request na inzerát na portál).
- Stav po ingestu: 150 aktivních Sreality (Praha) + 30 Otodom, medián nájmu Praha **1 192 €/měsíc** (ECB 16. 10.).

## 2026-10-18 — Fáze 1b: ingest nasazený na JODU

- Director schválil s podmínkami: JODA (x86_64, linger), 06:40, `Persistent=true`, `RandomizedDelaySec=60`, `MemoryMax=128M`, `Nice=10`, chyby přes exit≠0, žádné surové datasety na disku (JODA má disk plný ze 73 %).
- Unity jsou v `deploy/joda/terra-ingest.{service,timer}`, deploy přes `make deploy-joda`. Binárka je statická linux/amd64 (6,6 MB), `.env` jsem přenesl přes ssh stdin s `umask 077`, takže token se nikde nevypsal.
- Ověřovací běh na JODĚ: success, 120 inzerátů, stav 112 KB (~930 B/inzerát, odhad +3 MB/měsíc). Rotaci stavu vyřeším až s napojením na DB TerraReality.
- **Apify schedule chybí:** classifier v Claude Code zablokoval `POST /v2/schedules` dvakrát, podruhé i po výslovném svolení uživatele. Uživatel ho založí v konzoli. Do té doby timer jen hlásí „no new successful run“ s exit 0.
- Architektura: Apify cloud stahuje data a nese placený provoz s capem na task. Doma běží jen levný idempotentní pull přes REST. Pro cover letter: Apify jako „cron + scraper“ služba, kterou neprovozuju; vlastní infra jen konzumuje dataset.

## TODO (čeká na uživatele / platformu)

- [ ] Registrace, token do `.env`, `/plugin marketplace add apify/apify-claude-code-plugin` + `/plugin install apify@apify`.
- [ ] Smoke test agenta `apify` („5 coffee shops in Prague → CSV“).
- [ ] Fáze 1: odhad ceny Google Maps runu (`inputs/maps-leads.json`, 7 dotazů × 5 míst = 35) → schválit → `apify-run` → `leads-normalize`.
- [ ] Fáze 1b: `fetch-actor-details` 4 Actorů, odhad denní ceny, smoke run, Standby vs. batch.
- [ ] Fáze 2: výběr Reddit + Play/App Store Actorů, schedule a webhook. Nasazení na JODU jen po OK od Directoru, cloudflared hostname po OK od uživatele.

## 2026-10-18 — Fáze 1b: Apify schedule založený

- Uživatel založil schedule `terra-praha-rent-daily` (ID `evrFu0bH4tqrW0rVC`) sám přes `curl` na `POST /v2/schedules`. Classifier v Claude Code to předtím dvakrát zablokoval jako trvalou perzistenci.
- Definice je v `inputs/terra-praha-schedule.json`. Původně se nezapsala, protože ji blokace odnesla spolu s POSTem, a první pokus uživatele skončil chybou curl „error encountered when reading a file“.
- Cron je `10 6 * * *` Europe/Prague, schedule je exclusive. API vrací `nextRunAt` v UTC (`2026-10-19T04:10:00Z`, tedy 6:10 SELČ). Pull na JODĚ běží v 6:40, rezerva 30 min na run, který trvá pár sekund.
- Pipeline je kompletní: Apify schedule → task (cap $0.15) → dataset ← `terra-ingest.timer` na JODĚ (REST pull, idempotentní podle run ID).

## 2026-10-19 — Re-check nekonzistence MCP vs. REST před založením issue

- `fetch-actor-details` pro `pringlesbitch/cz-reality-aggregator` dnes vrací **správně**: `PAY_PER_EVENT`, $0.002/inzerát, `userTier: FREE`, popis i vývojář vyplněné. Nález ze 17. 10. (`pricing.model: FREE`, prázdný popis) **už neplatí**. Mezi 14. a 16. 10. vyšly verze MCP serveru 0.18.2–0.18.4, ale 17. 10. jsme už běželi proti hostovanému serveru, takže příčina mohla být i v cache metadat na straně Store. Issue proto nezakládám; v cover letteru změnit na „already fixed by the time I checked again“.
- Kontrola na dalších dvou Actorech s tiered pricingem (`nice_dev/sreality-listings-scraper`, `compass/crawler-google-places`): MCP vrací `tieredPricing` po tierech a souhlasí s REST `eventTieredPricingUsd`. `outputSchema` je `{}` jen u aggregatoru (12 uživatelů celkem), protože se odvozuje z nedávných runů; to není chyba.
- Upřesnění k 17. 10.: REST `eventPriceUsd` je `null` **jen u tiered událostí** (compass, nice_dev); aggregator má plochou cenu a `eventPriceUsd: 0.002` vyplněné. Klient tedy musí číst obě pole.
- Drobnost: MCP vrací u `compass/crawler-google-places` `description: ""`, REST má popis o 284 znacích. Neškodné, ale je to další místo, kde MCP a REST nesouhlasí.
- Duplicity v `apify/apify-mcp-server`: #905 „MCP Apps shows different Pricing info than the public Actor page“ (otevřené od 05/2026, bez komentářů, týká se widgetu) a #1543 „Gate pricing hints on fetch-actor-details availability“. Kdyby se chyba vrátila, patří repro jako komentář k #905, ne nové issue.
- `apify/apify-claude-code-plugin` má issues zapnuté a **žádné založené**, takže nález „subagent `apify` nedostane MCP nástroje z OAuth“ by byl první. Re-check viz další záznam.

## 2026-10-19 — Re-check: subagent `apify` z pluginu nemá MCP nástroje (platí)

- Repro dnes: subagent `apify` (plugin `apify@apify` 1.0.2, Claude Code 2.1.303) hlásí, že má jen `Read`, `Bash` a hand-back; žádný `mcp__*` nástroj, takže `search-actors` nemůže zavolat. V hlavní session jsou nástroje po OAuth dostupné jako `mcp__plugin_apify_apify__search-actors` atd.
- Pravděpodobná příčina: `agents/apify.md` má `tools: [Read, Glob, Grep, Bash, mcp__apify]`. Plugin MCP servery jsou v Claude Code namespacované jako `mcp__plugin_<plugin>_<server>__<tool>`, takže `mcp__apify` odpovídá jen uživatelsky nakonfigurovanému serveru jménem `apify`, ne tomu z pluginu (`.mcp.json` pluginu: server `apify`, `https://mcp.apify.com/?client=claude+code+plugin`). Pro plugin by to musel být `mcp__plugin_apify_apify`.
- Důsledek: doporučená cesta z README („vždy jít přes agenta apify“) po OAuth nefunguje; agent spadne na „Route 1 přes Apify CLI“, které není nainstalované. Hlavní session musí volat MCP sama.
- Repo `apify/apify-claude-code-plugin` má issues zapnuté a prázdné; návrh issue je připravený, založení schvaluje uživatel.
- Založeno jako [apify/apify-claude-code-plugin#27](https://github.com/apify/apify-claude-code-plugin/issues/27) (2026-10-19).

## 2026-10-19 — Fáze 1b: první plně automatický běh a rotace tokenu

- Řetězec schedule → task → dataset → JODA proběhl bez zásahu: run tasku v 06:10 CEST (API: 04:10 UTC) za $0.0928, `terra-ingest.timer` v 06:41 stáhl 120 nových inzerátů, 0 přeskočených, stav 240 inzerátů / 224 KB. Odhad ~$2.8/měsíc drží.
- Token prošel chatem (uživatel vložil výstup `cat .env`), proto rotace: nový token v konzoli, do `.env` lokálně i na JODĚ přes `pbpaste` a ssh stdin s `umask 077`, ověřeno `GET /v2/users/me` z obou strojů. Starý token uživatel zneplatnil. Poučení pro postupy: ani „ukaž mi token“ se nemá řešit výpisem do terminálu, který se pak vkládá do chatu; stačí odkaz na konzoli.

## 2026-10-25 — RÚIAN Actory D a B: první nasazení a běhy na Apify

- Bez Apify CLI: Actory založené přes `POST /v2/acts` se zdrojem `GIT_REPO` a URL `https://github.com/lioilsources/apifysandbox#master:actors/<složka>` (větev + podsložka monorepa funguje, build klonuje veřejné repo). Build stojí 0,04–0,05 CU. Go balíček sdílený mezi Actory musí být vendorovaný, protože build vidí jen podsložku.
- `pricingInfos` (pay-per-event) při založení vrací `operation-not-allowed`, při update `cannot-monetize-without-payout-billing-info`: ceny jdou nastavit až po vyplnění výplatních a fakturačních údajů v konzoli (uživatel).
- Validace vstupního schématu při buildu: **každé pole musí mít `description`**, jinak build selže („Field schema.properties.X.description is required").
- Dataset se schématem (`storages.dataset`) **validuje položky při zápisu**: nesoulad typu vrací `400 schema-validation-error` s cestou k poli. Vlastní Go runtime musí logovat tělo odpovědi, jinak je v logu jen „HTTP 400" (doplněno).
- Ceny běhů (FREE plán):

| Běh | Čas | CU | USD | RAM max |
|---|---|---|---|---|
| D batch (5 dotazů) | 7 s | 0,0003 | 0,0001 | 18 MB |
| B batch, celá sklizeň 266 desek | 317–450 s | 0,022–0,031 | 0,004–0,006 | 71–93 MB |

- Standby + MCP (`https://<user>--<actor>.apify.actor/mcp`, `Authorization: Bearer`) funguje; studený start D ~1 s, gazetteer RÚIAN se načte do ~10 s, do té doby nástroje vrací srozumitelné `isError`.
- **Geoblokace z datacentra Apify:** z 266 úředních desek se z Apify nepřipojí ~90 (lokálně z ČR jen ~30 chyb), z toho 41 na jednom hostingu `78.156.158.0/24`. Sklizeň se tím natahuje (opraveno: 10 s timeout na spojení).
- Apify Proxy na FREE: k dispozici jen 5 datacentrových IP (`BUYPROXIES94952`), `RESIDENTIAL` 0; `country-CZ` na platformě vrací 403 Forbidden a externí přístup k proxy (`proxy.apify.com` z lokálu) je zakázaný („Proxy external access feature isn't enabled"). Plné pokrytí Actoru B z Apify tedy vyžaduje placený plán s rezidenční proxy CZ, nebo sklizeň z české IP.
