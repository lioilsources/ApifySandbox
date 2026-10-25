# NOTES — RÚIAN Parcel & Address MCP (Actor D)

Doménová zjištění a měření. Platformní zjištění (CU, ceny, chování Standby na
Apify) patří do `apify/FINDINGS.md`. Plán: `Prompts/03-RUIAN_ACTORS_PLAN.md`.

## 2026-10-25 — implementace v0.1 (dny 1–3 plánu)

### Zdroj

- Vše jde přes ArcGIS REST `ags.cuzk.cz/arcgis/rest/services/RUIAN/MapServer`
  (verze 11.5, bez klíče). Názvy polí jsou **malými písmeny**; CamelCase vrací
  `400 Failed to execute query`. `nespravny=1` vrací `400 Unable to complete
  operation` – filtr na nesprávné prvky nepoužívat.
- Vrstvy obsahují jen platné prvky (`platido IS NOT NULL` = 0 ve vrstvách 5, 7, 12).
- `returnCentroid=true` služba ignoruje → centroid počítáme z polygonu.
- `MapServer/identify` vrátí k.ú. a všechna účelová území v bodě jedním
  dotazem; atributy jsou klíčované **českými aliasy** („Kód katastrálního území",
  „Název účelového prvku", „Subtyp účelového prvku") a hodnoty jako řetězce,
  prázdné jako `"Null"`.
- Zóna CHKO/NP je v `Subtyp účelového prvku` (číselník `CE_SUBTYP_UUP`: 12–15 =
  I.–IV. zóna CHKO, 8–11 = zóny A–D NP).
- Památkové rezervace/zóny (typy UUP 28–30) ve službě **nejsou**.

### Gazetteer

- Místo VFR (36 MB XML) stačí 6 dotazů na ArcGIS: 13 074 k.ú., 6 258 obcí,
  393 POÚ, 206 ORP, 76 okresů, 14 krajů; celé za 0,4–2 s, < 5 MB RAM.
  VFR proto v0 nepoužíváme (odchylka od plánu, kap. 1.3).
- **Názvy k.ú. jsou v ČR unikátní** (0 duplicit) → přesná shoda je konečná.
  Obce se opakují (např. Lhota), proto `FindObec` vrací seznam.
- Skloňování: stem (odříznutí jedné koncovky) + Levenshtein ≤ 1–2 + bodová
  penalizace za každou editaci. Známá chyba: „v Libni" → Libná (přesná shoda
  kmene) místo Libeň; střídání samohlásek (eň → ni) pravidla neřeší.
- „Brno-město" není k.ú. (k.ú. se jmenuje „Město Brno").

### Adresy

- Fulltext `LIKE '%…%'` na poli `adresa` trvá 2–8 s (2,9 mil. řádků).
  Strukturovaná cesta je 50–220 ms: ulice (vrstva 4, `UPPER(nazev)=UPPER(…)`
  + kód obce) → `ulice IN (…)` + číslo; obec bez ulic: část obce (vrstva 11)
  → budova `castobce IN (…) AND cisladomovni='12'` → adresní místa
  `stavebniobjekt IN (…)`. Fulltext zůstává jen jako záloha.
- Pole `adresa` je složený text („Veveří 331/95, Veveří, 60200 Brno",
  „č.p. 12, 67527 Hrutov"); vrstva adres nemá kód obce.

### Extrakce parcel z textu

- 20 golden případů (`ruian/ruian_test.go`) prochází: seznamy, rozsahy,
  „st. p. č.", kód k.ú. v závorce, dvě k.ú. v jednom textu, výměry a data se
  nechytají, hranice věty, „kat. území", „katastrálního území".
- Texty jsou zatím **syntetické** (psané podle vzoru vyhlášek). Reálné vzorky
  přinese Actor B z úředních desek; pak doplnit golden sadu na 30+ a změřit
  recall/precision proti ručnímu značení (cíl ≥ 0,9 / ≥ 0,95).
- Číslo bez „st." se podle zvyklosti KN bere jako pozemková parcela. Když
  pozemková neexistuje, vrátí se stavební se stejným číslem **s poznámkou**
  (vzorek Hrutov: „parc. č. 3" → jen st. 3 existuje).

### Číselníky

- Oficiální CSV v **Windows-1250** v zipech na `services.cuzk.cz/sestavy/cis/`
  (seznam na stránkách „Číselníky ISÚI" a „Číselníky ISKN – k nemovitosti").
  Generátor `cmd/ruian-codebooks`, 14 číselníků, 206 položek.
- Hodnoty „Nedefinováno"/„Nezjištěno" (kód 8/9 u TEA) vracíme jako `null`.
  U starších budov je většina TEA prázdná (Hrutov č.p. 12: jen podlaží, byty,
  zastavěná plocha, výtah).

### Měření (lokálně, macOS → ČÚZK, 2026-10-25 03:56)

| Nástroj | studený | z cache |
|---|---|---|
| resolve_parcel (bez budovy) | 0,10 s | 5 ms |
| resolve_parcel + context | 0,69 s | 5 ms |
| resolve_address | 0,21 s | < 1 ms |
| parcel_context | 0,60 s | < 1 ms |
| nearby (budovy, 100 m) | 0,09 s | 1 ms |
| extract_parcels (4 parcely) | 0,34 s | 5 ms |
| export_parcels Hrutov / Veveří | 0,6 s / 0,7 s (0,5 MB / 1,2 MB) | – |

Docker image 24 MB, Standby po načtení gazetteeru 17 MiB RAM → `minMemoryMbytes`
128 místo plánovaných 256. CU na Apify zatím neměřeno (vyžaduje placený běh,
HITL).

### Odchylky od plánu

- Sdílený balíček je modul `ruian/` v repu, do Actora se **vendoruje**
  (`make ruian-vendor`) místo tagování `ruian/v0.x`: Apify build vidí jen
  adresář Actora a vendor nevyžaduje push ani tag před každým buildem.
- README je anglicky s českým shrnutím (konvence prvního Actora a publikum
  Store), NOTES česky.
- PPE: žádné vlastní eventy; počítat se dá výchozí událostí za položku datasetu.
  MCP volání ve Standby jdou jen přes CU. Cenu schvaluje uživatel.

### Otevřené

- `RUIAN_CONTACT` (e-mail do User-Agent) nastavit v Actor settings → env, ne do kódu.
- Denní změnové soubory VFR: URL nezjištěna (v0 nepotřebuje).
- Ochrana proti přetížení: limit 4 souběžné požadavky na instanci; Standby
  může běžet ve více instancích → sledovat chybovost 503 po nasazení.
