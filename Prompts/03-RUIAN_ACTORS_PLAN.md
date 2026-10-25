# Plán: Actor D „RÚIAN Parcel & Address MCP" a Actor B „Stavební řízení CZ" nad RÚIAN

Datum: 2026-10-25. Autor plánu: Claude (Fable 5.1) po ověření zdrojů s uživatelem.
Určeno pro implementační session (Opus). Navazuje na `00-APIFY_PLAN.md` (pravidla
sandboxu, Fáze 3) a `02-CZ_LEGAL_ACTORS_PLAN.md` (kap. 3 Actor B, kap. 5 ČÚZK a
RÚIAN). Tento dokument je samostatný: všechno potřebné pro implementaci je
tady, do 02 se odkazuje jen pro širší kontext.

---

## 0. Proč a co stavíme

**Teze (uživatel, 2026-10-25):** nad RÚIAN existují jen wrappery pro vývojáře
(importéry do PostgreSQL/MySQL: `fordfrog/ruian2pgsql`, `ctu-geoforall-lab/gdal-vfr`,
`Spoje-NET/ruian2SQL`, `CzechRepublic/ruian-api`). **Žádný není použitelný
z LLM agenta** (ověřeno 2026-10-25: GitHub 0 výsledků pro „ruian mcp" a
„cuzk mcp"; Apify Store 0 Actorů pro „ruian", „czech cadastre"). Hodnota není
v datech (jsou zdarma a veřejná), ale v tom, že agent dostane nástroj, který:

1. rozumí české právní a úřední řeči („v k.ú. Hrutově na pozemku parc. č. st. 56"),
2. překládá kódy na význam (druh pozemku 13 → „zastavěná plocha a nádvoří"),
3. odpovídá na prostorové otázky jednou větou (leží v CHKO? pod které ORP spadá?),
4. vrací kompaktní výstup do kontextu (centroid, výměra; polygon jen na vyžádání),
5. říká, co dál (odkaz na VDP/Nahlížení pro ruční dohledání vlastníka).

Stavíme dva Actory nad jedním sdíleným Go balíčkem:

| | Actor D | Actor B |
|---|---|---|
| Název ve Store | **RÚIAN Parcel & Address MCP** (`ruian-parcel-mcp`) | **Czech Building Notices** (`czech-building-notices`) |
| Co dělá | Standby + MCP server nad RÚIAN: parcela/adresa/budova/kontext území | Harvestuje úřední desky (OFN) → stavební řízení → obohacuje přes RÚIAN |
| Zdroj | ČÚZK ArcGIS REST, VFR, INSPIRE CP (vše zdarma, bez klíče) | NKOD → 573 desek ORP/krajů (OFN Úřední desky) + RÚIAN |
| Pořadí | **první** (3–4 dny) | druhý (6 dní), používá balíček z D |
| Monetizace | zdarma / PPE symbolicky; hlavně portfolio a MCP showcase | PPE za záznam; `watch_area` jako Standby |

Co **nestavíme**: nic s vlastníky nemovitostí. Důvody v `02-…` kap. 5
(§§ 53/57 katastrálního zákona, podmínky API KN). Actor D vrací k parcele jen
odkaz na VDP a doporučení ručně nahlédnout do KN.

---

## 1. Zdroje RÚIAN (všechno ověřeno 2026-10-24/25 přímo curl-em)

### 1.1 Právní základ

§ 62 odst. 1 zákona č. 111/2009 Sb. o základních registrech (znění k
2026-01-01, text stažen z e-Sbírky): registr územní identifikace „je veřejně
přístupný způsobem umožňujícím dálkový přístup. Údaje z tohoto registru se
poskytují v elektronické podobě, včetně zhotovování trvalé nebo dočasné přímé
nebo nepřímé rozmnoženiny jeho celého obsahu nebo jeho podstatné části, a to
jakýmikoli prostředky nebo jakýmkoli způsobem". Veřejné kanály RÚIAN neobsahují
osobní údaje. → Kopírování, cache i redistribuce jsou legální; žádný klíč,
žádná registrace. V README obou Actorů uvést zdroj „ČÚZK, RÚIAN" a tento §.

### 1.2 ArcGIS REST (primární on-line kanál)

Base: `https://ags.cuzk.cz/arcgis/rest/services/RUIAN/MapServer`
(`?f=json` vrátí seznam 67 vrstev). `maxRecordCount` = 1 000 000 (prakticky
bez stránkování, ale my limitujeme sami). Bez klíče, bez rate-limit dokumentace
→ **držet ≤ 4 současné požadavky a cache**; měřit latenci den 1.

Použité vrstvy a pole (bez `objectid`, `shape`, `st_*`):

| # | Vrstva | Pole |
|---|---|---|
| 5 | **Parcela** | `id` (RÚIAN id, double), `kmenovecislo`, `poddelenicisla`, `cisloparcely` (string „123/4"), `vymeraparcely` (m²), `druhpozemkukod`, `zpusobyvyuzitipozemku`, `druhcislovanikod` (1 = stavební „st.", 2 = pozemková), `katastralniuzemi` (kód k.ú.), `platiod`, `platido`, `rizeniid`, `nespravny` |
| 3 | **StavebniObjekt** | `kod`, `cisladomovni`, `identifikacniparcela` (→ Parcela.id), `typstavebnihoobjektukod`, `zpusobvyuzitikod`, `castobce`, `momc`, `isknbudovaid`, TEA: `dokonceni`, `druhkonstrukcekod`, `obestavenyprostor`, `pocetbytu`, `pocetpodlazi`, `podlahovaplocha`, `pripojenikanalizacekod`, `pripojeniplynkod`, `pripojenivodovodkod`, `vybavenivytahemkod`, `zastavenaplocha`, `zpusobvytapenikod` |
| 1 | **AdresniMisto** | `kod`, `cislodomovni`, `cisloorientacni`, `cisloorientacnipismeno`, `psc`, `stavebniobjekt` (→ StavebniObjekt.kod), `ulice`, `vo_kod`, **`adresa`** (složený text „Veveří 958/10, Litice, 32100 Plzeň") |
| 7 | **KatastralniUzemi** | `kod`, `nazev`, `obec`, `existujedigitalnimapa`, `datumvzniku` |
| 12 | **Obec** | `kod`, `nazev`, `okres`, `pou`, `statuskod`, `nutslau` |
| 14 | **ObecSRozsirenouPusobnosti** (ORP) | `kod`, `nazev`, `spravniobeckod`, `okres`, `vusc` |
| 15 / 17 | Okres / VÚSC (kraj) | `kod`, `nazev` |
| 30, 40, 44, 52, 56, 60, 62, 22, 24 | NP, **CHKO**, NPR, PR, PP, EVL, ptačí oblast, dobývací prostor, chráněné ložiskové území | `kod`, `nazev`, `popis`, `odkaz`, `plocha`, `vyhlasovacidokumentace` |
| 28 | BPEJ | kód BPEJ (bonita půdy; cena mimo RÚIAN) |

Ověřené dotazy (vzory pro implementaci):

```text
# parcela podle k.ú. + číslo (kmen 1, bez poddělení) → 2 výsledky (st. 1 a poz. 1)
GET /5/query?where=katastralniuzemi=648876 AND kmenovecislo=1 AND poddelenicisla IS NULL
    &outFields=id,cisloparcely,vymeraparcely,druhpozemkukod,zpusobyvyuzitipozemku,druhcislovanikod,platiod
    &returnGeometry=true&outSR=4326&f=json
→ {"cisloparcely":"1","vymeraparcely":318,"druhpozemkukod":13,"druhcislovanikod":1}, rings 12 bodů WGS84
→ {"cisloparcely":"1","vymeraparcely":123,"druhpozemkukod":5,"druhcislovanikod":2}, rings 5 bodů
  (returnCentroid=true centroid NEVRACÍ → spočítat lokálně z polygonu)

# bod → CHKO (point-in-polygon)
GET /40/query?geometry=13.87,50.03&geometryType=esriGeometryPoint&inSR=4326
    &spatialRel=esriSpatialRelIntersects&outFields=nazev,kod&returnGeometry=false&f=json
→ [{"nazev":"Křivoklátsko","kod":7784643}]      (15.75,49.42 → [] = mimo CHKO)

# bod → ORP
GET /14/query?geometry=15.75,49.42&geometryType=esriGeometryPoint&inSR=4326&spatialRel=esriSpatialRelIntersects
    &outFields=kod,nazev&returnGeometry=false&f=json
→ [{"kod":1503,"nazev":"Jihlava"}]

# adresa fulltext
GET /1/query?where=adresa LIKE 'Veveří 95%'&outFields=kod,adresa,psc&returnGeometry=true&outSR=4326&f=json
→ {"kod":19176651,"adresa":"Veveří 953/73, Veveří, 60200 Brno","psc":60200}, geometry {x:16.594, y:49.205}

# stavební objekt na parcele
GET /3/query?where=identifikacniparcela=<Parcela.id>&outFields=*&returnGeometry=false&f=json
```

Odkazy pro lidi (ověřeno 200): `https://vdp.cuzk.gov.cz/vdp/ruian/parcely/<id>`,
`https://vdp.cuzk.gov.cz/vdp/ruian/adresnimista/<kod>`,
`https://vdp.cuzk.gov.cz/vdp/ruian/stavebniobjekty/vyhledej?so.identifikacniParcela.id=<id>`.
Nahlížení do KN nemá konstruovatelnou URL na parcelu (šifrovaný parametr) →
odkazovat na `https://nahlizenidokn.cuzk.gov.cz/` + instrukce „k.ú. X, parcela Y".

### 1.3 VFR (výměnný formát) a CSV – pro offline číselníky

| Soubor | URL (YYYYMMDD = poslední den měsíce) | Velikost | Použití |
|---|---|---|---|
| Stát, úplná, bez geometrie | `https://vdp.cuzk.gov.cz/vymenny_format/soucasna/20260930_ST_UKSH.xml.zip` | 36 MB | **číselník k.ú. (13 074), obcí (6 254), ORP (206), POÚ, okresů, krajů** – stáhnout 1× měsíčně, parsovat streamem (`encoding/xml` Decoder.Token), uložit kompaktní JSON do KV store |
| Obec, úplná, s geometrií | `…/soucasna/20260930_OB_554782_UKSH.xml.zip` (Brno) | 120 MB | **nepoužívat on-line**; jen pokud by ArcGIS selhal |
| Obec, základní | `…/soucasna/20260930_OB_554782_UZSZ.xml.zip` | 48 MB | – |
| Adresní místa CSV | `https://vdp.cuzk.gov.cz/vymenny_format/csv/20260930_OB_ADR_csv.zip` | 63 MB (~2,9 mil. adres, 1 CSV/obec) | volitelný lokální geokodér; v0 stačí ArcGIS vrstva 1 |
| Denní změnové soubory | existují na VDP („Změnová data"), URL `…/zmeny/…` vrátila 404 → **přesnou URL zjistit den 1** přes VDP formulář `vdp/ruian/vymennyformat/vyhledej` | – | v0 nepotřebujeme |

Číselníky kódů (`druhpozemkukod`, `zpusobyvyuzitipozemku`, `zpusobvyuzitikod`
budovy, `typstavebnihoobjektukod`, `druhkonstrukcekod`, `zpusobvytapenikod`,
`pripojeni*kod`): jsou v XSD VFR a na webu ČÚZK (`cuzk.gov.cz` → RÚIAN →
číselníky). **Den 1 stáhnout, uložit do `internal/ruian/codebook/*.json`
a pokrýt testem.** Nehardcodovat z paměti. Známé hodnoty k ověření: druh
pozemku 2 orná půda, 3 chmelnice, 4 vinice, 5 zahrada, 6 ovocný sad,
7 trvalý travní porost, 10 lesní pozemek, 11 vodní plocha, 13 zastavěná plocha
a nádvoří, 14 ostatní plocha (vzorek Hrutov: st. 1 = 13, poz. 1 = 5 sedí).

### 1.4 INSPIRE CP (geometrie parcel, standardní formát)

- ATOM: `https://atom.cuzk.cz/CP/CP.xml` (17,8 MB, **13 074 entries** = 1 GML
  zip na k.ú.).
- WFS: `https://services.cuzk.cz/wfs/inspire-cp-wfs.asp?service=WFS&request=GetCapabilities`,
  typy `cp:CadastralParcel`, `cp:CadastralZoning`, `cp:CadastralBoundary`.
- Použití v0: `export_parcels(ku)` → GeoJSON celého k.ú. (pro EUDR / GIS
  use-case, viz kap. 5). ArcGIS by to zvládl také, ATOM je ale oficiální „bulk"
  cesta a nezatěžuje dotazovací službu.

---

## 2. Sdílený balíček `ruian` (Go)

### 2.1 Umístění a modul

Každý Actor má vlastní `go.mod` a Apify build má kontext jen v adresáři Actora
(`Dockerfile` dělá `COPY . .`), takže `replace ../` nefunguje na platformě.
Doporučení: **samostatný modul v repu** `ruian/` s `go.mod`
`module github.com/lioilsources/apifysandbox/ruian`, tagovaný `ruian/v0.1.0`,
`ruian/v0.2.0`… Repo je veřejné, takže `go get` přes proxy funguje. Actor D i B
ho importují normálně. Fallback, pokud by tagování zdržovalo: B volá D přes
Standby HTTP (`GET /parcel?…`) – stojí to CU navíc, proto až druhá volba.

### 2.2 Veřejné API balíčku (návrh, upravit podle potřeby)

```go
package ruian

type Client struct { /* http.Client, base URL, semaphore(4), cache */ }
func New(opts ...Option) *Client            // WithUserAgent("… (+mailto:…)"), WithCache(kv), WithConcurrency(n)

// Codebooks (embedded JSON, see 1.3)
func DruhPozemku(code int) string
func ZpusobVyuzitiPozemku(code int) string
func ZpusobVyuzitiBudovy(code int) string
// … one func per codebook, unknown → "neznámý (<code>)"

// Codelists of territorial units, loaded once from VFR ST_UKSH (or KV cache)
type Gazetteer struct { /* k.ú., obce, ORP, okresy, kraje; name index with normalisation */ }
func LoadGazetteer(ctx, store KV) (*Gazetteer, error)
func (g *Gazetteer) KU(code int) (KU, bool)
func (g *Gazetteer) FindKU(name string, obecHint string) []KU   // diacritics-insensitive, handles declension via stem matching + known suffix table
func (g *Gazetteer) ORPForObec(obecCode int) ORP

// Parcel references in free text
type ParcelRef struct { KUName, KUCode, Number string; Building bool /* "st." */; Raw string; Confidence float64 }
func ExtractParcelRefs(text string, g *Gazetteer) []ParcelRef   // handles: "parc. č.", "p. č.", "p.č.", "pozemek č.", "st. p.", "st. 56", lists "123/4, 123/5 a 124", ranges "123/1–123/3" (expand ≤ 50), k.ú. context carried forward

// Online lookups (ArcGIS)
func (c *Client) Parcel(ctx, kuCode int, number string) ([]Parcel, error)     // may return st. + poz. with same number
func (c *Client) ParcelByID(ctx, id int64) (*Parcel, error)
func (c *Client) Buildings(ctx, parcelID int64) ([]Building, error)
func (c *Client) Addresses(ctx, query string, limit int) ([]Address, error)  // LIKE on `adresa`
func (c *Client) AddressByKod(ctx, kod int64) (*Address, error)
func (c *Client) Context(ctx, lon, lat float64) (*Context, error)            // ORP, obec, k.ú., okres, kraj + protected areas + mining areas (parallel layer queries)
func (c *Client) Nearby(ctx, lon, lat, radiusM float64, what Layer, limit int) ([]Feature, error)

type Parcel struct {
    ID int64; KU KU; Number string; Building bool; AreaM2 float64
    LandType, LandTypeCode, Use, UseCode string/int
    Centroid LatLon; Geometry *GeoJSONPolygon `json:",omitempty"`  // omitted unless asked
    ValidFrom time.Time; VDPURL string
}
```

Pravidla:
- **Souřadnice** vždy WGS84 (`outSR=4326`); centroid počítat lokálně
  (ArcGIS ho pro tuhle službu nevrací). Pořadí `lon, lat` v GeoJSON,
  `lat, lon` v textovém výstupu pro lidi.
- **Cache**: parcely a adresy se mění zřídka; cache v KV store s TTL 7 dní,
  klíč `p:<ku>:<number>` / `a:<kod>` / `ctx:<lon4>:<lat4>`. Gazetteer TTL do
  konce měsíce (podle data v názvu VFR).
- **Chyby ze zdroje** (ArcGIS občas `{"error":{"code":400,"message":"Failed to execute query."}}`
  i na syntakticky dobrý dotaz – stalo se při použití CamelCase názvů polí;
  pole jsou **lowercase**): retry 2× s backoffem, pak chyba s textem ze zdroje.
- Komentáře v kódu anglicky, README a NOTES česky (pravidlo repa).

---

## 3. Actor D — RÚIAN Parcel & Address MCP

### 3.1 Vstup (batch režim) – `.actor/input_schema.json`

```json
{
  "mode": "parcels | addresses | context | export",
  "parcels": [{"ku": "Hrutov", "number": "st. 1"}, {"kuCode": 648876, "number": "123/4"}],
  "text": "volný text vyhlášky/smlouvy – Actor z něj vytáhne parcely sám",
  "addresses": ["Veveří 95, Brno"],
  "points": [{"lat": 49.42, "lon": 15.75}],
  "kuCode": 648876,                      // pro mode=export
  "withGeometry": false,
  "withBuildings": true,
  "withContext": true,                   // ORP, kraj, chráněná území
  "limit": 200
}
```

Batch je pro Store uživatele bez agenta: nahraje CSV parcel nebo adres, dostane
dataset. PPE event `result` za každý vrácený záznam (cena symbolická, návrh
$0.001; schvaluje uživatel).

### 3.2 Výstup (dataset / HTTP / MCP shodně)

```json
{
  "input": {"ku": "Hrutov", "number": "st. 1"},
  "matches": [{
    "id": 2078094710, "ku": {"code": 648876, "name": "Hrutov", "obec": "Hrutov", "orp": "Jihlava", "okres": "Jihlava", "kraj": "Kraj Vysočina"},
    "number": "st. 1", "building": true,
    "areaM2": 318, "landType": "zastavěná plocha a nádvoří", "landTypeCode": 13, "use": null,
    "centroid": {"lat": 49.42, "lon": 15.75},
    "buildings": [{"kod": 1234, "cp": "12", "use": "rodinný dům", "floors": 2, "flats": 1, "builtArea": 118, "completed": 1978, "heating": "lokální", "water": true, "sewer": false, "gas": false, "elevator": false}],
    "context": {"protectedAreas": [], "miningAreas": [], "bpej": null},
    "links": {"vdp": "https://vdp.cuzk.gov.cz/vdp/ruian/parcely/2078094710", "nahlizeni": "https://nahlizenidokn.cuzk.gov.cz/", "ownerLookupHint": "Vlastníka RÚIAN neobsahuje. Ručně: Nahlížení do KN → Parcela → k.ú. Hrutov, st. 1 (po přihlášení Identitou občana)."}
  }],
  "ambiguous": false, "source": "ČÚZK RÚIAN (ArcGIS REST), § 62 z. 111/2009 Sb.", "fetchedAt": "2026-10-25T10:00:00Z"
}
```

### 3.3 MCP nástroje (`POST /mcp`, Streamable HTTP, JSON odpovědi – stejný minimalistický server jako `actors/czech-public-contracts/mcp.go`)

| Nástroj | Vstup | Výstup | Poznámka |
|---|---|---|---|
| `resolve_parcel` | `ku` (název nebo kód), `number` („123/4", „st. 56"), `withGeometry?`, `withBuildings?`, `withContext?` | Parcel[] (viz 3.2) | Nejasný název k.ú. (více shod) → vrátit kandidáty s `obec`, ne hádat |
| `extract_parcels` | `text` (≤ 50 kB), `kuHint?` | ParcelRef[] + rozřešené Parcel[] | Jádro pro Actor B a pro agenty čtoucí smlouvy/vyhlášky |
| `resolve_address` | `query`, `limit?` (≤ 10) | Address[] {kod, text, psc, obec, lat, lon, buildingKod, parcelId} | LIKE na `adresa`; normalizovat „č.p./č.o.", diakritiku |
| `building_info` | `parcelId` nebo `addressKod` | Building[] (TEA přeložené do textu) | TerraReality use-case |
| `parcel_context` | `lat, lon` nebo `parcelId` | {ku, obec, orp, okres, kraj, stavebniUrad: ORP, protectedAreas[], miningAreas[]} | 6–10 paralelních point-in-polygon dotazů, cache |
| `nearby` | `lat, lon, radiusM` (≤ 2000), `what: parcels|buildings|addresses` , `limit` | Feature[] | `geometry=<circle as polygon>` nebo `distance=` parametr ArcGIS |
| `export_parcels` | `kuCode`, `format: geojson` | odkaz na KV store record (GeoJSON celého k.ú.) | z INSPIRE ATOM, výsledek nikdy inline (desítky MB) |
| `list_codebooks` | – | číselníky s kódy a texty | aby agent uměl formulovat filtry |

Anotace všech nástrojů: `readOnlyHint: true, openWorldHint: true`. Popisy
nástrojů anglicky i s českými příklady vstupu (agenti dostávají české texty).

HTTP zrcadlo pro ne-MCP klienty: `GET /parcel?ku=…&number=…`, `GET /address?q=…`,
`GET /context?lat=…&lon=…`, `POST /extract` (text/plain).

### 3.4 Architektura a limity

```
Standby HTTP ──► handler ──► ruian.Client (sem 4) ──► ArcGIS REST
                      │              │
                      │              └─► KV cache (parcely 7 d, gazetteer měsíc)
                      └─► Gazetteer (načten při startu z KV; chybí-li → VFR ST_UKSH 36 MB, ~20 s)
```

- `actor.json`: `usesStandbyMode: true`, `webServerMcpPath: "/mcp"`,
  `minMemoryMbytes: 256`, `maxMemoryMbytes: 1024` (parsování VFR streamem se
  vejde do 256 MB; změřit).
- Studený start Standby: gazetteer z KV (JSON ~3 MB) < 1 s; první start měsíce
  stáhne VFR → zapsat do FINDINGS, kolik to stojí CU.
- Limity požadavku: `limit ≤ 200` parcel v batchi, `text ≤ 50 kB`, geometrie
  jen na vyžádání, `nearby` radius ≤ 2 km. Odpověď MCP cílit < 20 kB bez geometrie.
- Robustnost: ArcGIS timeout 20 s, retry 2×, při výpadku vrátit 503 s textem
  „zdroj ČÚZK nedostupný" (ne prázdný výsledek).
- `User-Agent: ruian-parcel-mcp/<ver> (+https://apify.com/<user>/ruian-parcel-mcp; mailto:<e-mail od uživatele>)`.

### 3.5 Testy a akceptace

- Golden testy v `testdata/`: 30 reálných úryvků vyhlášek a smluv (anonymizované:
  bez jmen) → očekávané `ParcelRef[]`; cíl recall ≥ 0,9, precision ≥ 0,95.
- Skloňování k.ú.: tabulka 40 dvojic („Hrutově"→Hrutov, „Veveří"→Veveří,
  „Horních Počernicích"→Horní Počernice, „Starém Městě"→Staré Město…).
- Online testy (tag `//go:build online`): Hrutov st. 1 = 318 m² / kód 13;
  bod 13.87,50.03 ∈ CHKO Křivoklátsko; bod 15.75,49.42 → ORP Jihlava;
  adresa „Veveří 953/73" → kod 19176651.
- Akceptace: Claude Code připojené na `/mcp` zvládne bez nápovědy dotaz
  „Jaká je výměra a druh pozemku parcely 123/4 v k.ú. Hrutov a leží v CHKO?"
  dvěma voláními nástrojů.

### 3.6 Milníky (3–4 dny)

1. **Den 1:** balíček `ruian`: ArcGIS klient, číselníky (stažené, testované),
   gazetteer z VFR, `Parcel/Addresses/Context`. Měření latencí a CU → FINDINGS.
2. **Den 2:** `ExtractParcelRefs` + skloňování + golden testy; Standby + MCP
   (`resolve_parcel`, `extract_parcels`, `resolve_address`, `parcel_context`).
3. **Den 3:** `building_info`, `nearby`, `export_parcels` (ATOM), batch režim,
   dataset schema, README (česky + anglický úvod pro Store), PPE konfigurace.
4. **Den 4 (rezerva):** `apify push` po schválení uživatelem, test z Claude Code
   přes vzdálený MCP, zápis do FINDINGS, tag `ruian/v0.1.0`.

---

## 4. Actor B — Czech Building Notices (nad RÚIAN)

Detail zdrojů a klasifikace je v `02-…` kap. 3; tady je to, co se mění díky D.

### 4.1 Pipeline

```
NKOD SPARQL (datové sady conformsTo ofn.gov.cz/úřední-desky; 573 k 2026-10-24)
 └► harvester (denně, ETag/If-Modified-Since, diff proti KV `seen`)
     └► classifier (slovník + agenda → zahajeni | rozhodnuti | kolaudace | uzemni | odstraneni | ostatni)
         └► extractor: název + (volitelně) text PDF ──► ruian.ExtractParcelRefs ──► ruian.Parcel/Context
             └► záznam: úřad, typ, k.ú.[], parcely[] (výměra, druh, centroid), ORP, chráněná území, odkazy
                 └► dataset (PPE `result`) / Standby `watch_area` / MCP
```

### 4.2 Co RÚIAN přidává do záznamu

| Pole | Odkud | K čemu |
|---|---|---|
| `parcely[].centroid`, `areaM2`, `landType` | `resolve_parcel` | mapa, filtr „stavba na orné půdě", velikost záměru |
| `ku.code`, `obec`, `orp`, `kraj` | gazetteer | normalizace regionu i když deska píše jen „k.ú. Hrutov" |
| `stavebniUrad` | ORP → název úřadu (z NKOD metadata desky nebo tabulka ORP→úřad) | kam se obrátit |
| `protectedAreas[]`, `miningAreas[]` | `parcel_context` | „záměr v CHKO" je silný signál pro NGO, novináře, sousedy |
| `existingBuildings[]` | `building_info` | novostavba vs. přestavba; počet podlaží dnes |
| `geoBBox` záznamu | agregace centroidů | `watch_area(bbox)` a mapové výstupy |

### 4.3 MCP nástroje B (doplněné)

- `search_building_notices(region | bbox | kuCodes, types, keywords, since, until, inProtectedArea?)`
- `get_notice(id, withText)`
- `watch_area(kuCodes | orp | bbox)` → nové záznamy od posledního volání (kurzor v KV)
- `list_offices(region)` → stavební úřady a jejich desky (IRI, poslední sklizeň, kvalita)
- `notice_parcels(id)` → plné RÚIAN detaily parcel záměru (delegace na balíček)

### 4.4 GDPR (beze změny z 02 kap. 3.5, zopakováno, protože je to tvrdé pravidlo)

Neukládat jména fyzických osob (stavebníků, účastníků). Parcely a budovy bez
vazby na osobu nejsou osobní údaj. Nikdy nedoplňovat vlastníka. Respektovat
`relevantní_do`: po uplynutí vracet jen metadata a odkaz, ne text. README
uvádí účel (monitoring výstavby) a zdroj (úřední desky, RÚIAN).

### 4.5 Milníky (6 dní, po dokončení D)

1. Den 1: inventura NKOD (kolik z 573 má živou distribuci, velikosti, podíl
   s `agenda`), 30 vzorků, slovník klasifikace → NOTES.
2. Den 2–3: harvester + diff + klasifikace; golden 50 záznamů (přesnost ≥ 0,9).
3. Den 4: integrace `ruian` (parcely, kontext, budovy), dataset schema.
4. Den 5: Standby + MCP, `watch_area`, `search … inProtectedArea`.
5. Den 6: měření CU (573 desek/den), HITL ceny, README, push po schválení.

---

## 5. Use cases (konkrétně, s posloupností volání)

1. **Právník / realitní makléř čte smlouvu.** Agent dostane PDF kupní smlouvy →
   `extract_parcels(text)` → tabulka parcel s výměrou, druhem a mapou; ověří,
   že výměra ve smlouvě odpovídá RÚIAN; `parcel_context` ukáže CHKO/dobývací
   prostor (omezení stavby). Vlastníka dohledá ručně podle `ownerLookupHint`.
2. **Soused / spolek hlídá výstavbu.** Actor B `watch_area(kuCodes=[648876])`
   týdně přes Apify Schedule → nové „oznámení o zahájení řízení" s parcelami,
   velikostí a odkazem na vyhlášku; agent napíše shrnutí a lhůtu pro námitky
   (z textu vyhlášky).
3. **Developer hledá pozemky.** „Orná půda nad 5 000 m² v k.ú. X a Y mimo CHKO":
   `export_parcels(ku)` → filtr podle `landTypeCode=2 && areaM2>5000` →
   `parcel_context` na výsledky. Výstup je seznam čísel parcel, další krok je
   ruční Nahlížení. (Bez vlastníka, ale ušetří hodiny v mapě.)
4. **Investiční analýza nájmů (TerraReality).** Pro každý inzerát z Fáze 1b:
   `resolve_address(adresa)` → `building_info` → rok dokončení, počet podlaží,
   výtah, vytápění, počet bytů v domě. Doplní chybějící/lživé údaje v inzerátu
   a dá feature do odhadu nájmu. Interní integrace, viz kap. 6.
5. **Novinář / datový analytik.** „Kolik stavebních řízení za Q3 bylo v CHKO a
   kde": B `search_building_notices(since, inProtectedArea=true)` → agregace
   podle ORP a CHKO, mapa z centroidů.
6. **EUDR (nařízení EU 2023/1115 o odlesňování) – due diligence dodavatele.**
   Dřevo/zemědělské komodity vyžadují geolokaci pozemků původu. `export_parcels`
   nebo `resolve_parcel(withGeometry=true)` dá polygon parcely v GeoJSON pro
   prohlášení o náležité péči. Analogický Actor pro Rakousko ve Store existuje
   (`mcalex0902/eudr-parcel-polygons-austria`, 2 uživatelé) → poptávka malá,
   ale nulová konkurence pro ČR. **Termíny účinnosti EUDR se opakovaně
   posouvaly – před propagací ověřit aktuální stav.**
7. **Pojišťovna / banka – ověření nemovitosti.** Z adresy → budova → `zastavenaplocha`,
   `pocetpodlazi`, `druhkonstrukce`, `dokonceni` jako vstup pro ocenění a
   rizikové skóre (povodňové zóny mimo RÚIAN – možný budoucí zdroj: ČHMÚ/DIBAVOD).
8. **Obce a úřady.** `parcel_context` + `nearby(addresses)` pro doručování
   veřejnou vyhláškou: které adresy leží do 100 m od záměru.
9. **Agent „asistent stavebníka".** Kombinace: `resolve_parcel` → `parcel_context`
   (ORP = stavební úřad, CHKO = nutné stanovisko AOPK) → B `list_offices(orp)`
   (úřední deska, kontakt) → Actor C (`02-…` kap. 4) judikatura k „povolení
   záměru" v obdobných případech.

---

## 6. Integrace s jinými Actory

### 6.1 Vlastní Actory (repo ApifySandbox)

| Actor | Směr | Co se předává |
|---|---|---|
| **Czech Public Contracts Scraper** (`actors/czech-public-contracts`) | smlouva → D | Smlouvy o dílo na stavby a nájemní smlouvy obcí citují parcely: `extract_parcels(predmet + text přílohy)` → k zakázce přidat `parcely[]`, `ku`, `orp`. Umožní dotaz „které obce v ORP Jihlava uzavřely smlouvy na stavby na orné půdě". Přes sdílený Go balíček, ne přes síť. |
| **TerraReality ingest** (Fáze 1b, JODA) | inzerát → D | `resolve_address` + `building_info` v kroku ingestu (Go, offline cache). Nový sloupec `building_*` v SQLite; feature pro odhad nájmu. Nasazení na JODU jen po OK uživatele (pravidlo). |
| **Actor C – ÚOHS a soudy** (`02-…` kap. 4) | B ↔ C | Z rozhodnutí soudu o stavbě (`klicovaSlova` obsahuje „stavební řízení", „územní rozhodnutí") → `extract_parcels` → propojení judikátu s parcelou; opačně `notice_parcels` → `search_decisions(area, keywords=k.ú.)`. |
| **Actor A – e-Sbírka** (`02-…` kap. 2) | B → A | Vyhláška cituje „§ 184 zákona č. 283/2021 Sb." → `get_section` vrátí text ustanovení; agent vysvětlí lhůty a práva účastníků. |
| **Webhook monitor** (Fáze 2) | platforma | Při `ACTOR.RUN.SUCCEEDED` Actora B s novými záznamy → Telegram/e-mail souhrn (přes existující monitor). |

### 6.2 Actory třetích stran ve Store (ověřené názvy, 2026-10-25)

| Store Actor | Uživatelů | Integrace |
|---|---|---|
| `compass/crawler-google-places` (Google Maps Scraper, top-10) | 100k+ | Místo z Google Maps má adresu a souřadnice → `resolve_address`/`parcel_context` → parcela, budova (podlaží, rok), ORP, CHKO. „Obohacení Google Maps o katastrální kontext" je srozumitelný Store pitch. |
| `sian.agency/bezrealitky-property-scraper`, `solidcode/bezrealitky-ck-scraper`, Sreality scrapery | 2–17 | Inzeráty → `building_info` (stáří domu, výtah, počet bytů) – stejné jako TerraReality, ale pro cizí uživatele. Jediný CZ-specifický obohacovač v Store. |
| `regdata/czech-drazby-auction-scraper` | 4 | Dražby nemovitostí uvádějí k.ú. a parcely → `resolve_parcel(withContext)` → mapa, výměra, CHKO; řádově zvýší hodnotu dražebního záznamu. |
| `magistercrane/Firmy-cz-Crane-Scraper`, `solidcode/firmy-search-scraper` | 47–86 | Firmy → sídlo → `resolve_address` → budova/parcela; propojení na registr smluv přes IČO (náš Actor). |
| `mcalex0902/eudr-parcel-polygons-austria`, `mcalex0902/austria-cadastre-mcp` | 1–2 | Vzor i konkurence: CZ varianta přes `export_parcels` (INSPIRE). Sledovat jejich README/ceny. |
| `apify/rag-web-browser`, `apify/web-fetch` | – | Agent si stáhne vyhlášku/smlouvu → pošle text do `extract_parcels`. |

Technicky: integrace přes **Apify Actor-to-Actor** (`Actor.call` z JS Actorů,
nebo HTTP na Standby URL `https://<user>--ruian-parcel-mcp.apify.actor/parcel?…`),
nebo přes MCP v jednom agentu (Claude Code / Apify MCP server
`mcp.apify.com` umí volat více Actorů). V README D uvést „Use with" sekci s
těmito kombinacemi a ukázkovým promptem.

### 6.3 Platforma

- **Schedules** (zakládá uživatel): B denně 05:00 CET (po publikaci desek), D
  měsíčně 1. den 03:00 refresh gazetteeru (nebo lazy při prvním požadavku).
- **Webhooks** → existující monitor (Fáze 2).
- **Datasets**: B pojmenovaný dataset `building-notices` (append), D bez
  trvalého datasetu (Standby), export k.ú. do pojmenovaného KV store `ruian-exports`.

---

## 7. Pravidla práce (závazná, zkráceno z 00-APIFY_PLAN.md a CLAUDE.md)

- Komunikace s uživatelem **česky**; Go komentáře anglicky; README česky s
  anglickým úvodem pro Store.
- `APIFY_TOKEN` jen v `.env` (chmod 600), nikdy do repa, logů, FINDINGS ani do
  chatu. Nevypisovat `.env`.
- Před každým placeným během (včetně prvního `apify push` + test runu) **odhad
  ceny a souhlas uživatele (HITL)**. Schedules a placené běhy zakládá uživatel.
- Nic nenasazovat na SPARK/JODU (kontejnery, tunely, timery) bez výslovného OK.
  SPARK jen přes `sparkctl`. Nesahat na `terra-staging`.
- `apify/FINDINGS.md`: append-only, datované záznamy (`## YYYY-MM-DD — téma`),
  jen platformní zjištění (CU, ceny, chování Standby/MCP, chyby platformy).
  Doménová zjištění (RÚIAN pole, kvalita desek) → `actors/<name>/NOTES.md`.
- Repo je **veřejné**: žádné e-maily, interní IP, tokeny; kontaktní e-mail do
  User-Agent dodá uživatel a půjde přes env/Actor input, ne natvrdo do kódu.
- Commit a push jen na pokyn uživatele. Konvence commit zpráv viz git log.
- Respektovat zdroje: ≤ 4 současné požadavky na ArcGIS ČÚZK, ≤ 2 na jednu
  úřední desku, cache, identifikovatelný User-Agent.

---

## 8. Rizika

| Riziko | Dopad | Mitigace |
|---|---|---|
| ArcGIS služba ČÚZK bez SLA, bez dokumentovaného rate-limitu | výpadky Standby | cache, retry, 503 s jasnou hláškou; fallback plán: lokální index z VFR (ST + vybrané obce) v KV, zapnout jen pokud bude výpadků > 1/týden |
| Názvy k.ú. nejsou unikátní (např. „Lhota" ×N) | špatná parcela | vždy vracet kandidáty s obcí/ORP, nikdy nevybírat tiše; `kuHint` z kontextu dokumentu |
| Skloňování a překlepy v úředních textech | nižší recall extrakce | stem + suffix tabulka + Levenshtein ≤ 2 nad gazetteerem; golden testy |
| Číselníky se mění (nový stavební zákon, nové kódy využití) | špatný text kódu | číselníky jako data s datem; neznámý kód vrátit jako „neznámý (N)", ne spadnout |
| Geometrie v odpovědích zahltí kontext agenta | nepoužitelný nástroj | geometrie jen na `withGeometry`, polygon zjednodušit (Douglas-Peucker 1 m) |
| Právní dojem „katastr v MCP" | reputační | README jasně: RÚIAN ≠ katastr, žádní vlastníci, odkaz na § 62 a na 02 kap. 5 |
| Malý trh (CZ) | málo uživatelů | nečekat příjem; cílem je showcase MCP + obohacovač pro B a TerraReality |

---

## 9. Otevřené otázky pro uživatele

1. Kontaktní e-mail do `User-Agent` (přes Actor input/env, ne do kódu).
2. Cena PPE pro D (návrh $0.001/záznam, MCP volání zdarma v rámci Standby CU)
   a pro B (návrh $0.005/záznam).
3. Zda tagovat sdílený modul `ruian/v0.x` (doporučeno) nebo nechat B volat D
   přes HTTP.
4. Zda zařadit TerraReality obohacení (JODA) hned po D, nebo až po B.
