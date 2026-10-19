# Trading bot s Apify — plán experimentu

Datum: 2026-10-18. Navazuje na `00-APIFY_PLAN.md` (Fáze 1b ukázala vzor: Apify cloud stahuje, doma běží jen levný pull).
Ceny Actorů níže jsou z `search-actors` pro FREE tier k 18. 10. 2026, existence ověřená. Nic z toho není investiční doporučení.

## 0. Teze, kterou testujeme (a proč je formulovaná takhle)

> Levná, pomalá (denní) strategie typu trend/momentum na likvidních instrumentech, doplněná o veřejný sentiment (Reddit, Google News, Google Trends) stažený přes Apify, **neprodělá víc než náklady** a sentiment jako překryv **měřitelně zlepší** baseline bez něj.

Není to „bot vydělá“. Důvody:
- Veřejná data (Reddit, zprávy) jsou v ceně během minut. Edge z nich pro retail bota je malý nebo žádný. Testujeme, jestli vůbec existuje.
- Statistika je proti nám: aby šel Sharpe 0.5 odlišit od nuly na 95 %, potřebujeme řádově **15+ let** dat; Sharpe 1 zhruba 6 let. Za týdny provozu se dá dokázat, že strategie **je rozbitá**, ne že je zisková. Proto má plán tvrdé stop-podmínky, ne cílový výnos.
- Čistý zisk experimentu je i tak kladný: naučíme se celý řetězec data → signál → risk → exekuce → monitoring, a vznikne druhý případ pro cover letter (Apify jako data layer vs. bezplatná oficiální API).

## 1. Role Apify (a kde Apify **ne**)

| vrstva | nástroj | poznámka |
|---|---|---|
| ceny pro backtest a signály | **ne Apify**: burzovní API (Kraken public REST zdarma), Stooq/Yahoo CSV, FRED, EIA API, CFTC COT CSV | zdarma, spolehlivé, bez latence a bez credit capu |
| exekuce | **ne Apify**: API burzy/brokera | Apify neumí držet klíče k účtu ani garantovat latenci |
| sentiment Reddit | `harshmaur/reddit-scraper` ($0.002/post + $0.02/start, 16,7k uživatelů, 4,6★, `postedAfter`, volitelná AI klasifikace $0.0005) nebo `clearpath/reddit-subreddit-posts-scraper` ($0.002/post, start $0.0005) | denní delta přes `postedAfter`, stejný vzor jako Sreality `onlyNew` |
| zprávy | `lhotanova/google-news-scraper` ($0.002/článek + $0.005/start, `dateFrom`) | `easyapi` má start $0.09, nevhodný na denní malé runy |
| zájem veřejnosti | `apify/google-trends-scraper` (oficiální, $0.003/řádek, 16k uživatelů, 3,7★) | týdně stačí |
| term structure komodit | `scrapemint/commodity-futures-settlements` ($0.004/řádek, 17 uživatelů) | denní settlement CME, contango/backwardation = carry signál; jediný Actor, který dává něco, co zdarma těžko seženu v jednom JSONu |
| ad-hoc research | Apify MCP (`search-actors`, `call-actor`) z Claude/Directoru | jen v research fázi, **ne v produkční smyčce** |

Pravidlo: **LLM a MCP nerozhodují o obchodech.** LLM běží jen dávkově (okno 13–07, `openclaw-default`) a mění text na strukturované featury (ticker, směr, síla, novost). Rozhodnutí dělá deterministický kód, který jde backtestovat. Agentní „zeptám se Claude, co koupit“ se nedá testovat ani reprodukovat.

### Rozpočet Apify
Free plán má $5/měsíc, z toho ~$2,8 bere Praha (Fáze 1b). Zbývá ~$2 → ~$0,065/den → **~30 Reddit postů denně**. To je na sentiment málo.
Varianty:
1. **Zdarma mimo Apify:** Reddit API (OAuth app, 100 req/min, pro osobní použití zdarma) + Google News RSS. Apify by zůstalo jen na Trends a CME settlements (~$0,5/měsíc).
2. **Placený plán Apify** (nejnižší tier je řádově desítky $/měsíc). Dává smysl jen pokud chceme Apify vzorec ověřit „naostro“ pro cover letter.
3. Ořezat Prahu (např. `maxItems 60`) a uvolnit ~$1,4/měsíc.

Doporučení: varianta 1 pro provoz, Apify Reddit Actor jen na **jednorázový historický backfill** sentimentu (např. 12 měsíců × 3 subreddity × top posty ≈ 3 000 postů ≈ $6, jednorázově, schválit předem). Zjištění pro FINDINGS: pro osobního bota vyhrává oficiální bezplatné API; Apify vyhrává tam, kde API není (Trends, CME stránka, TradingView).

## 2. Kde se obchoduje a z jakých peněz

Dvě oddělené kasy, nikdy se nemíchají:
- **Apify kredit** platí data. Cap přes `maxTotalChargeUsd` na task, jako u Prahy.
- **Obchodní kapitál** je na účtu u burzy/brokera, API klíč jen s právy *trade*, **bez withdraw**, IP whitelist na JODU. Klíč v `.env` (600), stejně jako Apify token.

Výběr trhu pro retail bota s malým kapitálem:

| trh | pro | proti |
|---|---|---|
| **krypto spot** (Kraken/Coinbase, API zdarma, 24/7, testnet/paper) | nejmenší tření pro bota, drobné velikosti, sandbox účty | vysoká volatilita, daně z každého prodeje |
| **komodity přes ETC/ETF** (IBKR/Degiro: zlato, ropa, široký koš) | reálně obchodovatelné s 10k CZK, denní rebalance | API u IBKR je těžkopádné (TWS), Degiro API neoficiální |
| komodity přes futures | „pravé“ komodity | kontrakt WTI = 1 000 barelů, margin tisíce USD, pro náš kapitál ne |
| komodity přes CFD (XTB ap.) | malé velikosti | spread + overnight swap sežere pomalou momentum strategii |

Doporučení: **začít na krypto spotu** (nejlevnější smyčka pro ověření celé pipeline), komodity zařadit jako **ETC koš přes IBKR** až ve fázi 4, pokud fáze 3 projde. Odpověď na „které komodity kupovat“ není seznam, ale **pravidlo**: univerzum ~10 likvidních instrumentů, každý týden seřadit podle momenta (12-1 měsíc), držet horní třetinu, hotovost když je trh pod 200denním průměrem. U komodit přidat carry (backwardation = plus) z CME settlements.

Daně (CZ): každý prodej krypta je zdanitelná událost (ostatní příjem; od 2025 osvobození při držení 3 roky nebo do 100k CZK příjmu ročně). Bot musí vést log obchodů ve formátu použitelném pro přiznání od prvního dne.

## 3. Architektura

```
  Apify cloud (schedules, cap na task)        zdarma API
  ├─ reddit-daily  (postedAfter=včera)        ├─ Kraken OHLCV (denní)
  ├─ news-daily    (dateFrom=včera)           ├─ Stooq/Yahoo ETF ceny
  ├─ trends-weekly                            ├─ FRED (USD index, sazby)
  └─ cme-settle-daily                         └─ CFTC COT (pátek)
          │ REST pull (idempotentní podle run ID, jako terra-ingest)
          ▼
  JODA  ~/deploy/tradebot  (systemd --user timery, časy podle Directora)
  ├─ 07:05 ingest      → SQLite: prices (Kraken), texts, trends, settlements (Apify REST pull)
  ├─ 14:15 llm-batch   → texts → features (ticker, směr −1..1, novost) přes openclaw-default
  ├─ 18:00 signal      → ranking, cílové váhy, návrh objednávek; na LLM nečeká, bez featur jede baseline
  ├─ HITL              → nový Telegram bot: inline tlačítka Schválit/Zamítnout (fáze 3 auto-paper, HITL se jen cvičí)
  ├─ 18:30 execute     → paper engine (fáze 4 live je samostatné rozhodnutí uživatele, ne krok plánu)
  ├─ každou h  risk    → drawdown, expozice, stale data → kill switch
  └─ 20:00 report      → Telegram přes nového bota + dashboard JSON (127.0.0.1)
```

Zdroje cen (ověřeno 18. 10.): **Binance klines** pro historický backfill (zdarma, od 2017, 1 000 svíček/request), **Kraken OHLC** pro denní aktualizaci a paper fill ceny (jen 720 svíček zpět, 633 USD párů). Stooq je za JS challenge, nepoužitelný.

Komponenty v Go (reuse `pkg/apifyrest`, `cmd/terra-ingest` vzor):
- `tradebot/data`: adaptéry (Kraken, Stooq, Apify datasety), vše do SQLite, append-only.
- `tradebot/features`: momentum, volatilita, carry, sentiment agregace (denní skóre per instrument, počet zmínek, z-score oproti 30 dnům).
- `tradebot/strategy`: čistá funkce `(features, portfolio) → target weights`. Žádné I/O, aby šla backtestovat.
- `tradebot/risk`: limity, kill switch, deník.
- `tradebot/exec`: `Broker` interface: `Paper`, `Kraken`. Stejný kód ve fázi 3 i 4.
- `cmd/tradebot-backtest`, `cmd/tradebot-run`, `cmd/tradebot-dash` (statický HTML + JSON na JODĚ, veřejně jen po schválení cloudflared hostnamu).

Nasazení na JODU **až po OK Directora** (timer, disk, paměť). LLM jen v okně 13–07. Telegram jednosměrně.

## 4. Algoritmus (v0, baseline)

```
každý obchodní den D po uzávěrce:
  U = univerzum (krypto: BTC, ETH, SOL, + 5 dalších podle objemu; ETC: GLD, SLV, USO, DBC, DBA, CPER, UNG, PPLT)
  pro i v U:
    mom_i  = cena(D-21) / cena(D-252) - 1            # 12-1 měsíční momentum (poslední měsíc vynechaný)
    vol_i  = směrodatná odchylka denních výnosů za 60 dní × √252
    trend_i = cena(D) > SMA200(D)
    carry_i = (front - second) / second                # jen komodity, z CME settlements
    sent_i  = z-score denního sentiment skóre za 30 dní # jen pokud je zapnutý overlay

  kandidáti = { i : trend_i a mom_i > 0 }
  pořadí podle mom_i (+ 0.5·carry_i u komodit)
  vybrat top N = 3
  váha_i = (cíl_vol / vol_i) / N, cíl_vol = 10 % p.a., součet vah ≤ 1, zbytek hotovost
  overlay (varianta B): váha_i × clamp(1 + 0.25·sent_i, 0.5, 1.5)   # sentiment jen škáluje, nikdy neotáčí směr

  rebalance jen když |váha_cíl − váha_aktuální| > 5 p. b. (šetří poplatky)
  objednávky: limit post-only, max 1 % od mid, platnost do dalšího dne
```

Risk pravidla (tvrdá, mimo strategii):
- max 40 % kapitálu v jednom instrumentu, max 100 % hrubá expozice, bez páky, bez shortů;
- **kill switch**: drawdown od vrcholu > 15 % alokovaného kapitálu → vše do hotovosti, bot se vypne, zapne jen člověk;
- stale data (ceny starší 36 h, nebo chybí settlement/sentiment 3 dny) → žádné nové obchody, jen držet;
- rozdíl live vs. paper fill > 0,3 % mediánově za 20 obchodů → zpět do paperu.

Varianty pro backtest: A = baseline bez sentimentu, B = s overlayem, C = jen sentiment (long top-3 podle sent). **Pokud B neporazí A po nákladech ve walk-forwardu, sentiment se zahodí** a Apify v projektu zůstává jen pro CME settlements a cover letter.

## 5. Pre-screen před reálnými penězi (brány)

| brána | co | kritérium pro průchod |
|---|---|---|
| G0 rozhodnutí | trh, kapitál, cíl | uživatel: částka, kterou je ochotný celou ztratit (návrh 5–10k CZK) |
| G1 backtest | 5+ let denních dat, náklady 0,26 % taker / 0,16 % maker Kraken, slippage 0,1 %, walk-forward 3 roky in / 1 rok out | A: Sharpe > 0,5 po nákladech, max DD < 30 %, ne horší než buy-and-hold BTC o víc než 30 % výnosu p.a.; B > A aspoň o 0,1 Sharpe out-of-sample, jinak overlay padá |
| G2 paper | ≥ 8 týdnů identický kód, simulované fill ceny z reálného orderbooku | 0 chyb pipeline, které by v live znamenaly špatný obchod; tracking vůči backtestu v tolerance; všechny alerty došly |
| G3 live micro | 3 měsíce, kapitál z G0, každý obchod schválený příkazem | fill vs. paper < 0,3 %; žádné porušení risk limitů; náklady (Apify + poplatky) < 1 % kapitálu/měsíc |
| G4 škálování | | jen pokud G3 projde a Sharpe za 3 měsíce ≥ 0; i pak max 2× kapitál na krok |

Žádná brána se nepřeskočí ani „na zkoušku“. Přeskočení je ztráta disciplíny, kterou bot neopraví.

## 6. Dashboard (JODA, statický HTML + JSON, 1 obrazovka)

```
┌─ TRADEBOT · paper · den 23/56 ──────────────────────────────── stav: OK  kill: OFF ─┐
│ Kapitál 10 000 → 10 180 CZK (+1,8 %)   DD −2,1 % (limit −15 %)   Sharpe 56d: 0,4  │
│ Benchmark BTC hold: +3,2 %     Alfa vs. bench: −1,4 p. b.     Náklady: 0,21 %       │
├─ equity (56 d) ──────────────────────────┬─ pozice ─────────────────────────────────┤
│  ▁▂▂▃▃▂▃▄▄▅▅▄▅▅▆▆▅▆▆▇  bot              │ BTC  34 %  mom +0,41  vol 48 %  sent +0,3│
│  ▁▂▃▃▄▅▅▆▆▇▇▇█▇▇██▇██  bench            │ ETH  28 %  mom +0,22  vol 61 %  sent −0,1│
│                                          │ SOL  18 %  mom +0,18  vol 85 %  sent +1,2│
│                                          │ cash 20 %                                │
├─ signály dnes ───────────────────────────┼─ data health ────────────────────────────┤
│ kandidáti 5/8 · rebalance: SOL −4 pb (ne)│ Kraken OHLCV   07:02  ✓  8/8 instr.      │
│ návrh objednávek: 0                      │ reddit-daily   06:14  ✓  212 postů $0,43 │
│ poslední obchod: 14. 10. ETH buy 0,05    │ news-daily     06:20  ✓  38 článků $0,08 │
│                                          │ cme-settle     06:30  ✗  2 dny stale  !  │
│                                          │ llm-batch      13:31  ✓  250 textů 9 min │
├─ Apify kredit ───────────────────────────┼─ posledních 5 alertů ────────────────────┤
│ měsíc: $3,92 / $5,00  (Praha $1,80)      │ 17.10 cme-settle run FAILED (timeout)    │
│ odhad do konce: $5,30  !                 │ 15.10 sent z-score SOL > 2 (hype)        │
└──────────────────────────────────────────┴──────────────────────────────────────────┘
```

Co na něm **musí** být a proč: equity vs. benchmark (bot, který nedělá nic, je legitimní výsledek), drawdown vůči limitu, stav kill switche, data health s cenou runů (Apify účtuje až po skončení runu, viz FINDINGS), poslední alerty. Co tam **není**: tick grafy, indikátory, cokoliv, co svádí k ručnímu zásahu.

## 7. Jak poznám, že to nefunguje

| signál | práh | akce |
|---|---|---|
| drawdown | > 15 % alokace | kill switch, hotovost, post-mortem |
| B vs. A out-of-sample | Sharpe B ≤ A | sentiment overlay zahodit, Apify Reddit/News vypnout |
| live fill vs. paper | > 0,3 % mediánově za 20 obchodů | zpět do paperu, zmenšit objednávky |
| obrat (turnover) | > 50 %/měsíc | strategie honí šum, zvýšit práh rebalance |
| náklady | Apify + poplatky > 1 % kapitálu/měsíc | strategie nemá šanci, zastavit |
| data health | 3 dny po sobě stale nebo FAILED | žádné nové obchody, nahlásit |
| rozklad signálu | korelace mom_i s následným 20d výnosem za 12 měsíců ≤ 0 | zpochybnit baseline, ne ladit parametry |
| chování člověka | ruční override > 1×/měsíc | problém je v důvěře k systému, ne v systému; řešit pravidly |
| po 12 měsících live | Sharpe < 0 nebo horší než hold | ukončit experiment, sepsat FINDINGS; to **je** platný výsledek |

Co **není** důkaz, že to nefunguje: 3 ztrátové týdny v řadě, jeden špatný obchod, horší měsíc než BTC hold. Co **není** důkaz, že to funguje: ziskový měsíc.

## 8. Harmonogram a náklady

| fáze | čas | Apify | kapitál |
|---|---|---|---|
| F1 data layer (adaptéry, SQLite, 1 Apify schedule na Trends/CME, historický Reddit backfill) | 2 týdny | ~$6 jednorázově + $0,5/měsíc | 0 |
| F2 backtest A/B/C, walk-forward, report do FINDINGS | 2–3 týdny | 0 | 0 |
| F3 paper + dashboard + alerty na JODĚ (po OK Directora) | 8 týdnů | $0,5–2/měsíc | 0 |
| F4 live micro, HITL příkazem | 3 měsíce | totéž | 5–10k CZK |
| F5 škálování | dle G4 | | |

Před každým placeným runem odhad a schválení, jako doteď. Quant výsledky (backtesty, paper) jdou do `tradebot/RESEARCH.md` (append-only, datované); do `apify/FINDINGS.md` jen to, co se týká Apify (Actory, ceny, tření), aby zdroj pro cover letter zůstal čistý. Pro cover letter: „Apify jako data layer pro quant experiment: kde dává smysl (Trends, CME, TradingView bez API) a kde ne (ceny, exekuce, Reddit s vlastním API)“.

## 9. Rozhodnutí G0 (uživatel, 2026-10-18)

1. **Trh: krypto spot.** Komodity přes ETC nejdřív po G2.
2. **Kapitál pro F4:** zatím neurčený, potřeba před G3.
3. **Apify: placený plán.** Uživatel upgraduje v konzoli, já ověřím přes `GET /v2/users/me`. Reddit a News tedy jdou přes Apify Actory podle tabulky v sekci 1; varianta „Reddit přes vlastní API“ zůstává jako záložní srovnání pro FINDINGS.
4. **HITL: nový Telegram bot** (vlastní token od BotFather, oddělený od Clown bota). Dlouhý polling z JODY, inline tlačítka Schválit/Zamítnout, callback_query. Žádná veřejná URL. Token do `.env` jako `TELEGRAM_BOT_TOKEN`, chat ID uživatele jako `TELEGRAM_CHAT_ID`; bot odpovídá jen tomuto chat ID.

### Podmínky Directora pro JODU (2026-10-18)

1. Telegram polling jako trvalá služba je OK: jeden Go proces, `Restart=on-failure`, `RestartSec=30`, `StartLimitBurst=5` za 10 min, `MemoryMax=64M`, `CPUQuota=10%`, `Nice=10`. Při chybě sítě nebo HTTP 409 exponenciální backoff, nikdy smyčka restartů.
2. Timery: `MemoryMax=256M`, `Nice=10`, `IOSchedulingClass=idle`. SQLite strop 1 GB s alarmem v reportu, surová data max 30 dní, logy jen do journalu. Záloha DB týdně mimo JODU (cíl domluvit před nasazením). JODA: 3,8 GB RAM, 2 jádra, ~16 GB volného disku.
3. Časy: ingest **07:05** (06:40 jede terra-ingest), LLM dávka **14:15** (13:00 přepínání SPARKu, StoryTeller 13:05–13:20). Souběh max 2, timeout 300 s, při 500 nebo nedostupném modelu retry 3× po 10 min, pak dávku přeskočit a uvést v reportu. Signal na LLM nečeká. LLM okno 13:00–07:00.
4. Všechny notifikace tradebotu přes nového bota, `notify.sh` ani token Clown bota se nepoužívají. Token nového bota v `~/deploy/tradebot/.env` (600), nikdy v repu (repo je veřejné, `.env` je v `.gitignore`). Callbacky jen z whitelistovaného `chat_id`.
5. Dashboard jen na 127.0.0.1, veřejně až po souhlasu uživatele.
6. Na JODU jde jen paper trading. Klíče ke Krakenu s právem obchodovat nebo vybírat tam nepatří. Přechod na reálné obchody je samostatné rozhodnutí uživatele.
7. Před nasazením dostane Director přesné unit soubory a časy.

## 10. Co musí udělat uživatel

- [ ] Upgrade Apify plánu v konzoli (Settings → Billing). Potom mi napsat, ověřím limit přes API.
- [ ] Telegram: v @BotFather `/newbot`, token přes `pbpaste` rovnou do `.env` (`TELEGRAM_BOT_TOKEN=`), nikdy do chatu. Pak botu napsat jednu zprávu, abych z `getUpdates` zjistil chat ID.
- [ ] Kraken účet existuje? Pro F1–F3 stačí veřejné API bez účtu. API klíč (trade only, bez withdraw, IP whitelist JODA) až před G3.
- [ ] Částka pro F4 před G3.
