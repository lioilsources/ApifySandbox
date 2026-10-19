# Tradebot RESEARCH

Append-only, datovaný log quant výsledků (backtesty, paper trading). Apify věci patří do `apify/FINDINGS.md`.
Parametry se nefitují na data; každá změna parametru je nová varianta s vlastním záznamem.

---

## 2026-10-18 — Data layer a první backtest varianty A

**Data:** Binance denní svíčky (keyless) od 2017-08-17 pro BTC/ETH, ostatní od listingu (SOL a DOT od 08/2020). Kraken doplňuje jen dny, které Binance nemá (ceny obou burz se liší o jednotky bps). Stooq nejde (JS challenge). Univerzum 10 coinů pevně zvolené podle existence v roce 2020, ne podle dnešního objemu, kvůli survivorship biasu. Celkem 28 858 svíček, `make tradebot-data` je idempotentní.

**Metoda:** signál z close[t], fill na close[t+1], náklad 0,26 % fee + 0,10 % slippage na stranu. Momentum 12-1 (252/21 dní), SMA200 jako trend filtr, vol 60 dní, váha = cíl_vol / (N · vol_i), cap 40 %, rebalance band 5 p. b. Start obchodování 2018-04-28 (po 253 dnech BTC historie), 3 095 dní.

| varianta (pevné parametry) | CAGR | vol | Sharpe | maxDD | obrat/rok | náklady celkem | obchody |
|---|---|---|---|---|---|---|---|
| A: top 3, cíl vol 10 % | 2,9 % | 4,1 % | 0,85 | −5,5 % | 1,5× | 4,7 % | 203 |
| A: top 3, cíl vol 20 % | 9,5 % | 13,4 % | 0,89 | −20,6 % | 5,6× | 17,0 % | 501 |
| A: top 5, cíl vol 15 % | 4,2 % | 4,2 % | 1,08 | −5,3 % | 1,1× | 3,3 % | 153 |
| BTC buy-and-hold | 29,8 % | 61,5 % | 0,74 | −76,6 % | | | |
| A: top 3, vol 10 %, jen od 2021-09 | 0,8 % | 3,6 % | 0,46 | −5,5 % | 2,2× | 4,0 % | 169 |
| BTC hold od 2021-09 | 12,3 % | 51,5 % | 0,48 | −76,6 % | | | |

Po letech (A, top 3, vol 10 %): 2018 0,0 % · 2019 −1,5 % · 2020 +12,9 % · 2021 +9,7 % · 2022 0,0 % · 2023 +2,6 % · 2024 +2,6 % · 2025 −0,2 % · 2026 −0,7 %. BTC: −58 / +94 / +302 / +60 / −64 / +156 / +121 / −6 / −7 %.

**Čtení výsledků, poctivě:**
- Strategie dělá to, co má: v medvědích letech (2018, 2022) je v hotovosti a nemá ztrátu, maxDD −5,5 % proti −77 % u BTC. Risk-adjusted (Sharpe 0,85–1,08) je o něco lepší než hold (0,74), ale rozdíl 0,1–0,3 Sharpe na 8,5 letech **není statisticky odlišitelný od nuly** (standardní chyba Sharpe ≈ 0,35).
- Výsledek drží rok 2020. Od 09/2021 je Sharpe 0,46 a prakticky shodný s holdem (0,48). Momentum na krypto od 2021 slábne nebo bylo náhodné.
- Expozice je malá: s cílovou volatilitou 10 % a krypto volatilitou 60–80 % drží bot typicky 10–20 % kapitálu v coinech, zbytek hotovost. Proto 2,9 % p.a. Vyšší cíl (20 %) zvedne výnos na 9,5 %, ale obrat 5,6×/rok sežere 17 % kapitálu na nákladech za 8,5 let. Náklady jsou u denní rebalance hlavní nepřítel, ne signál.
- **Ekonomika:** na 10 000 CZK je očekávaný hrubý zisk řádově 300–900 CZK ročně. Placený Apify plán stojí řádově 10–15 000 CZK ročně, tedy 10–40× víc než jakýkoli realistický zisk bota. Projekt se nemůže zaplatit sám ani v optimistické variantě; jeho hodnota je pipeline, disciplína a cover letter. To je potřeba říct před G1, ne po.
- Brána G1 pro A: Sharpe > 0,5 ✓ (0,85), maxDD < 30 % ✓, horší než hold o 26,9 p. b. CAGR (limit 30 p. b.) ✓ těsně. Kritérium „CAGR vs. hold“ se ukázalo špatně zvolené, porovnává nesrovnatelné riziko; pro G1 nahrazuju: Sharpe A ≥ Sharpe hold a maxDD < 30 %.

**Co dál (F2):** varianta B (sentiment overlay) potřebuje historický sentiment, tedy Reddit backfill přes Apify (odhad ~$6, čeká na placený plán a schválení). Bez něj zůstává otázka „přidá Apify něco?“ nezodpovězená, a to je hlavní otázka celého experimentu. Druhý směr: týdenní místo denní rebalance (nižší obrat), jako varianta A2, bez fitování.
