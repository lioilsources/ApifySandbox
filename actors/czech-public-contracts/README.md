# Czech Public Contracts Scraper

Search the **Czech contract registry (Registr smluv)** — every contract over 50 000 CZK signed by a Czech public body since July 2016 — by company ID (IČO), party name, subject keywords, value and date. Get clean JSON with the authority, the supplier, the value and a link to the official record.

## What you can do with it

- **Lead generation**: find companies that win public contracts in your field ("kampaň", "software", "úklid") and see who buys from them.
- **Due diligence**: list every public contract of a company by its IČO.
- **Market monitoring**: run it daily on a schedule and get new contracts in a dataset or webhook.
- **AI agents**: call it live through the Standby API or as an **MCP server**.

## Input

| field | example | notes |
|---|---|---|
| `ico` | `["00064581"]` | authority or any counterparty, leading zeros optional |
| `name` | `"agentura"` | substring of any party name |
| `keywords` | `["kampaň", "video"]` | at least one must be in the subject |
| `dateFrom`, `dateTo` | `2026-09-01` | publication date; default last 30 days |
| `minValueCzk` | `100000` | value excluding VAT |
| `onlyValid` | `true` | skip invalidated records |
| `limit` | `100` | the run stops here; you pay per contract |

## Output

```json
{
  "source": "registr_smluv",
  "contractId": "37581753",
  "url": "https://smlouvy.gov.cz/smlouva/39963349",
  "publishedAt": "2026-10-16T08:01:13+02:00",
  "signedAt": "2026-10-14",
  "subject": "Objednávka - kampaň - podzim 2026 Barvy Zoo Praha",
  "valueCzk": 207000,
  "authority": { "ico": "00064459", "name": "Zoologická zahrada hl. m. Prahy" },
  "supplier": { "ico": "25636057", "name": "ADJUST ART, spol. s r.o." },
  "parties": [{ "ico": "25636057", "name": "ADJUST ART, spol. s r.o." }],
  "attachments": ["https://smlouvy.gov.cz/smlouva/soubor/…"]
}
```

## Standby API and MCP

```
GET https://<user>--czech-public-contracts.apify.actor/search?ico=00064581&keywords=software&dateFrom=2026-10-01&limit=20
POST https://<user>--czech-public-contracts.apify.actor/mcp      (tool: search_contracts)
```

Send the Apify token as `Authorization: Bearer <token>`. Live queries cover at most 31 days and 50 results. Use a batch run for longer ranges.

## Data and privacy

The data comes from the official open-data dumps at data.smlouvy.gov.cz, published under the Act No. 340/2015 Coll. The Actor reads public dumps only. It does not log in, solve CAPTCHAs or touch the registry's web UI.

**Private persons are removed**: parties without an IČO and the approver's name never appear in the output.

## Limits

- One contract appears once even if it has several versions in the range. The first version found (newest dump first) is kept.
- Public procurement notices (Věstník veřejných zakázek, CPV codes) are planned.
- Monthly dumps take minutes to stream, so long ranges without a limit run longer.

Written in Go.
