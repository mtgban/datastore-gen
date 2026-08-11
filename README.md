# datastore-gen

Builders for the mtgban game datastores. Each command ingests a public
card dataset and the TCGplayer catalog dump for its game and emits one
JSON datastore, published to b2://mtgban-datastore/<game>/<game>.json.xz
for go-mtgban and the website to consume.

The builders are standalone on purpose: no dependency on go-mtgban, no
external modules, types duplicated rather than imported, so a datastore
change never drags a library upgrade behind it.

- cmd/riftbound - Riftbound (League of Legends TCG), from the official
  card gallery and the category 89 catalog dump
- cmd/lorcana - Disney Lorcana, from LorcanaJSON and the category 71
  catalog dump
- cmd/onepiece - One Piece Card Game, from the category 68 catalog dump
  annotated with punk-records' mirror of the official Bandai card list
- cmd/yugioh - Yu-Gi-Oh, from the category 2 catalog dump with release
  dates filled from YGOPRODeck's set list
- cmd/fleshandblood - Flesh and Blood, from the category 62 catalog dump
  annotated with the flesh-and-blood-cards dataset
