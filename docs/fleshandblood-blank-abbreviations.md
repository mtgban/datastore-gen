# Flesh and Blood: the set codes a blank abbreviation is given

TCGplayer leaves the abbreviation blank on 47 Flesh and Blood groups. Every
builder gives such a group `G<group id>` through `emit.SetCodes`. This table
lists what each of the 47 groups is called under that rule, next to the
group-name initials `cmd/fleshandblood` used to derive for it.

It was measured on 2026-11-03 against the bucket's catalog and the-fab-cube
files of that day. The `G<id>` rule changes the `setCode` of 3,031 cards
and the id of 70 sealed products, which spells its set code. No card id
changes, and the go-mtgban catalog replay (10,721 names) gives the same
answer for every name.

The initials collided four times (ADA, BDHHK, BDOA, HP1BDD), so a second
group was already being carried on `<initials>-<group id>`. None of the
initials is the code the cards print: The Hunted's cards are numbered HNT,
not TH.

| group | before | after | cards | sealed |
|---|---|---|---|---|
| 1st Strike | 1S | G23548 | 65 | 3 |
| Armory Deck: Arakni | ADA-24526 | G24526 | 31 | 2 |
| Armory Deck: Aurora | ADA-24162 | G24162 | 31 | 1 |
| Armory Deck: Boltyn | ADB | G23534 | 29 | 2 |
| Armory Deck: Gravy Bones | ADGB | G24268 | 51 | 1 |
| Armory Deck: Ira | ADI | G24315 | 27 | 2 |
| Armory Deck: Jarl Vetreidi | ADJV | G23771 | 31 | 1 |
| Armory Deck: Kayo | ADK | G23506 | 30 | 2 |
| Armory Deck: Legends Prism | ADLP | G24417 | 28 | 1 |
| Armory Deck: Legends Viserai | ADLV | G24416 | 30 | 1 |
| Armory Deck: Maxx Nitro | ADMN | G24253 | 32 | 2 |
| Armory Deck: Pleiades | ADP | G24375 | 32 | 1 |
| Armory Deck: Rhinar | ADR | G24474 | 30 | 2 |
| Blitz Deck: Heavy Hitters - Betsy | BDHHB | G23390 | 31 | 1 |
| Blitz Deck: Heavy Hitters - Kassai | BDHHK | G23384 | 30 | 1 |
| Blitz Deck: Heavy Hitters - Kayo | BDHHK-23388 | G23388 | 29 | 1 |
| Blitz Deck: Heavy Hitters - Olympia | BDHHO | G23391 | 30 | 1 |
| Blitz Deck: Heavy Hitters - Rhinar | BDHHR | G23389 | 29 | 1 |
| Blitz Deck: Heavy Hitters - Victor | BDHHV | G23392 | 30 | 1 |
| Blitz Deck: Outsiders - Arakni | BDOA | G22898 | 29 | 1 |
| Blitz Deck: Outsiders - Azalea | BDOA-22899 | G22899 | 30 | 1 |
| Blitz Deck: Outsiders - Benji | BDOB | G22900 | 26 | 1 |
| Blitz Deck: Outsiders - Katsu | BDOK | G22901 | 26 | 1 |
| Blitz Deck: Outsiders - Riptide | BDOR | G22902 | 30 | 1 |
| Blitz Deck: Outsiders - Uzuri | BDOU | G22903 | 31 | 1 |
| Blitz Deck: Part the Mistveil - Enigma | BDPTME | G23477 | 29 | 1 |
| Blitz Deck: Part the Mistveil - Nuu | BDPTMN | G23478 | 30 | 1 |
| Blitz Deck: Part the Mistveil - Zen | BDPTMZ | G23479 | 30 | 1 |
| Blitz Deck: The Hunted - Arakni | BDTHA | G24212 | 26 | 0 |
| Blitz Deck: The Hunted - Arakni, Web of Deceit | BDTHAWOD | G24214 | 31 | 0 |
| Blitz Deck: The Hunted - Cindra | BDTHC | G24215 | 29 | 0 |
| Blitz Deck: The Hunted - Fang | BDTHF | G24213 | 28 | 0 |
| High Seas | HS | G24266 | 552 | 3 |
| Historic Pack 1 Blitz Deck: Bravo | HP1BDB | G23760 | 27 | 0 |
| Historic Pack 1 Blitz Deck: Dash | HP1BDD | G23761 | 28 | 0 |
| Historic Pack 1 Blitz Deck: Dorinthea | HP1BDD-23762 | G23762 | 27 | 0 |
| Historic Pack 1 Blitz Deck: Kano | HP1BDK | G23763 | 27 | 0 |
| Historic Pack 1 Blitz Deck: Rhinar | HP1BDR | G23764 | 26 | 0 |
| Historic Pack 1 Blitz Deck: Viserai | HP1BDV | G23765 | 27 | 0 |
| Mastery Pack Guardian | MPG | G24330 | 214 | 3 |
| Outbreak Pre-Release Kit | OPRK | G24916 | 0 | 1 |
| Silver Age Chapter 1 | SAC1 | G24524 | 180 | 6 |
| Silver Age Chapter 2 | SAC2 | G24525 | 185 | 6 |
| Silver Age Chapter 3 | SAC3 | G24706 | 175 | 6 |
| Silver Age: Outbreak | SAO | G24915 | 0 | 4 |
| Smash Palace | SP | G24415 | 34 | 1 |
| The Hunted | TH | G23815 | 528 | 4 |
