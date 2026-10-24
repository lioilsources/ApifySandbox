# Plán: tři české právní Actory + proč ne katastr

Datum: 2026-10-24. Navazuje na `00-APIFY_PLAN.md` (Fáze 3) a na průzkum Store
z 2026-10-24 (žádný z níže uvedených zdrojů nemá v Apify Store ani jeden Actor;
hledáno: „e-sbirka", „sbirka zakonu", „czech law", „stavebni urad", „uredni deska",
„justice rozhodnuti", „nssoud", „nalus" → 0 relevantních výsledků).

Všechna tvrzení o zdrojích níže jsou ověřená přímo (curl/WebFetch 2026-10-24),
pokud není uvedeno „neověřeno".

---

## 0. Shrnutí a doporučené pořadí

| # | Actor | Datový zdroj | Právní/technické riziko | Pracnost | Pořadí |
|---|---|---|---|---|---|
| C | **Rozhodnutí ÚOHS a soudů** | oficiální open data ÚOHS (JSON-LD) + oficiální open data API MSp (rozhodnuti.justice.cz) | nízké | 3–4 dny | **1.** |
| A | **e-Sbírka / české zákony** | SPARQL endpoint + open data per ELI (bez klíče); REST API jen s registrací přes datovou schránku | nízké právně, střední technicky (objem, nedokumentované) | 4–6 dní | **2.** |
| B | **Stavební řízení CZ** | úřední desky ORP jako open data (OFN, 573 datových sad v NKOD) – veřejné vyhlášky stavebních úřadů | střední (heterogenita, PDF, osobní údaje stavebníků) | 5–8 dní | **3.** |
| – | Katastr / ČÚZK | – | **vysoké**, viz kap. 5 | – | nedělat (jen RÚIAN varianta) |

Důvod pořadí: C má dva čisté oficiální zdroje a dá se dokončit nejrychleji; A má
jistý zdroj (SPARQL), ale velký objem a bude chtít návrh cache; B nemá žádný
centrální registr a stojí na agregaci stovek desek.

Společné pro všechny tři: stejný Go skeleton jako `actors/czech-public-contracts`
(Standby + `/mcp`, `usesStandbyMode: true`, pay-per-event), stav v key-value
store, žádné trvalé ukládání surových dumpů, odhad ceny před každým placeným
během a HITL. Vše jsou **úřední díla** (§ 3 písm. a) autorského zákona), tedy bez
autorskoprávní překážky; omezení plynou jen z podmínek služeb a z GDPR.

---

## 1. Společný základ (platí pro A, B, C)

- **Kostra:** zkopírovat `actors/czech-public-contracts` → `actors/<name>`; sdílené
  věci (Apify klient, Standby server, MCP router, PPE účtování, rate limiter,
  retry s backoffem) vytáhnout do `pkg/actorkit` až při druhém Actoru, ne dřív.
- **Dva režimy:** batch run (input → dataset) a Standby (`GET /search…`, `/mcp`).
  MCP nástroje jsou tenké obálky nad stejnými funkcemi jako batch.
- **Cena (PPE):** `actor-start` + `result` (1 záznam v datasetu) + `document-text`
  (když vracíme plný text / extrahujeme PDF). Konkrétní čísla až po změření CU.
- **Slušné chování:** `User-Agent` s kontaktem, max 2 souběžné požadavky na jeden
  zdroj, respektovat `robots.txt` i tam, kde to není právně nutné (viz NS v kap. 4).
- **Osobní údaje:** nic navíc nad to, co zdroj sám zveřejňuje; žádné spojování
  fyzických osob napříč zdroji; u B neukládat jména stavebníků (viz 3.5).
- **Testy:** golden testy na parsování (uložené vzorky v `testdata/`), žádné živé
  volání v `go test`.
- **Dokumentace:** README podle Apify guidelines, česky + anglicky input popisky.
- **Co jde do FINDINGS:** jen věci o Apify platformě (PPE, Standby, MCP, review);
  doménová zjištění do `actors/<name>/NOTES.md`.

---

## 2. Actor A — e-Sbírka / české zákony

### 2.1 Zdroje (ověřeno)

| Kanál | Stav | Použitelnost |
|---|---|---|
| **SPARQL** `https://opendata.eselpoint.gov.cz/sparql` | funguje bez klíče, 49 112 473 trojic (COUNT 2026-10-24) | **primární** pro metadata a vyhledání předpisu/verze |
| **Open data per ELI** `https://opendata.eselpoint.gov.cz/esel-esb/eli/cz/sb/<rok>/<číslo>/<datum-znění>` | funguje bez klíče, vrací XHTML+RDFa; občanský zákoník 2026-01-01 = 5,6 MB | **primární** pro plný text znění k datu |
| **Open data per fragment** (LodView, `Accept: text/turtle`) `…/<datum-znění>/dokument/norma/cast_2/par_62/odst_1` → `obsahuje-fragment` → `esel-esb/právní-akt-fragment/<id>` s `text-fragmentu` | ověřeno 2026-10-24 na z. 111/2009 § 62: 2 požadavky na odstavec, odpověď ~1 kB turtle, bez klíče. Stránka znění bez fragmentu je jen seznam URI fragmentů (500 kB pro 111/2009), ne text. | **primární pro `get_section`** (jen požadovaný §), per-ELI XHTML až pro `get_act` celého předpisu |
| **Bulk dumpy** (JSON / JSON-LD) z `e-sbirka.gov.cz/open-data` | existují; podle Lupa.cz největší soubor ~1 GB zip / 27 GB rozbalený, formát nestreamovatelný, odkazy mimo NKOD | **nepoužívat** (velikost, náklady na CU) |
| **REST API** `https://api.e-sbirka.gov.cz` | ověřeno: bez klíče `401 NEPLATNY_API_KLIC`; registrace formulářem přes **datovou schránku MV ČR**, uvádí se účel a očekávaný objem; read-only; hlavička s access key; OpenAPI 3.0 ke stažení | **volitelné** rozšíření; klíč by musel dodat uživatel Actora jako secret input |
| Interní endpointy SPA (`/sbr-cache`, `/sbr-externi`, mCaptcha) | nedokumentované, chráněné captcha | **nepoužívat** |

Cizí nástroje: v Apify Store 0 Actorů. Mimo Apify existuje `judikatura-mcp`
(na justice.cz, ne na e-Sbírku). Komerční: zakonyprolidi.cz, ASPI, Beck-online
(nekopírovat jejich texty, máme oficiální zdroj).

### 2.2 Co Actor dělá

Vstup (input schema):
- `query` – číslo předpisu (`89/2012`), ELI, nebo název/klíčové slovo
- `asOf` – datum znění (default dnes)
- `sections` – seznam `§` (např. `["§ 2079", "§ 2080-2084"]`), prázdné = celý předpis
- `includeAmendments` – vrátit seznam novel a datum účinnosti
- `format` – `text | html | markdown`
- `limit`

Výstup (dataset, 1 záznam = 1 předpis nebo 1 ustanovení podle `sections`):
`eli, cislo, rok, nazev, zneniOd, zneniDo, ucinnostOd, paragraf, text, url`.

MCP nástroje (Standby):
- `find_act(query)` → kandidáti (ELI, název, platnost)
- `get_section(eli, section, asOf)` → text ustanovení k datu
- `get_act(eli, asOf, format)` → celé znění (účtuje se `document-text`)
- `list_versions(eli)` → znění a novely
- `diff_sections(eli, section, dateA, dateB)` → textový diff ustanovení
- `recent_changes(since)` → nově vyhlášené předpisy / nabývající účinnosti

### 2.3 Architektura

```
input ─► resolver (SPARQL: číslo/název → ELI + seznam znění)
      ─► fetcher (per-ELI RDFa z opendata.eselpoint; ETag/Last-Modified cache v KV store)
      ─► parser (RDFa → strom: část/hlava/§/odst.; zachovat ELI fragmenty)
      ─► selector (vybrat § podle inputu, as-of logika)
      ─► output (dataset / HTTP / MCP)
```

- **Cache:** klíč `eli@zneniOd`, hodnota komprimovaný parsovaný strom (ne surové
  5 MB HTML). Znění se nemění, cache lze držet dlouho; invalidace jen při nové
  verzi (SPARQL dotaz na seznam znění je levný).
- **Paměť:** OZ 5,6 MB XHTML → streamovat parserem (`golang.org/x/net/html`
  tokenizer), nenačítat do DOM celé. `minMemoryMbytes: 512`.
- **Fulltext napříč předpisy:** v0 ne (SPARQL endpoint nemá garantovaný
  fulltext index; neověřeno). V0 vyhledává jen názvy a čísla; fulltext v rámci
  jednoho předpisu dělá Actor lokálně.

### 2.4 Rizika a otevřené otázky

- SPARQL endpoint bez SLA; při výpadku Actor vrací poslední cache + varování.
- Struktura RDFa není dokumentovaná → první den věnovat zmapování (OZ, trestní
  zákoník, jedna vyhláška, jeden předpis se zrušenými ustanoveními).
- REST API registrace: podat žádost hned (datová schránka, 1 krátký formulář);
  pokud přijde klíč, přidat jako volitelný rychlejší backend. Do té doby bez něj.
- Právní: předpisy jsou úřední díla, žádná licence není potřeba; podmínky
  open dat e-Sbírky uvádějí „omezení užití" (stránka je SPA, text se nepodařilo
  strojově stáhnout) → **před publikací přečíst ručně v prohlížeči a zapsat do NOTES**.

### 2.5 Milníky a akceptace

1. Den 1: SPARQL průzkum, mapa RDFa, `find_act` + `get_section` nad OZ.
2. Den 2–3: parser + cache + as-of; golden testy (`§ 2079 OZ k 2026-01-01`
   se musí shodovat s textem na e-sbirka.gov.cz; `§ 1 zák. 283/2021` k datu
   před a po 1. 7. 2024).
3. Den 4: Standby + MCP, README, PPE události, měření CU na 20 předpisech.
4. Den 5: HITL odhad ceny, `apify push`, odeslání do review.

Akceptace: 10 náhodných ustanovení z různých předpisů = 10/10 shoda textu;
`get_section` ze Standby < 2 s při teplé cache.

---

## 3. Actor B — Stavební řízení CZ

### 3.1 Klíčové zjištění: centrální veřejný registr řízení neexistuje

- **Portál stavební správy** (`portal.stavebnisprava.gov.cz`, zákon 283/2021 Sb.)
  je SPA pro stavebníka: přihlášení Identitou občana, přehled *vlastních* záměrů
  a řízení, podávání žádostí, Evidence elektronických dokumentací (§ 271). Uživatelská
  dokumentace v1.13 (16. 12. 2025, 137 stran) nepopisuje žádný veřejný seznam
  řízení, export ani API. Evidence stavebních postupů (§ 270) není veřejně
  listovatelná.
- Veřejnost se o řízení dozvídá **veřejnou vyhláškou na úřední desce**
  (oznámení o zahájení řízení, rozhodnutí o povolení záměru, kolaudace, územní
  opatření, doručování při velkém počtu účastníků).
- **Úřední desky jako open data:** od 1. 2. 2022 mají ORP, kraje a státní orgány
  povinnost publikovat úřední desku podle **OFN Úřední desky** a registrovat ji
  v NKOD. Dotaz na `data.gov.cz/sparql` 2026-10-24: **573 datových sad**
  `conformsTo ofn.gov.cz/úřední-desky`. Obecné stavební úřady sídlí převážně
  právě na ORP → pokrytí většiny stavebních úřadů jedním formátem.
- OFN záznam má pole: `název, vyvěšení, relevantní_do, číslo_jednací,
  spisová_značka, agenda, dokument[].url, umístění, subjekt` (všechna nepovinná).
- Záložní zdroj: **edesky.cz API** (`/api/v1/documents?keywords=…`, XML,
  `api_key` po registraci, „testovací provoz", fulltext přes `search_with=es`,
  VOP třetí strany). Použít jen jako doplněk pro obce mimo NKOD, ne jako základ.

### 3.2 Co Actor dělá

Vstup:
- `region` – kraj / ORP / IČO úřadu / seznam kódů k.ú.
- `types` – `zahajeni | rozhodnuti | kolaudace | uzemni | ostatni`
- `keywords` – volitelně (např. „fotovoltaick", „bytový dům")
- `since`, `until`
- `extractText` – stáhnout PDF a vytáhnout text (účtováno zvlášť)
- `limit`

Výstup (1 záznam = 1 vyhláška/informace):
`id, urad (název, IČO, ORP, kraj), nazev, typ (klasifikace), cisloJednaci,
spisovaZnacka, vyveseno, relevantniDo, katastralniUzemi[], parcely[] (z regexu
„parc. č."), stavbaPopis (krátký), dokumentUrl[], zdrojDeskaIri, textUryvek`.

MCP:
- `search_building_notices(region, types, keywords, since)`
- `get_notice(id, withText)`
- `watch_area(ku_codes | orp)` → vrací změny od posledního volání (stav v KV)
- `list_offices(region)` → stavební úřady a jejich desky

### 3.3 Architektura

```
NKOD SPARQL (seznam datových sad OFN úřední desky, distribuce JSON-LD)
   └► harvester: denně stáhnout každou desku (ETag), diff proti KV stavu
        └► classifier: pravidla (regex na název/agendu) → typ; nejistota → "ostatni"
             └► extractor (volitelně): PDF → text (pdftotext-like Go lib), regex k.ú./parcely
                  └► dataset / Standby / MCP
```

- Stav: `desky.json` (iri → ETag, poslední vyvěšení) + `seen` (iri informace →
  hash), cíl < 50 MB; surové JSON-LD se neukládají.
- Klasifikace v0 **bez LLM**: slovník (`stavební povolení`, `společné povolení`,
  `povolení záměru`, `oznámení o zahájení`, `kolaudační`, `územní rozhodnutí`,
  `dodatečné povolení`, `odstranění stavby`, `veřejná vyhláška` + odbor výstavby
  v `agenda`). LLM až v1 jako volitelný input s vlastním klíčem uživatele.
- Výkon: 573 desek × denně; velikost JSON-LD 10 kB–50 MB (neověřeno
  plošně, změřit první den na 30 vzorcích). Odhad CU doplnit po měření.

### 3.4 Rizika

- **Heterogenita:** část datových sad je v NKOD označena jako zastaralá;
  některé obce dávají jen název a PDF. Metrika kvality per deska do výstupu
  (`coverage: full|partial`).
- **PDF:** skenované vyhlášky bez textové vrstvy → OCR mimo rozsah v0 (vrátit jen
  metadata + odkaz).
- **Obce I. a II. typu** se stavebním úřadem mimo NKOD → mezery v pokrytí; řešit
  edesky.cz fallbackem až v1.
- **Nový stavební zákon**: terminologie se změnila (povolení záměru místo
  stavebního/územního); slovník musí mít oba světy.

### 3.5 Osobní údaje (GDPR)

Veřejné vyhlášky obsahují jména stavebníků – fyzických osob. Actor:
- neextrahuje a neukládá jména fyzických osob (jen název úřadu, čísla parcel,
  k.ú., popis stavby, odkaz na originál);
- čísla parcel bez vazby na vlastníka nejsou osobní údaj; vazbu na vlastníka
  Actor nikdy nedoplňuje (viz kap. 5);
- v README uvést účel (monitoring výstavby) a to, že data pocházejí z úředních
  desek s omezenou dobou vyvěšení; respektovat `relevantní_do` (po uplynutí
  nevracet plný text, jen metadata).

### 3.6 Milníky

1. Den 1: NKOD inventura (573 sad → kolik má platnou distribuci, velikosti,
   kolik má `agenda`), 30 vzorků, slovník klasifikace.
2. Den 2–3: harvester + diff + klasifikace; golden testy na 50 ručně
   označených záznamech (cíl přesnost ≥ 0,9 pro `zahajeni|rozhodnuti`).
3. Den 4–5: extraktor parcel/k.ú., Standby + MCP, `watch_area`.
4. Den 6: měření CU, HITL, README, push.

---

## 4. Actor C — Rozhodnutí ÚOHS a soudů

### 4.1 Zdroje (ověřeno)

| Zdroj | Forma | Zjištění |
|---|---|---|
| **ÚOHS – sbírky rozhodnutí** | oficiální open data `https://uohs.gov.cz/opendata/rozhodnuti.jsonld` (16,8 MB) + číselníky (`instance`, `odbory`, `typy-rizeni`, `typy-rozhodnuti`) | **přesně 10 000 záznamů** (nejnovější, zpět do 2015) → open data nejsou úplná; HTML výpis má jen pro VZ 16 641 rozhodnutí / 1 665 stran, server-rendered, `robots.txt` povoluje. Pole: `věc, číslo_jednací, spisová_značka, instance, odbor, datum_právní_moci, účastník[], typ_řízení, typ_rozhodnutí, dokument[].url` (PDF, textová vrstva, ~100 kB). Pět oblastí: hospodářská soutěž, veřejné zakázky, veřejná podpora, významná tržní síla, přístup k dopravní infrastruktuře. Zveřejňují se jen **pravomocná** rozhodnutí. `uredni-deska.jsonld` 2026-10-24 nedostupná (HTTP 000). |
| **Okresní, krajské a vrchní soudy** | oficiální open data API MSp `https://rozhodnuti.justice.cz/api/opendata/<rok>/<měsíc>/<den>?page=N` (100/str.), detail `…/api/finaldoc/<uuid>` (JSON ~72 kB) | anonymizováno u zdroje; měsíčně 5–8 tisíc rozhodnutí (2026); pole listingu: `jednaciCislo, soud, autor, ecli, predmetRizeni, datumVydani, datumZverejneni, klicovaSlova[], zminenaUstanoveni[]`; detail: `header, verdictText, justificationText, information, metadata`. Jen procházení podle data zveřejnění, vyhledávání podle sp. zn. jen přes web. |
| **NSS** `vyhledavac.nssoud.cz` | ASP.NET aplikace se stavem v session (`/Home/SetCondition`, `/Home/MyResTRowsCont`, `/Home/Export`), bez API, bez `robots.txt` | scrapovatelné, ale křehké; má export výsledků (formát neověřen). **Fáze 2** po spike. |
| **NS** (Nejvyšší soud, Brno – vrchol obecné justice pro civil a trestní dovolání; nezaměňovat s NSS = Nejvyšší správní soud a ÚS = Ústavní soud) `rozhodnuti.nsoud.cz` | `robots.txt`: `Disallow: /` pro všechny krom `DG_JUSTICE_CRAWLER`; sitemapy odkazují na starý Lotus Notes `nsoud.cz/Judikatura/judikatura_ns.nsf`; **v open datech MSp (`rozhodnuti.justice.cz`) NS není** – ověřeno na vzorku dne: jen okresní, krajské a vrchní soudy | **mimo rozsah v0** – respektovat robots. Jediná cesta k judikatuře NS je požádat NS o souhlas se strojovým přístupem nebo o export (NS má vlastní databázi, žádné otevřené API). Zda žádost poslat, rozhoduje uživatel; návrh dopisu připravím na vyžádání. Do té doby Actor C pokrývá ÚOHS + obecné soudy I. a II. stupně + vrchní soudy. |
| **ÚS – NALUS** | jen webové rozhraní, od 2/2026 AI chatbot nad NALUS, API nenalezeno | mimo rozsah v0. |

### 4.2 Co Actor dělá

Vstup:
- `source` – `uohs | justice | all` (v1: `nss`)
- `query` – fulltext ve věci/klíčových slovech/`zminenaUstanoveni` (např. `§ 257 134/2016`)
- `party` – účastník (ÚOHS) / `court` (justice)
- `area` – oblast ÚOHS; `instance`; `outcome` (typ rozhodnutí z číselníku)
- `from`, `to` – datum právní moci / zveřejnění
- `withText` – stáhnout plný text (ÚOHS: PDF → text; justice: `finaldoc`)
- `limit`

Výstup: `id, source, cisloJednaci, spisovaZnacka, ecli, soudNeboOdbor, instance,
vec, ucastnici[], typRizeni, typRozhodnuti, datumVydani, datumPravniMoci,
klicovaSlova[], ustanoveni[], url, dokumentUrl, text (volitelně)`.

MCP:
- `search_decisions(source, query, filters, from, to)`
- `get_decision(id, withText)`
- `decisions_citing(provision)` → rozhodnutí odkazující na ustanovení (justice má
  `zminenaUstanoveni`, u ÚOHS regex v PDF textu)
- `watch(query)` → nové od posledního volání (stav v KV)

Tohle je přirozený doplněk Actora A: `get_section("§ 257 ZZVZ")` →
`decisions_citing("§ 257 134/2016")`.

### 4.3 Architektura

```
ÚOHS: denně rozhodnuti.jsonld (ETag) → index v KV (10 000 nejnovějších)
      starší než index: on-demand průchod HTML výpisu (stránkování, 2 req/s)
      PDF → text jen při withText (účtovat document-text)
justice: denní inkrement /api/opendata/Y/M/D → index listingů (bez textu)
         text přes finaldoc jen při withText
index: SQLite nebo gob v KV store, cíl < 200 MB; fulltext nad listingy lokálně
```

- Index nad justice listingy roste ~80 tisíc záznamů/rok × ~1 kB → OK v KV
  store po kompresi; plné texty neukládat.
- ÚOHS PDF: textová vrstva, Go extrakce (`pdfcpu`/`ledongthuc/pdf`); fallback
  vrátit jen metadata.

### 4.4 Rizika

- ÚOHS strop 10 000 v open datech: pro starší rozhodnutí nutný HTML průchod;
  jednorázová indexace 1 665 stran = ~1 665 požadavků, pak jen diff.
- justice API nemá uvedenou licenci ani rate limit → držet 2 req/s, uvést
  zdroj v každém záznamu.
- ÚOHS rozhodnutí obsahují názvy právnických osob a výjimečně fyzické osoby
  (podnikatele) – ponechat tak, jak zveřejnil úřad, nic nedoplňovat.
- NSS/NS: v0 jasně v README označit jako nepokryté.

### 4.5 Milníky

1. Den 1: ÚOHS JSON-LD + číselníky → index; justice inkrement za poslední měsíc.
2. Den 2: vyhledávání a filtry, PDF/finaldoc text, golden testy (3 ÚOHS, 3 justice).
3. Den 3: Standby + MCP, `watch`, měření CU, HITL, README.
4. Den 4: push, review; spike NSS exportu (max 2 h) → rozhodnutí o fázi 2.

---

## 5. Katastr / ČÚZK – proč na to Actor nestavět

### 5.1 Právní rámec (zákon č. 256/2013 Sb., katastrální zákon)

- **§ 52** – katastr je veřejný, každý má právo nahlížet a pořizovat si opisy pro
  svou potřebu. Ale: přehled vlastnictví z území ČR, sbírka listin a dosažené
  ceny jen po **prokázání totožnosti**, úřad o tom vede evidenci (§ 52 odst. 3–5).
- **§ 53** – „Údaje katastru lze užít jen k účelům uvedeným v § 1 odst. 2."
  a „Šířit údaje katastru lze pouze se souhlasem Českého úřadu zeměměřického a
  katastrálního." Účely § 1 odst. 2 jsou ochrana práv k nemovitostem, daně,
  ochrana ŽP, územní plánování atd. – ne obecný komerční datový produkt.
- **§ 57** – porušení je přestupek, pokuta až 50 000 Kč (FO) / 100 000 Kč (PO,
  podnikající FO).
- **§ 55 odst. 3** – dálkový přístup k elektronickým údajům je **za úplatu**
  podle vyhlášky č. 358/2013 Sb.

Actor ve Store, který by **šířil** údaje katastru třetím osobám, naráží přímo na
§ 53 (šíření jen se souhlasem ČÚZK) bez ohledu na to, odkud data vezme.

### 5.2 Technické kanály a jejich podmínky (ověřeno 2026-10-24)

| Kanál | Cena | Podmínky | Použitelnost pro Actor |
|---|---|---|---|
| **Nahlížení do KN** (`nahlizenidokn.cuzk.gov.cz`) | zdarma | určeno jen pro interaktivní užití; automatizované získávání dat zakázáno; anti-bot detekce podle IP; údaje o vlastníkovi jen po přihlášení Identitou občana (stav se v čase měnil) | **ne** – porušení podmínek, captcha, přihlášení |
| **REST API KN** (`api-kn.cuzk.gov.cz`, od 1. 7. 2024, § 14a vyhl. 358/2013) | zdarma | ApiKey po registraci (FO Identita občana, PO účet DP); „**API neposkytuje osobní údaje**" (tedy žádní vlastníci); „slouží pro napojení externích systémů … **neslouží pro vytěžování a hromadné stahování dat**"; limit volání za období (`/AplikacniSluzby/StavUctu`); klíč „**nesmí předávat jiné osobě nebo službě**" a nesmí být na zařízení klienta; bez SLA; porušení = přestupek § 57 až 100 000 Kč | **ne pro veřejný Actor**: klíč nelze legálně vložit do Actora ani převzít od uživatele (předání „službě"); navíc bez vlastníků odpadá hlavní užitná hodnota |
| **DP / WSDP** (dálkový přístup, webové služby) | placené; vyhl. 358/2013 příl. 4: výpis z KN (LV) 100 Kč, informace o parcele/stavbě/jednotce 10 Kč | účet jen pro PO nebo podnikající FO; ověřené výstupy s pečetí (XML/PDF); účel užití dle § 1 odst. 2; evidence poskytnutí | technicky ano, ale drahé na jednotku, účelově omezené a šíření opět jen se souhlasem ČÚZK |
| **RÚIAN / VFR, INSPIRE CP (WFS)** | zdarma, otevřená data | adresní místa, parcely (geometrie, výměra, druh pozemku), budovy, k.ú., obce – **bez vlastníků** | **ano** – jediná bezproblémová varianta |

### 5.3 Závěr

- „Kdo vlastní parcelu X" jako Apify Actor **nedělat**: §§ 53/57 KatZ, podmínky
  API (zákaz předání klíče službě, zákaz vytěžování, žádné osobní údaje) a
  podmínky Nahlížení to společně vylučují. Komerční řešení v ČR existují jen s
  individuálním souhlasem/smlouvou s ČÚZK.
- Co jde: **RÚIAN Actor** – parcela/adresa → k.ú., výměra, druh pozemku, geometrie,
  obec, ORP, stavební úřad. To je přesně doplněk pro Actor B (`parcely[]` →
  normalizace a geolokace) a nevyžaduje nic od ČÚZK. Rozsah 2 dny. Zařadit jako
  volitelný modul B, ne jako samostatný produkt.

### 5.4 RÚIAN podrobně (ověřeno 2026-10-24)

**Co to je.** Registr územní identifikace, adres a nemovitostí – jeden ze čtyř
základních registrů podle zákona č. 111/2009 Sb. (vedle ROB – obyvatelé, ROS –
osoby, RPP – práva a povinnosti). Správcem je ČÚZK. Na rozdíl od katastru
nemovitostí jsou jeho údaje **veřejné bez omezení účelu**. § 62 odst. 1 z.
111/2009 Sb. (znění k 2026-01-01, ověřeno z e-Sbírky): „Registr územní
identifikace je veřejně přístupný způsobem umožňujícím dálkový přístup. Údaje z
tohoto registru se poskytují v elektronické podobě, **včetně zhotovování trvalé
nebo dočasné přímé nebo nepřímé rozmnoženiny jeho celého obsahu nebo jeho
podstatné části, a to jakýmikoli prostředky nebo jakýmkoli způsobem**; na
nosičích dat se poskytují za úplatu…" – tedy zákon výslovně dovoluje kopírovat
celou databázi; za úplatu je jen fyzický nosič, stažení je zdarma. Veřejné kanály
(VDP, VFR, ArcGIS, INSPIRE) **neobsahují osobní údaje** – žádné vlastníky, LV ani
ceny. Odst. 2 sice říká, že RÚIAN „zprostředkovává z katastru údaje o
vlastníkovi", ale to je služba základních registrů pro orgány veřejné moci přes
ISZR, ne součást otevřených výstupů. Na RÚIAN tak nedopadají §§ 53/57
katastrálního zákona ani podmínky API KN.

**Co obsahuje (prvky a klíčové atributy).**

| Prvek | Atributy použitelné v Actorech | Poznámka |
|---|---|---|
| Stát → kraj (VÚSC) → okres → ORP → POÚ → obec → část obce → MOMC | kód, název, nadřazený prvek, geometrie (hranice) | hierarchie pro `region` filtr Actora B a pro určení příslušného stavebního úřadu (ORP) |
| Katastrální území (k.ú.) | kód (6 číslic), název, obec, geometrie | klíč pro normalizaci „k.ú. Hrutov" → `648876` |
| Parcela | `id`, `kmenovecislo`, `poddelenicisla`, `cisloparcely`, `vymeraparcely` (m²), `druhpozemkukod` (orná, zahrada, zastavěná plocha…), `zpusobyvyuzitipozemku`, `druhcislovanikod` (stavební/pozemková), `katastralniuzemi`, `platiod/platido`, polygon | **bez vlastníka a bez LV** |
| Stavební objekt | `kod`, `cisladomovni` (č.p./č.ev.), `identifikacniparcela`, `typstavebnihoobjektukod`, `zpusobvyuzitikod` (RD, bytový dům, garáž…), TEA: `dokonceni`, `pocetpodlazi`, `pocetbytu`, `zastavenaplocha`, `obestavenyprostor`, `podlahovaplocha`, `druhkonstrukcekod`, `zpusobvytapenikod`, připojení voda/kanalizace/plyn, `vybavenivytahemkod`, `isknbudovaid` | TEA = technicko-ekonomické atributy; hodnota pro realitní a stavební use-casy (TerraReality!) |
| Adresní místo | ulice, č.p./č.o., PSČ, obec, část obce, souřadnice S-JTSK (+ WGS84 přepočet) | geokódování adres z vyhlášek |
| Ulice, ZSJ, volební okrsek | kód, název, geometrie | – |
| Účelové prvky (od 2023+) | chráněná území, NP/CHKO, dobývací prostory, BPEJ, body bodového pole | zajímavé pro B: „záměr leží v CHKO" |

**Kanály přístupu (všechny bez klíče, všechny dnes odpověděly 200).**

| Kanál | URL | Formát / velikost | Hodí se na |
|---|---|---|---|
| **VFR – výměnný formát RÚIAN** (VDP) | `https://vdp.cuzk.gov.cz/vymenny_format/soucasna/YYYYMMDD_ST_UKSH.xml.zip` (stát bez geometrie, 36 MB), `…/YYYYMMDD_OB_<kód obce>_UKSH.xml.zip` (obec vč. parcel a geometrie; Brno 554782 = 120 MB), varianta `UZSZ` = základní, 48 MB; denní změnové soubory `…/zmeny/` | GML/XML podle XSD ČÚZK, měsíční úplná dávka (k poslednímu dni měsíce) + denní přírůstky | offline index k.ú./obcí/ORP (jednou za měsíc do KV store), bulk zpracování |
| **CSV adresních míst** | `https://vdp.cuzk.gov.cz/vymenny_format/csv/YYYYMMDD_OB_ADR_csv.zip` (63 MB, ~2,9 mil. adres) | CSV, 1 soubor na obec | geokódování adres bez cizí služby |
| **ArcGIS REST** | `https://ags.cuzk.cz/arcgis/rest/services/RUIAN/MapServer/<vrstva>/query` – vrstvy 5 Parcela, 3 StavebniObjekt, 1 AdresniMisto, 7 KatastralniUzemi, 12 Obec, 14 ORP, 40 CHKO… | JSON/GeoJSON, `where=katastralniuzemi=648876 AND kmenovecislo=1`, `maxRecordCount` 1 000 000 | **on-line dotaz z Actora** (parcela → výměra/druh/geometrie za 1 request, ověřeno: k.ú. Hrutov p. č. 1 → 318 m² a 123 m²) |
| **INSPIRE CP** | ATOM `https://atom.cuzk.cz/CP/CP.xml` (GML na k.ú., ~13 000 položek), WFS `https://services.cuzk.cz/wfs/inspire-cp-wfs.asp` (`cp:CadastralParcel`, `cp:CadastralZoning`, `cp:CadastralBoundary`) | GML 3.2 (INSPIRE schéma) | mezinárodně standardní geometrie parcel; WFS pro prostorové dotazy (bbox) |
| **VDP web** `vdp.cuzk.gov.cz/vdp/ruian/…` | interaktivní vyhledávání | HTML | jen ruční kontrola, Actor nepoužívá |

**Co z toho použít v Actoru B (modul `ruian`).**

1. Při startu Standby: načíst (nebo z KV store obnovit) číselník k.ú. + obcí + ORP
   z `ST_UKSH` (36 MB, parsovat streamem, uložit ~15 000 řádků k.ú. a ~6 250
   obcí jako JSON do KV store, obnovovat 1× měsíčně podle data v názvu souboru).
2. Pro každou vyhlášku: regex vytáhne `k.ú. <název>` a `p. č. 123/4, st. 56`;
   název k.ú. normalizovat podle číselníku (fuzzy na diakritiku a skloňování:
   „v k.ú. Hrutově" → Hrutov).
3. Pro každou parcelu 1 dotaz na ArcGIS vrstvu 5 → `vymera`, `druhPozemku`,
   `geometrie` (centroid WGS84). Pro `st.` parcely navíc vrstva 3 (stavební
   objekt na parcele → č.p., způsob využití, počet podlaží) přes
   `identifikacniparcela`.
4. Výstup: `parcely[] = {kod_ku, nazev_ku, cislo, vymera_m2, druh_pozemku,
   lat, lon, stavebni_objekt?}` + `obec`, `orp`, `stavebni_urad` (= ORP, pro
   obecné stavby) a `chranene_uzemi[]` (prostorový dotaz na vrstvy 30/40/60).
5. MCP nástroj `resolve_parcel(ku, cislo)` a `geocode_address(text)` zdarma pro
   všechny tři Actory (sdílený balíček `internal/ruian`).

Co RÚIAN **nedá**: vlastníka, LV, věcná břemena, zástavy, ceny, řízení V/Z
(plomby), BPEJ cenu půdy. Kdo to chce, musí ručně do Nahlížení nebo přes DP s
vlastním účtem – mimo Actor.

---

## 6. Harmonogram, náklady, rozhodnutí uživatele

| Týden | Práce |
|---|---|
| 1 | Actor C (4 dny) + podání žádosti o REST API e-Sbírky (den 1, 30 min) |
| 2 | Actor A (5 dní) |
| 3 | Actor B (6 dní) + RÚIAN modul, pokud zbude čas |

Náklady Apify: všechny tři zdroje jsou bezplatné; platí se jen CU za běhy a
Standby. Odhady CU doplnit po měření v den 1 každého Actora (zapsat do
FINDINGS, protože je to platformní zjištění). Před každým placeným během HITL.

Co potřebuji od uživatele (stav 2026-10-24):
1. Souhlas s pořadím C → A → B — **schváleno 2026-10-24**.
2. Podání žádosti o REST API e-Sbírky datovou schránkou (jen uživatel má DS) —
   **uživatel podá** (2026-10-24). Pro Actor A to není blokující, v0 jede na
   SPARQL + open data.
3. Rozhodnutí, zda oslovit NS (Nejvyšší soud) kvůli strojovému přístupu k jeho
   judikatuře — **otevřené**; bez něj Actor C zůstává bez NS a NSS, což README
   přizná.
4. Kontaktní e-mail do `User-Agent` všech tří Actorů — **otevřené**.

Výsledky měření a doménová zjištění: `actors/<name>/NOTES.md`; platformní
zjištění: `apify/FINDINGS.md` (datovaně, append-only).
