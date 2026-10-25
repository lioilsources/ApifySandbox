# Czech Building Permits & Land Notices — MCP for AI agents

Know what is being built, demolished, re-zoned or sold in any Czech region, from the official notice boards (úřední desky) of municipalities, regions and state bodies. The Actor collects every board published as open data, keeps the notices about **building and land**, finds the **cadastral areas and parcels** they mention and looks them up in **RÚIAN**, the official land register (area, land type, map position, protected areas).

Czechia has no public register of building proceedings. The law makes building offices publish them as public notices on their boards. This Actor turns those boards into one searchable feed.

## What you get

| type | what it means |
|---|---|
| `zahajeni` | start of a permit proceeding: deadline for objections, public hearing |
| `rozhodnuti` | building permit, permit of a plan (povolení záměru), change of a permit |
| `kolaudace` | occupancy approval, change of use |
| `odstraneni` | demolition |
| `stanovisko` | binding opinion, unified environmental opinion (JES) |
| `uzemni_plan` | zoning plan and its changes, building moratorium |
| `eia` | environmental impact assessment |
| `majetek` | municipality sells, leases or rents land or premises |
| `ostatni_stavebni` | other building information |

Each notice has the type, title, case number, office with its municipality/ORP/region, the territory, the k.ú. and parcel numbers found in the text (resolved in RÚIAN), links to the PDF documents and the notice page.

## Use cases

- **Neighbours and associations**: a daily schedule with `onlyNew` for your k.ú. tells you about a new proceeding while you can still object.
- **Developers and real-estate agents**: municipal land for sale (`majetek`), new permits near your project, zoning changes.
- **Builders and suppliers**: leads from new permits (`rozhodnuti`) for houses, flats, solar plants or networks in your region.
- **Journalists and NGOs**: building in protected landscape areas (`inProtectedArea`), EIA procedures, demolitions.
- **AI agents**: "What was permitted in Jihlava this month?" via MCP.

## MCP tools

Connect `https://<user>--czech-building-notices.apify.actor/mcp` with `Authorization: Bearer <APIFY_TOKEN>`.

| tool | what it does |
|---|---|
| `search_building_notices` | filter by kraj / ORP / obec / k.ú. / bbox, types, keywords, dates, protected areas |
| `watch_area` | new notices since the previous call (cursor) |
| `get_notice` | one notice with parcels resolved in RÚIAN, optionally protected areas |
| `notice_parcels` | full RÚIAN detail of the notice's parcels, buildings included |
| `list_offices` | which boards are covered for a region and their status |
| `list_notice_types` | the types above with Czech labels |

The Standby index refreshes every 3 hours (`HARVEST_INTERVAL` env, min 15 min).

## Batch input

```json
{
  "kraj": "Jihomoravský",
  "types": ["zahajeni", "rozhodnuti"],
  "keywords": ["bytový dům", "fotovoltaick"],
  "since": "7d",
  "onlyNew": true,
  "stateKey": "jmk-daily",
  "withContext": false,
  "limit": 500
}
```

`onlyNew` remembers what earlier runs returned (named key-value store `czech-building-notices-state`). Use one `stateKey` per schedule if you run several with different filters. Notices over `limit` stay "new" for the next run.

## Output example (shortened)

```json
{
  "type": "rozhodnuti",
  "typeLabel": "rozhodnutí o záměru (povolení stavby)",
  "title": "Rozhodnutí o povolení ve zrychleném řízení",
  "description": "Stavební úpravy části RD č.p.1030, Pohořelice … na pozemku parc.č.1920 v k.ú. Pohořelice nad Jihlavou",
  "posted": "2026-10-22T00:00:00Z",
  "relevantUntil": "2026-11-09T00:00:00Z",
  "caseNumber": "MUPO-149053/2026/SU/TRM",
  "office": { "name": "Město Pohořelice", "ico": "00283509", "orp": "Pohořelice", "kraj": "Jihomoravský kraj" },
  "territorySource": "parcel",
  "kus": [{ "code": 724866, "name": "Pohořelice nad Jihlavou" }],
  "parcels": [{ "number": "1920", "resolved": true, "areaM2": 386, "landType": "zastavěná plocha a nádvoří", "centroid": { "lat": 48.981258, "lon": 16.525339 } }],
  "documents": [{ "url": "https://www.pohorelice.cz/e_download.php?file=…" }],
  "url": "https://www.pohorelice.cz/mesto/uredni-deska-1/…"
}
```

## Coverage, honestly

- Source: notice boards registered in the national open-data catalogue (NKOD) under the OFN "Úřední desky" standard, about 270 boards without courts. On 2026-10-25 they held 44 000 items. About 4 200 were about building or land, 2 300 of them still within their "relevant until" date.
- About two thirds of the 206 ORP offices (which run most building offices) publish such a board. Smaller municipalities with their own building office often do not. `list_offices` shows what is covered.
- About 30 boards are broken at the publisher's side on any given day (dead link, HTML instead of JSON). They are skipped and retried on the next run.
- The k.ú. is found in 57 % of current notices, parcel numbers in 19 % (measured 2026-10-25). Many notices name the place only in the attached PDF, which the Actor does not read yet; the rest fall back to the office's territory (`territorySource: "office"`).

## Personal data

Notices name applicants and owners. The Actor **redacts names of natural persons** (birth dates, academic titles, "žadatel/stavebník/povinný …", "RD Novákovi", common first names) and drops document file names, which often contain a surname. It never adds owners to parcels (RÚIAN has none). After the notice's "relevant until" date only metadata and the link are returned. Redaction is a heuristic, not a guarantee; process the data under GDPR.

## Data sources

- Notice boards: open data of the publishing authorities, listed in NKOD (`data.gov.cz`), Act No. 106/1999 Coll. and the OFN standard.
- Office seat: ARES (`ares.gov.cz`).
- Parcels and territory: ČÚZK RÚIAN, § 62 of Act No. 111/2009 Coll.

No login, no CAPTCHA, no scraping of web pages. At most 2 parallel requests per server, conditional requests (ETag) where the server supports them.

---

**Česky:** Actor sbírá úřední desky obcí, krajů a státních úřadů publikované jako otevřená data a vybírá z nich vyhlášky o stavbách a pozemcích. Patří sem zahájení řízení, povolení, kolaudace, demolice, územní plány, EIA a prodej či pronájem obecních pozemků. Parcely a k.ú. dohledává v RÚIAN a jména fyzických osob rediguje.
