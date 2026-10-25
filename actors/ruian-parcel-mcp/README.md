# Czech Parcels & Addresses (RÚIAN) — MCP for AI agents

Give your AI agent the Czech land and address register. This Actor answers questions about **cadastral parcels, addresses, buildings and protected areas in Czechia** from **RÚIAN**, the official register of the Czech Office for Surveying, Mapping and Cadastre (ČÚZK). It reads Czech the way Czech documents are written: declined place names ("v k. ú. Hrutově"), parcel lists ("parc. č. 123/4, 123/5 a st. 56") and ranges.

Existing RÚIAN tools import the data into PostgreSQL for developers. This one is built for **LLM agents**: codes are translated into Czech words, coordinates are WGS84, answers are compact and every answer says what to do next.

## What you can do with it

- **Read a contract or a public notice**: pass the text, get every parcel with its area, land type, building and map position.
- **Check a place before building**: is the parcel in a protected landscape area (CHKO) and which zone, in Natura 2000, in a mining area? Which municipality, ORP and region?
- **Enrich real-estate listings**: from an address to the building's use, floors, flats, built-up area, heating, water, sewer, gas and elevator.
- **GIS and EUDR**: export all parcels of a cadastral area as GeoJSON.
- **Geocode Czech addresses** without a third-party geocoder.

## MCP tools

Connect `https://<user>--ruian-parcel-mcp.apify.actor/mcp` (Streamable HTTP, header `Authorization: Bearer <APIFY_TOKEN>`).

| tool | what it does |
|---|---|
| `resolve_parcel` | k.ú. (name, declined name or code) + number → area, land type, centroid, municipality/ORP/region, building, optional protected areas and polygon |
| `extract_parcels` | Czech free text → every parcel reference with k.ú., confidence and the matching parcels |
| `resolve_address` | "Veveří 95, Brno", "Hrutov 12" → official address point, PSČ, WGS84, building data |
| `building_info` | building by parcel, address or building code → use, č.p., floors, flats, areas, completion, construction, heating, connections, elevator |
| `parcel_context` | point or parcel → k.ú., municipality, POÚ, ORP, district, region, national park / CHKO and zone, nature reserves, Natura 2000, mining areas, soil unit |
| `nearby` | addresses, buildings or parcels within 2 km, nearest first |
| `export_parcels` | all parcels of one k.ú. → GeoJSON file in the key-value store (URL in the answer) |
| `list_codebooks` | the official ČÚZK codebooks behind the Czech names |

Example: *"What is the area and land type of parcel st. 1 in k. ú. Hrutov, and is it in a CHKO?"* → one `resolve_parcel` call with `withContext: true`.

## Standby HTTP API

```
GET  https://<user>--ruian-parcel-mcp.apify.actor/parcel?ku=Hrutov&number=st.%201&withContext=true
GET  https://<user>--ruian-parcel-mcp.apify.actor/address?q=Veveří%2095,%20Brno
GET  https://<user>--ruian-parcel-mcp.apify.actor/context?lat=50.03&lon=13.87
GET  https://<user>--ruian-parcel-mcp.apify.actor/nearby?lat=49.2052&lon=16.594&radiusM=100&what=buildings
POST https://<user>--ruian-parcel-mcp.apify.actor/extract        (body: Czech text, max 50 kB)
```

Errors: `400` bad input, `503` the ČÚZK service is unavailable (retry later).

## Batch input

```json
{
  "parcels": [{ "ku": "Hrutov", "number": "st. 1" }, { "ku": "Hrutově", "number": "2" }],
  "text": "… na pozemcích parc. č. 3 a 4 v k. ú. Hrutov …",
  "addresses": ["Veveří 95, Brno", "Hrutov 12"],
  "points": [{ "lat": 50.03, "lon": 13.87 }],
  "exportKu": "Hrutov",
  "withBuildings": true,
  "withContext": true,
  "limit": 200
}
```

Each answer is one dataset item with `type` (`parcel`, `text-parcel`, `address`, `context`, `export`, `…-not-found`) and `query`.

## Output example (shortened)

```json
{
  "number": "st. 1",
  "building": true,
  "territory": { "ku": { "code": 648876, "name": "Hrutov" }, "obec": { "name": "Hrutov" }, "orp": { "name": "Jihlava" }, "kraj": { "name": "Kraj Vysočina" } },
  "areaM2": 318,
  "landType": "zastavěná plocha a nádvoří",
  "centroid": { "lat": 49.255598, "lon": 15.678901 },
  "buildings": [{ "number": "č.p. 12", "use": "rodinný dům", "floors": 1, "flats": 1, "builtAreaM2": 116 }],
  "context": { "protectedAreas": [], "miningAreas": [], "bpej": "83434" },
  "links": {
    "vdp": "https://vdp.cuzk.gov.cz/vdp/ruian/parcely/2078094710",
    "ownerLookupHint": "Vlastníka RÚIAN neobsahuje. Ručně: Nahlížení do KN → …"
  }
}
```

## No owners, on purpose

RÚIAN does not contain owners, title deeds (LV), mortgages or prices, and this Actor never tries to get them. Owner data lives in the cadastre (katastr nemovitostí), whose use and redistribution are restricted by § 53 of Act No. 256/2013 Coll. Each parcel carries a hint for a **manual** lookup in Nahlížení do katastru nemovitostí instead.

## Data source and licence

ČÚZK, RÚIAN, public ArcGIS REST service `ags.cuzk.cz/arcgis/rest/services/RUIAN/MapServer` and official codebooks from `services.cuzk.cz`. § 62(1) of Act No. 111/2009 Coll. makes RÚIAN publicly accessible and allows copying its content by any means. No login, no CAPTCHA, no scraping of web pages. Requests are capped at 4 in parallel and cached.

## Limits

- Parcels kept only in the simplified register (PK/EN parcels) are not in RÚIAN.
- Addresses need Czech diacritics.
- Many technical building attributes are empty for older buildings; empty means "ČÚZK has no value", never zero.
- The building office is not in RÚIAN; the answer names the municipalities where it usually sits.

---

**Česky:** Actor zpřístupňuje AI agentům RÚIAN: parcely, adresy, budovy a chráněná území ČR. Rozumí českým zápisům parcel ve vyhláškách a smlouvách. Vlastníky neobsahuje a nehledá.
