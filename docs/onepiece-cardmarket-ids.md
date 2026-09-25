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
