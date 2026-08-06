# riftbound-datastore

Builds the Riftbound datastore file consumed by
[go-mtgban](https://github.com/mtgban/go-mtgban)'s `mtgmatcher/riftbound`
loader, and publishes it to B2 for the workers to pull.

The tool merges two public sources:

- the official card gallery payload from riftbound.leagueoflegends.com
  (resolving the current site build id automatically), which provides the
  main sets;
- the TCGplayer catalog via [tcgcsv](https://tcgcsv.com), which provides the
  TCGplayer product id stamped onto every printing (feeding the matcher's
  external identifier index) and the promotional printings the gallery does
  not carry, appended as separate promo-typed sets.

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

The `publish` workflow runs daily (and on demand) and uploads the file to B2.
It needs:

- secrets `B2_APPLICATION_KEY_ID_DATASTORE` /
  `B2_APPLICATION_KEY_DATASTORE`: an application key allowed to write the
  target bucket, inherited from the organization;
- variable `B2_BUCKET`: the bucket name alone, e.g. `mtgban-datastore` (no
  `b2://` scheme, no path); the object is always `riftbound.json`.

Consumers (the go-mtgban `bantool-*_riftbound` workflows) should point their
`DATASTORE_RIFTBOUND` repository variable at the bucket's public URL for the
file.

## License

MIT, see [LICENSE](LICENSE). Card data, images, and trademarks belong to Riot
Games; this project is not affiliated with or endorsed by Riot Games.
