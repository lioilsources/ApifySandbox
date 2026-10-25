# NOTES — Czech Building Notices (Actor B)

Doménová zjištění a měření. Platformní zjištění patří do `apify/FINDINGS.md`.
Plán: `Prompts/03-RUIAN_ACTORS_PLAN.md` kap. 4 a `Prompts/02-CZ_LEGAL_ACTORS_PLAN.md` kap. 3.

## 2026-10-25 — inventura desek a implementace v0.1

### NKOD inventura (den 1 plánu)

- SPARQL `data.gov.cz/sparql`, distribuce s `conformsTo` OFN úřední desky a
  `downloadURL`: **477 datových sad** (ne 573 z plánu – to číslo počítalo i
  sady bez distribuce/jazyka cs). Z toho **208 jsou soudy a státní
  zastupitelství** (`kiosek.justice.cz`, jeden vydavatel MSp) → vynechány,
  stavební vyhlášky nepublikují.
- Zbývá **266 desek** obcí, městských částí, krajů a státních úřadů.
  Stahovatelných 236–242 (podle dne); ~30 nefunkčních u vydavatele: 15–16× HTTP 404,
  5× HTML místo JSON, 4× rozbitý JSON, 1× expirovaný certifikát, 1× TLS handshake.
- Pokrytí ORP: podle shody názvů a hostitelů **~130–140 z 206 ORP** (cca dvě
  třetiny). Chybí mj. Liberec, Zlín, České Budějovice, Karlovy Vary, Kolín,
  Prostějov, Trutnov, Uherské Hradiště. Heuristika je hrubá (řada desek má v
  názvu jen „Úřední deska").
- Objem: 61–62 MB za plnou sklizeň, 40–44 tis. záznamů, medián desky 85 kB,
  max MHMP 10,6 MB. Plná sklizeň 25–35 s (8 vláken, max 2 požadavky na server).
- ETag/Last-Modified podporuje jen **38 desek** – podmíněné stahování ušetří málo.

### Struktura OFN v praxi

- `název`, `popis` jako `{"cs": …}` nebo řetězec; `agenda` jako seznam objektů,
  objekt nebo řetězec; `vyvěšení`/`relevantní_do` s `datum`, `datum_a_čas` nebo
  `nespecifikovaný` (4 600× „nespecifikováno" u `relevantní_do`).
- Některé soubory mají nahoře pole místo objektu, BOM, HTML entity v názvech
  (`&quot;`, `&#8211;`) nebo syrové řídicí znaky (`\r`, `\v`) uvnitř řetězců –
  parser to vše toleruje.
- IČO vydavatele je spolehlivě na konci IRI vydavatele v NKOD; pole
  `provozovatel` má `ičo` nebo `identifikátor_ovm`.
- Území úřadu: ARES `ekonomicke-subjekty/{ičo}` → `sidlo.kodObce` → gazetteer.
  **Praha nemá v RÚIAN okres** → kraj se bere přes ORP (oprava v `ruian`).

### Klasifikace (pravidla, bez LLM)

- Z 44 tis. záznamů ~4 200 stavebních/pozemkových, z toho 2 300 v době
  relevance (25. 10.). Rozložení aktuálních: majetek 614, rozhodnutí 461,
  stanovisko 414, ostatní 400, ÚP 183, zahájení 150, EIA 47, odstranění 35,
  kolaudace 7.
- Ruční kontrola náhodných vzorků (10–15 na typ): EIA, majetek, odstranění,
  rozhodnutí, stanovisko 10/10; kolaudace, ÚP, zahájení ~9/10;
  `ostatni_stavebni` ~12/15 po vyřazení dopravního značení (PDZ, DZ, PÚP),
  doručování, odpovědí podle zákona 106/1999 a inzerátů na pozice.
- Kroměříž publikuje velké množství JES (jednotné environmentální stanovisko)
  → typ `stanovisko`.
- Karlovy Vary píší jen identifikátory z národního systému stavební správy
  („Záměr: Z/2026/150902 Řízení: R/2026/191294") → `ostatni_stavebni`.
  Tyto identifikátory by šly v budoucnu párovat s Portálem stavební správy.
- Golden testy: 50+ reálných názvů v `internal/classify/classify_test.go`.

### Lokalizace

- k.ú. nalezeno u **57 %** aktuálních vyhlášek, čísla parcel u **19 %**,
  parcely s k.ú. u 16 % (měřeno na 2 311 vyhláškách).
- Brno a Praha píší čtvrť bez „k. ú." („Pekařská, Staré Brno", „Jehnice")
  → `MatchKUNames` nad k.ú. obce úřadu (u ORP sídla nad celým ORP).
- Nápověda pro parcely bez k.ú. **jen podle kódu**: obec Pohořelice leží v
  k.ú. „Pohořelice nad Jihlavou", zatímco k.ú. „Pohořelice" je jinde → nápověda
  podle jména obce by byla chybná.
- Zalomení řádku uvnitř názvu („Pohořelice nad⏎Jihlavou") řeší `ruian`.
- Kolize bez diakritiky: k.ú. „Tašovice" (Karlovy Vary) × „Tasovice" (Znojmo).

### TLS

- **Neúplný řetězec certifikátů** (server neposílá mezilehlý certifikát):
  edeska.brno.cz (3 desky, největší brněnská), eud.unmz.cz, portal.rymarov.cz,
  ud.mestolysa.cz. macOS si mezilehlý certifikát dohledá, Go na Linuxu ne →
  v kontejneru selhávaly. Řešení `internal/aia`: dohledání přes AIA URL a
  běžné ověření vůči systémovým kořenům a jménu hostitele (test v kontejneru
  `golang:1.25-alpine`: běžný klient selže, AIA klient projde, wrong-host,
  self-signed a expired odmítne).

### GDPR

- Jména v názvech: „pro: PETR KOVÁŘ, nar. …", „MUDr. …", „Povinný: …",
  „Novostavba RD Kopeckovi", „Výzva k odstranění vozidla … Miroslav Dvořák".
  Redakce `classify.Redact` (heuristika): datum narození, tituly, role
  (žadatel, stavebník, povinný, pro manžele), rodina po „RD/pro" v 3. pádě,
  časté křestní jméno + příjmení (mimo názvy ulic po „ul.", „nám." …).
- **Názvy souborů dokumentů vynechány** (např. „…_Nováková.pdf"), ponechány
  jen URL. URL samotné může jméno obsahovat – je to odkaz na veřejný originál.
- Po `relevantní_do` jen metadata a odkaz (bez popisu a dokumentů).

### Měření

| Co | Hodnota |
|---|---|
| plná sklizeň lokálně | 25–35 s, 62 MB, CPU 8 s (po optimalizaci z 59 s) |
| Standby RAM po sklizni (Docker) | 95–121 MiB |
| Docker image | 24 MB |
| `search` bez RÚIAN | < 10 ms |
| `search` s `inProtectedArea` / bbox | 8–10 s (paralelně po 8, strop 25 s) |
| `get_notice` / `notice_parcels` | 0,1–0,7 s |

CU na Apify neměřeno (placený běh, HITL).

### Odchylky od plánu

- 266 desek místo 573; pokrytí ORP ~2/3 → v README otevřeně.
- Typy rozšířeny o `stanovisko`, `eia`, `majetek` (prodej/pronájem obecních
  pozemků – silný use-case pro developery), `odstraneni` samostatně.
- `watch_area` je bezstavový (cursor = čas prvního spatření), ne stav v KV.
- PDF se nečtou (v1): vyžadovalo by PDF knihovnu a OCR u skenů.
- edesky.cz jako doplněk zatím ne (vyžaduje registraci API klíče).

### Otevřené

- Čtení PDF příloh (zvýší podíl s k.ú./parcelami nad 57 % / 19 %).
- Desky obcí mimo NKOD (edesky.cz API po registraci uživatelem).
- `RUIAN_CONTACT` do env v Actor settings.
