# riftbound-datastore

Builds the Riftbound datastore file consumed by
[go-mtgban](https://github.com/mtgban/go-mtgban)'s `mtgmatcher/riftbound`
loader, and publishes it to B2 for the workers to pull.

The tool merges two sources:

- the official card gallery payload from riftbound.leagueoflegends.com
  (resolving the current site build id automatically), which provides the
  main sets;
- our own TCGplayer catalog dump for category 89, written by tcgdumper and
  published beside the datastore, passed in with `-tcg-catalog`. It provides
  the TCGplayer product id stamped onto every printing (feeding the matcher's
  external identifier index), the promotional printings the gallery does not
  carry, appended as separate promo-typed sets, and the finishes each
  printing is sold in.

The gallery says nothing about finish, and most of Riftbound is sold in one
finish only - promotional printings foil, starter cards plain - so the
finishes come from the printings the catalog lists for a product. The dump is
used rather than a price feed because it names a printing whether or not
anyone is selling it today.

The output is the gallery payload itself with the extra data merged into the
gallery blade, so the loader reads it unchanged. Before writing anything the
result is round-tripped through the real `mtgmatcher/riftbound` loader and
checked against a minimum printing count (`-min-cards`, default 1000), so a
broken upstream payload can never be published.

## Usage

```
go run . -o riftbound.json
```

## Publishing

The `publish` workflow runs daily (and on demand) and uploads the file to
`b2://mtgban-datastore/riftbound/riftbound.json.xz`, so each game keeps its
own folder in the shared datastore bucket. Consumers decompress by suffix, so
the extension matters. It needs the secrets
`B2_APPLICATION_KEY_ID_DATASTORE` and `B2_APPLICATION_KEY_DATASTORE`, an
application key allowed to write that bucket.

go-mtgban's `bantool-*_riftbound` workflows read the object straight from B2
(`-datastore b2://mtgban-datastore/riftbound/riftbound.json.xz`), so the
bucket can stay private and no `DATASTORE_RIFTBOUND` URL is needed.

Consumers (the go-mtgban `bantool-*_riftbound` workflows) should point their
`DATASTORE_RIFTBOUND` repository variable at the bucket's public URL for the
file.

## License

MIT, see [LICENSE](LICENSE). Card data, images, and trademarks belong to Riot
Games; this project is not affiliated with or endorsed by Riot Games.
