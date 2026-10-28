### Units

| unit | perl | spread | go | spread | go faster | output |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| lua/simple | 6.6 µs | ±3% | 7.1 µs | ±2% | 0.9x | same |
| lua/loop | 173.7 µs | ±7% | 201.5 µs | ±3% | 0.9x | same |
| lua/strings | 12.2 µs | ±11% | 12.8 µs | ±2% | 1.0x | same |
| tt/table_100 | 697.1 µs | ±7% | 141.6 µs | ±3% | 4.9x | same |
| json/encode_1000 | 253.8 µs | ±6% | 1.19 ms | ±1% | 0.2x | same |
| json/decode_1000 | 617.1 µs | ±5% | 2.21 ms | ±0% | 0.3x | same |
| csv/parse_1000 | 1.82 ms | ±8% | 213.2 µs | ±1% | 8.5x | same |
| csv/write_1000 | 1.85 ms | ±1% | 317.0 µs | ±1% | 5.8x | same |
| date/parse_cldr | 160.4 µs | ±5% | 1.5 µs | ±1% | 105x | same |
| date/format_cldr | 54.7 µs | ±6% | 904 ns | ±1% | 60x | same |
| filter/parse_json | 64.7 µs | ±7% | 8.3 µs | ±1% | 7.8x | same |
| password/hash | 9.0 µs | ±8% | 1.3 µs | ±1% | 7.0x | same |
| password/match | 21.7 µs | ±9% | 1.7 µs | ±1% | 13x | same |
| db/count | 150.49 ms | ±6% | 638.4 µs | ±1% | 236x | same |
| db/page_50 | 442.84 ms | ±4% | 5.08 ms | ±1% | 87x | same |
| db/quick_search | 1311.31 ms | ±2% | 21.88 ms | ±2% | 60x | same |
| db/record_get | 54.20 ms | ±3% | 1.28 ms | ±1% | 42x | same |
| calc/eval_column | 59.8 µs | ±4% | 25.5 µs | ±1% | 2.3x | same |
| geometric mean |  |  |  |  | 7.5x |  |

(perl 5.40.5, csv Text::CSV_XS, json JSON::Backend::XS, go1.27.3)
