# One Piece: numberless products publish no number

SPECIFICATIONS §2.2 and §2.4: a product the catalog files with no `Number`
carries no `number`, and its id stem is the bare TCGplayer product id. The
One Piece builder filed such a product under its card type instead
(`"number": "DON"` on the DON!! cards, `"LEADER"` on the promo Leaders),
which put the type into the id (`don_456059_foil`, `leader_661879`).

## Measured 2026-11-03

Inputs: the bucket's `onepiece/tcgplayer-catalog.json.xz` (2026-11-03 12:32
UTC) and `cardmarket_catalog.json.xz`; punk-records `cards_by_id.json` and
`packs.json` fetched once. Master `887eddd` and the branch built on them.

- 250 of 7,358 singles have no catalog Number: 244 DON!! cards, 5 promo
  Leaders, and one trophy card that already published none.
- 320 entries (314 DON!!, 6 Leader) over 249 products drop `number` and
  respell their id. Every other field of every entry is identical; sets
  and sealed are unchanged; the baseline guard counts by product and
  finish and reads 7,546 cards before and after.
- Two branch builds are byte-identical; `TestPublishedVocabulary` passes.
- Log: only the `singles:` line, which now lists the numberless products
  that are not DON!! cards.

## go-mtgban (`origin/master` 3a0b82aad)

The matcher reads no `DON`, `LEADER` or `don_`: `numbered` and
`unnumberedTiebreak` ask whether a number carries a digit, which an absent
one does not either. One scraper did: gamenerdz's `landedOnLeader` kept a
codeless listing only on a number spelled `"LEADER"`, and would have
refused every promo Leader. go-mtgban#1419 reads both shapes (no digit, on
a card typed Leader) and lands before this publishes.

go-mtgban's tests pin the old ids, so the suite on the new file fails 19
cases (9 in `matcher/onepiece`'s replay corpus, 10 in `coolstuffinc`),
every one answering the same product and finish under the new id.
go-mtgban#1418 moves those ids and the two inline fixtures to the new
shape.

Catalog replay (`TestReplayCatalogNames`, 7,358 names): 239 answers name
a respelled id; mapped through the respelling, before and after are
identical. Error to resolved 0, resolved to error 0, resolved to another
printing 0.

## Ids that moved

Old id, new id.

```
don_434340	434340
don_456059_foil	456059_foil
don_456320	456320
don_482236_foil	482236_foil
don_482237_foil	482237_foil
don_482238_foil	482238_foil
don_482239_foil	482239_foil
don_482240_foil	482240_foil
don_482241_foil	482241_foil
don_482273_foil	482273_foil
don_483145	483145
don_483146	483146
don_483167_foil	483167_foil
don_483168_foil	483168_foil
don_483169_foil	483169_foil
don_483170_foil	483170_foil
don_483171_foil	483171_foil
don_498519_foil	498519_foil
don_517476_foil	517476_foil
don_517477_foil	517477_foil
don_517478_foil	517478_foil
don_520775	520775
don_520775_foil	520775_foil
don_520776	520776
don_520776_foil	520776_foil
don_525340_foil	525340_foil
don_525667_foil	525667_foil
don_525668_foil	525668_foil
don_527633_foil	527633_foil
don_529792_foil	529792_foil
don_530119_foil	530119_foil
don_530120_foil	530120_foil
don_539205_foil	539205_foil
don_541671_foil	541671_foil
don_542683_foil	542683_foil
don_542684_foil	542684_foil
don_544805_foil	544805_foil
don_549342	549342
don_549343	549343
don_555894_foil	555894_foil
don_557078_foil	557078_foil
don_557080_foil	557080_foil
don_561656_foil	561656_foil
don_576484_foil	576484_foil
don_576485_foil	576485_foil
don_577568_foil	577568_foil
don_586181_foil	586181_foil
don_586188_foil	586188_foil
don_586191_foil	586191_foil
don_586202_foil	586202_foil
don_586481_foil	586481_foil
don_586482_foil	586482_foil
don_586550_foil	586550_foil
don_586552_foil	586552_foil
don_586553_foil	586553_foil
don_586554_foil	586554_foil
don_586556_foil	586556_foil
don_586557_foil	586557_foil
don_586558_foil	586558_foil
don_586723_foil	586723_foil
don_586810_foil	586810_foil
don_586884_foil	586884_foil
don_586885_foil	586885_foil
don_586886_foil	586886_foil
don_586887_foil	586887_foil
don_586888_foil	586888_foil
don_587191_foil	587191_foil
don_587192_foil	587192_foil
don_587237_foil	587237_foil
don_587399_foil	587399_foil
don_587573_foil	587573_foil
don_587705_foil	587705_foil
don_587706_foil	587706_foil
don_587954_foil	587954_foil
don_587955_foil	587955_foil
don_587957_foil	587957_foil
don_593448_foil	593448_foil
don_593449_foil	593449_foil
don_593801	593801
don_593801_foil	593801_foil
don_593802	593802
don_593802_foil	593802_foil
don_593803	593803
don_593803_foil	593803_foil
don_593804	593804
don_593804_foil	593804_foil
don_593805	593805
don_593805_foil	593805_foil
don_593806	593806
don_593806_foil	593806_foil
don_593807	593807
don_593807_foil	593807_foil
don_593808	593808
don_593808_foil	593808_foil
don_593809	593809
don_593809_foil	593809_foil
don_593810	593810
don_593810_foil	593810_foil
don_593811	593811
don_593811_foil	593811_foil
don_593812	593812
don_593812_foil	593812_foil
don_593813	593813
don_593813_foil	593813_foil
don_593814	593814
don_593814_foil	593814_foil
don_593815	593815
don_593815_foil	593815_foil
don_593816	593816
don_593816_foil	593816_foil
don_593817	593817
don_593817_foil	593817_foil
don_593818	593818
don_593818_foil	593818_foil
don_593819	593819
don_593819_foil	593819_foil
don_593820	593820
don_593820_foil	593820_foil
don_593821	593821
don_593821_foil	593821_foil
don_593822	593822
don_593822_foil	593822_foil
don_593823	593823
don_593823_foil	593823_foil
don_593824	593824
don_593824_foil	593824_foil
don_593825	593825
don_593825_foil	593825_foil
don_593826	593826
don_593826_foil	593826_foil
don_593827	593827
don_593827_foil	593827_foil
don_593828	593828
don_593828_foil	593828_foil
don_593829	593829
don_593829_foil	593829_foil
don_593830	593830
don_593830_foil	593830_foil
don_600706	600706
don_604246_foil	604246_foil
don_605125_foil	605125_foil
don_605126_foil	605126_foil
don_619595	619595
don_620097_foil	620097_foil
don_620148_foil	620148_foil
don_620149_foil	620149_foil
don_620151_foil	620151_foil
don_623626_foil	623626_foil
don_624351_foil	624351_foil
don_624357	624357
don_624357_foil	624357_foil
don_624673	624673
don_624673_foil	624673_foil
don_624674	624674
don_624674_foil	624674_foil
don_635475_foil	635475_foil
don_636742_foil	636742_foil
don_636744_foil	636744_foil
don_636745_foil	636745_foil
don_646574_foil	646574_foil
don_649750_foil	649750_foil
don_649751_foil	649751_foil
don_651174_foil	651174_foil
don_651175_foil	651175_foil
don_655110	655110
don_655110_foil	655110_foil
don_655111_foil	655111_foil
don_655112_foil	655112_foil
don_655113	655113
don_655113_foil	655113_foil
don_655114	655114
don_655114_foil	655114_foil
don_655115_foil	655115_foil
don_655116_foil	655116_foil
don_655117	655117
don_655117_foil	655117_foil
don_655118_foil	655118_foil
don_655119	655119
don_655119_foil	655119_foil
don_655120	655120
don_655120_foil	655120_foil
don_655121_foil	655121_foil
don_655122	655122
don_655122_foil	655122_foil
don_655123_foil	655123_foil
don_655124_foil	655124_foil
don_655125	655125
don_655125_foil	655125_foil
don_655126_foil	655126_foil
don_655127	655127
don_655127_foil	655127_foil
don_655128	655128
don_655128_foil	655128_foil
don_655129_foil	655129_foil
don_655130	655130
don_655130_foil	655130_foil
don_655131_foil	655131_foil
don_655132_foil	655132_foil
don_655133	655133
don_655133_foil	655133_foil
don_655134_foil	655134_foil
don_655135	655135
don_655135_foil	655135_foil
don_655136	655136
don_655136_foil	655136_foil
don_655137_foil	655137_foil
don_655138_foil	655138_foil
don_655139	655139
don_655139_foil	655139_foil
don_655140	655140
don_655140_foil	655140_foil
don_655141_foil	655141_foil
don_655480	655480
don_655480_foil	655480_foil
don_655481_foil	655481_foil
don_655482	655482
don_655482_foil	655482_foil
don_655483_foil	655483_foil
don_655485	655485
don_655485_foil	655485_foil
don_655486_foil	655486_foil
don_655487_foil	655487_foil
don_655489	655489
don_655489_foil	655489_foil
don_655513	655513
don_655513_foil	655513_foil
don_655514_foil	655514_foil
don_655515	655515
don_655515_foil	655515_foil
don_655516_foil	655516_foil
don_655740_foil	655740_foil
don_655741	655741
don_655741_foil	655741_foil
don_655742	655742
don_655742_foil	655742_foil
don_655743_foil	655743_foil
don_655895	655895
don_655895_foil	655895_foil
don_655896_foil	655896_foil
don_655898	655898
don_655898_foil	655898_foil
don_655899_foil	655899_foil
don_655911_foil	655911_foil
don_655912	655912
don_655912_foil	655912_foil
don_655913	655913
don_655913_foil	655913_foil
don_655914_foil	655914_foil
don_655915_foil	655915_foil
don_655916	655916
don_655916_foil	655916_foil
don_655917	655917
don_655917_foil	655917_foil
don_655918_foil	655918_foil
don_660710_foil	660710_foil
don_660744_foil	660744_foil
don_672185_foil	672185_foil
don_672186_foil	672186_foil
don_672188_foil	672188_foil
don_672190_foil	672190_foil
don_672598	672598
don_672736_foil	672736_foil
don_672738_foil	672738_foil
don_677559_foil	677559_foil
don_677560	677560
don_677560_foil	677560_foil
don_677566	677566
don_677566_foil	677566_foil
don_677567_foil	677567_foil
don_677568	677568
don_677568_foil	677568_foil
don_677569_foil	677569_foil
don_677570	677570
don_677570_foil	677570_foil
don_677571_foil	677571_foil
don_680489_foil	680489_foil
don_681870_foil	681870_foil
don_681871_foil	681871_foil
don_683969	683969
don_686650_foil	686650_foil
don_686651_foil	686651_foil
don_689810_foil	689810_foil
don_689811_foil	689811_foil
don_692132_foil	692132_foil
don_693163_foil	693163_foil
don_696699_foil	696699_foil
don_698313_foil	698313_foil
don_698314_foil	698314_foil
don_698315_foil	698315_foil
don_698316_foil	698316_foil
don_702306	702306
don_710745	710745
don_710745_foil	710745_foil
don_710746_foil	710746_foil
don_710859_foil	710859_foil
don_710860_foil	710860_foil
don_711010_foil	711010_foil
don_711420_foil	711420_foil
don_711421_foil	711421_foil
don_712744_foil	712744_foil
don_712745_foil	712745_foil
don_712747	712747
don_712748	712748
don_712749	712749
don_712750	712750
don_715681_foil	715681_foil
don_715682_foil	715682_foil
don_717459	717459
don_719661_foil	719661_foil
don_719824_foil	719824_foil
don_725205_foil	725205_foil
don_728077_foil	728077_foil
don_728606_foil	728606_foil
don_728608_foil	728608_foil
leader_518691	518691
leader_518692	518692
leader_552137	552137
leader_552137_foil	552137_foil
leader_661879	661879
leader_712746	712746
```
