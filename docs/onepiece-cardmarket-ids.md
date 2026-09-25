# One Piece: Cardmarket ids on catalog rows

`cmd/onepiece`'s `productCardmarketIDs` gives a TCGplayer product's row the
Cardmarket product it is sold as. The table is only for products nothing else
links correctly, whether through CardTrader's blueprints or the product's
wording. go-mtgban's Cardmarket resolver answers a product by the datastore's
own `cardmarketId` before the CardTrader bridge or the wording.

Measured on 2026-09-25 against the published `onepiece.json`, the published
Cardmarket catalog, and CardTrader's One Piece blueprints (8,703). The walk
was replayed the way go-mtgban's `walkCatalog` runs it.

## The live mispricings

| TCGplayer product | row | Cardmarket | CardTrader | before | after |
|---|---|---|---|---|---|
| 712033 O-Nami (Dash Pack 2025) | `op05-062_712033_foil` | 874419 (V.2, trend €20.43) | 326797 "Dash Pack 2025", tcg id **623070** | priced the Illustration Box row | its own row |
| 623070 O-Nami (Illustration Box Vol.1) (Textured) | `op05-062_623070_foil` | 821356 (V.1, trend €130.29) | 397560 "Illustration Box Vol.1", no tcg id | refused as a twin | see below |
| 697483 Monkey.D.Luffy (OP16 Release Event) | `p-135_697483` | 888110 (Promos, trend €1.04) | 391165 "OP16 Release Event", no tcg id | its row | its row, by id |
| 697484 Monkey.D.Luffy (OP16 Release Event Winner) | `p-135_697484_foil` | 888111 (Special Tournament Promos, trend €87.92) | 391164 "Release Event Winner", no tcg id | the plain row | its own row |

**O-Nami.** CardTrader's Dash Pack blueprint carries the Illustration Box's
TCGplayer id. That put the Dash Pack's trend on the Illustration Box: the
site's MKMTrend showed $24.05 for a card Cardmarket trends at €130.
- With the ids published, 874419 lands on the Dash Pack row. The Illustration
  Box row no longer carries its price.
- 821356 is still refused as a twin. The bridge still claims the Illustration
  Box row for 874419 before any product is answered, and go-mtgban's One Piece
  claim pass (`claimByID`, `giveWay`) reads only the bridge.
- That is go-mtgban's to fix, by honouring the datastore's own id there.
  CardTrader's wrong id also still sends CardTrader's Dash Pack listings to the
  Illustration Box row, which is another go-mtgban fix.

**P-135.** Both products' wording reached the plain Release Event row.
Only one price survived there, and the Winner row went unpriced.

## Event prints with no TCGplayer id on CardTrader

These 66 products were all unpriced on go-mtgban master's walk: 31 refused, 27
answered with a matcher error, and 8 marked twins. CardTrader links each one
to its Cardmarket product, but its blueprint carries no TCGplayer id, so the
bridge cannot reach it. A product is listed only when all of these hold:

- the Cardmarket product names the row's card and collector number;
- exactly one CardTrader blueprint links it;
- every word of CardTrader's event name is in exactly one row's label at that
  number. "Championship" is read as "CS", and "ROUND1" as "Round 1";
- the label adds no placing, "trophy" or pack word that CardTrader did not
  state;
- no row already carries the Cardmarket id.

| Cardmarket | shelf | product | CardTrader event | row | before | trend € |
|---|---|---|---|---|---|---|
| 906848 | Special Tournament Promos | Donquixote Doflamingo (OP14-060) | Flame-Flame Fruit Coliseum Promo | `op14-060_719665_foil` | refused | 1800 |
| 906846 | Special Tournament Promos | Usopp (OP10-042) | Flame-Flame Fruit Coliseum Promo | `op10-042_719663_foil` | refused | 1300 |
| 906845 | Special Tournament Promos | Trafalgar Law (OP10-022) | Flame-Flame Fruit Coliseum Promo | `op10-022_719662_foil` | refused | 921.28 |
| 904389 | Special Tournament Promos | Monkey.D.Luffy (P-099) (V.2) | Championship 26-27 / Event Pack Finalist | `p-099_710259_foil` | error | 568.25 |
| 904392 | Special Tournament Promos | Tony Tony.Chopper (P-101) (V.1) | Championship 26-27 / Celebration Pack | `p-101_710239_foil` | refused | 490 |
| 901265 | Unnumbered Promos | Monkey.D.Luffy (OP16-095) (V.1) | ROUND1 Promo | `op16-095_707248_foil` | refused | 299.99 |
| 904393 | Special Tournament Promos | Nami (P-102) (V.1) | Championship 26-27 / Celebration Pack | `p-102_710240_foil` | error | 280 |
| 901261 | Unnumbered Promos | Nami (ST29-008) (V.1) | ROUND1 Promo | `st29-008_707242_foil` | refused | 271.7 |
| 902565 | Premium Bandai Products | Monkey.D.Luffy (OP13-001) (V.1) | 3rd Anniversary Set / English Version | `op13-001_710696_foil` | refused | 201.83 |
| 904425 | Special Tournament Promos | Shakuyaku (OP14-107) (V.2) | Regionals Finalist Card Set 26-27 Vol.2 | `op14-107_710732_foil` | error | 200 |
| 904396 | Special Tournament Promos | Sabo (P-105) (V.1) | Championship 26-27 / Celebration Pack | `p-105_710243_foil` | refused | 199 |
| 821361 | Unnumbered Promos | Boa Hancock (ST17-004) (V.3) | Illustration Box Vol.1 | `st17-004_623069_foil` | error | 192.26 |
| 902556 | Premium Bandai Products | Monkey.D.Luffy (OP13-118) (V.1) | 3rd Anniversary Set / English Version | `op13-118_710700_foil` | refused | 188.13 |
| 904416 | Special Tournament Promos | Monkey.D.Dragon (OP07-015) (V.2) | Regionals Finalist Card Set 26-27 Vol.2 | `op07-015_710733_foil` | error | 150 |
| 901270 | Unnumbered Promos | Nico Robin (EB03-054) (V.1) | ROUND1 Promo | `eb03-054_707249_foil` | refused | 149.64 |
| 901264 | Unnumbered Promos | Roronoa Zoro (PRB02-006) (V.2) | ROUND1 Promo | `prb02-006_707252_foil` | twin | 145.88 |
| 904395 | Special Tournament Promos | Shanks (P-104) (V.1) | Championship 26-27 / Celebration Pack | `p-104_710242_foil` | error | 143 |
| 821360 | Unnumbered Promos | Yamato (ST13-016) (V.1) | Illustration Box Vol.2 | `st13-016_623071_foil` | twin | 127.71 |
| 896419 | Unnumbered Promos | Dracule Mihawk (OP14-020) (V.1) | PSA Magazine | `op14-020_710762_foil` | refused | 126.13 |
| 902561 | Premium Bandai Products | Sabo (OP13-004) (V.1) | 3rd Anniversary Set / English Version | `op13-004_710697_foil` | error | 126.01 |
| 902563 | Premium Bandai Products | Portgas.D.Ace (OP13-002) (V.1) | 3rd Anniversary Set / English Version | `op13-002_710694_foil` | refused | 103.67 |
| 904422 | Special Tournament Promos | Kouzuki Oden (OP14-026) (V.2) | Regionals Finalist Card Set 26-27 Vol.2 | `op14-026_710735_foil` | error | 100 |
| 906852 | Premium Bandai Products | Portgas.D.Ace (OP16-001) (V.1) | Official Playmat - Flame-Flame Fruit Coliseum | `op16-001_717035_foil` | refused | 95.96 |
| 901266 | Unnumbered Promos | Sanji (OP15-047) (V.1) | ROUND1 Promo | `op15-047_707246_foil` | refused | 94.72 |
| 821358 | Unnumbered Promos | Black Maria (OP08-074) (V.1) | Illustration Box Vol.2 | `op08-074_623068_foil` | twin | 89.56 |
| 901269 | Unnumbered Promos | Tony Tony.Chopper (OP09-068) (V.1) | ROUND1 Promo | `op09-068_707254_foil` | refused | 88.47 |
| 814902 | Unnumbered Promos | Monkey.D.Luffy (OP05-060) (V.2) | Sound Loader Vol. 1  | `op05-060_594591_foil` | twin | 69.69 |
| 904369 | Special Tournament Promos | Morley (OP12-093) (V.1) | Championship 26-27 / Celebration Pack | `op12-093_710236_foil` | refused | 58.99 |
| 901267 | Unnumbered Promos | Brook (OP11-056) (V.1) | ROUND1 Promo | `op11-056_707251_foil` | refused | 53.97 |
| 904365 | Special Tournament Promos | Tsuru (OP06-051) (V.1) | Championship 26-27 / Celebration Pack | `op06-051_710232_foil` | refused | 50 |
| 901268 | Unnumbered Promos | Usopp (OP11-003) (V.1) | ROUND1 Promo | `op11-003_707256_foil` | refused | 48.48 |
| 902555 | Premium Bandai Products | Portgas.D.Ace (OP13-119) (V.1) | 3rd Anniversary Set / English Version | `op13-119_710698_foil` | error | 48.06 |
| 901262 | Unnumbered Promos | Jinbe (ST29-005) (V.1) | ROUND1 Promo | `st29-005_707257_foil` | refused | 43.74 |
| 901263 | Unnumbered Promos | Franky (ST21-011) (V.1) | ROUND1 Promo | `st21-011_707258_foil` | refused | 41.91 |
| 902554 | Premium Bandai Products | Sabo (OP13-120) (V.1) | 3rd Anniversary Set / English Version | `op13-120_710701_foil` | refused | 38.14 |
| 902558 | Premium Bandai Products | Ace & Sabo & Luffy (OP13-007) (V.2) | 3rd Anniversary Set / English Version | `op13-007_710702_foil` | twin | 32.33 |
| 904377 | Special Tournament Promos | Trafalgar Law (P-088) (V.2) | Championship 26-27 / Event Pack Finalist | `p-088_710262_foil` | error | 30.94 |
| 904385 | Special Tournament Promos | Shanks (P-097) (V.2) | Championship 26-27 / Event Pack Finalist | `p-097_710260_foil` | error | 29.36 |
| 874423 | Unnumbered Promos | Boa Hancock (ST17-004) (V.4) | Dash Pack 2025 | `st17-004_712035_foil` | error | 23.55 |
| 904381 | Special Tournament Promos | Shirahoshi (P-091) (V.2) | Championship 26-27 / Event Pack Finalist | `p-091_710261_foil` | error | 19.71 |
| 904379 | Special Tournament Promos | Charlotte Smoothie (P-090) (V.2) | Championship 26-27 / Event Pack Finalist | `p-090_710255_foil` | error | 14 |
| 874422 | Unnumbered Promos | Black Maria (OP08-074) (V.2) | Dash Pack 2025 | `op08-074_712037_foil` | twin | 10.86 |
| 904391 | Special Tournament Promos | Marshall.D.Teach (P-100) (V.2) | Championship 26-27 / Event Pack Finalist | `p-100_710258_foil` | error | 10.48 |
| 874424 | Unnumbered Promos | Yamato (ST13-016) (V.2) | Dash Pack 2025 | `st13-016_712036_foil` | twin | 10.08 |
| 904375 | Promos | Kaido (P-040) (V.1) | Event Pack Vol. 9 | `p-040_709539_foil` | error | 6.83 |
| 900586 | Unnumbered Promos | Eustass"Captain"Kid (OP14-014) (V.2) | Illustration Box Vol.8 | `op14-014_709093_foil` | error | 1.59 |
| 902495 | Unnumbered Promos | Kaido (EB04-030) (V.1) | 4th Anniversary Treasure Campaign Pack | `eb04-030_714349_foil` | refused | 1.18 |
| 902493 | Unnumbered Promos | Shanks (OP14-027) (V.1) | 4th Anniversary Treasure Campaign Pack | `op14-027_714351_foil` | refused | 1.17 |
| 902491 | Promos | Marshall.D.Teach (P-100) (V.2) | 4th Anniversary Treasure Campaign Pack | `p-100_714353_foil` | twin | 1.04 |
| 902489 | Unnumbered Promos | Edward.Newgate (ST15-002) (V.1) | 4th Anniversary Treasure Campaign Pack | `st15-002_714354_foil` | refused | 0.77 |
| 902494 | Unnumbered Promos | Buggy (OP12-049) (V.1) | 4th Anniversary Treasure Campaign Pack | `op12-049_714350_foil` | refused | 0.5 |
| 904364 | Special Tournament Promos | Because the Side of Justice Will Be Whichever Side Wins!! (OP05-037) (V.1) | Championship 26-27 / Celebration Pack | `op05-037_710223_foil` | refused | 0 |
| 904366 | Special Tournament Promos | Bartholomew Kuma (OP09-108) (V.1) | Championship 26-27 / Celebration Pack | `op09-108_710233_foil` | refused | 0 |
| 904367 | Special Tournament Promos | Kouzuki Hiyori (OP12-028) (V.1) | Championship 26-27 / Celebration Pack | `op12-028_710234_foil` | error | 0 |
| 904368 | Special Tournament Promos | Vinsmoke Sora (OP12-062) (V.1) | Championship 26-27 / Celebration Pack | `op12-062_710235_foil` | refused | 0 |
| 904383 | Special Tournament Promos | Koby (P-092) (V.2) | Championship 26-27 / Event Pack Finalist | `p-092_710257_foil` | error | 0 |
| 904394 | Special Tournament Promos | Portgas.D.Ace (P-103) (V.1) | Championship 26-27 / Celebration Pack | `p-103_710241_foil` | error | 0 |
| 904397 | Special Tournament Promos | Monkey.D.Luffy (P-106) (V.1) | Championship 26-27 / Celebration Pack | `p-106_710244_foil` | error | 0 |
| 904417 | Special Tournament Promos | Monkey.D.Dragon (OP07-015) (V.3) | Regionals Champion Card Set 26-27 Vol.2 | `op07-015_710738_foil` | error | 0 |
| 904419 | Special Tournament Promos | Jozu (OP08-047) (V.2) | Regionals Finalist Card Set 26-27 Vol.2 | `op08-047_710734_foil` | error | 0 |
| 904420 | Special Tournament Promos | Jozu (OP08-047) (V.3) | Regionals Champion Card Set 26-27 Vol.2 | `op08-047_710739_foil` | error | 0 |
| 904423 | Special Tournament Promos | Kouzuki Oden (OP14-026) (V.3) | Regionals Champion Card Set 26-27 Vol.2 | `op14-026_710736_foil` | error | 0 |
| 904426 | Special Tournament Promos | Shakuyaku (OP14-107) (V.3) | Regionals Champion Card Set 26-27 Vol.2 | `op14-107_710737_foil` | error | 0 |
| 906847 | Special Tournament Promos | Sabo (OP13-004) | Flame-Flame Fruit Coliseum Promo | `op13-004_719664_foil` | error | 0 |
| 906849 | Special Tournament Promos | Rebecca (OP15-039) | Flame-Flame Fruit Coliseum Promo | `op15-039_719666_foil` | refused | 0 |
| 906850 | Special Tournament Promos | Lucy (OP15-002) | Flame-Flame Fruit Coliseum Promo | `op15-002_719667_foil` | refused | 0 |

Left out, for the reasons in the source list:
- 14 ambiguous products, where two or more rows hold every word;
- 126 products with no row, which are mostly event prints TCGplayer does not
  sell;
- the Sanji products 912177 to 912179. Cardmarket numbers them EB02-054, but
  CardTrader links them to TCGplayer's EB04-052 rows.
