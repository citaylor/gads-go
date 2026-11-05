### b01_records

| b01_records | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 2355 | 247.5 | 9.5x |
| create 1000 records | 1 | 25903 | 3416 | 7.6x |
| record get | 50 | 66.6 | 7.44 | 9.0x |
| record update | 20 | 56.4 | 6.86 | 8.2x |
| count all | 20 | 4.64 | 2.24 | 2.1x |
| page of 50 sorted | 20 | 141.1 | 16.3 | 8.7x |
| page of 500 all columns | 3 | 140.3 | 49.0 | 2.9x |
| filter and | 20 | 89.9 | 14.8 | 6.1x |
| filter or dates | 20 | 64.5 | 16.1 | 4.0x |
| quick search | 10 | 148.3 | 35.0 | 4.2x |
| group by enum | 10 | 18.7 | 8.52 | 2.2x |
| group by curval | 10 | 20.6 | 8.28 | 2.5x |
| bar graph sum | 10 | 24.9 | 5.52 | 4.5x |
| line graph by date | 10 | 62.4 | 7.64 | 8.2x |
| csv download | 3 | 2388 | 1876 | 1.3x |
| update-cached | 2 | 5694 | 574.2 | 9.9x |
| geometric mean of steps |  |  |  | 4.8x |

### b02_import

| b02_import | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1267 | 78.1 | 16x |
| import 1000 rows | 1 | 22181 | 3170 | 7.0x |
| count imported | 1 | 18.6 | 5.09 | 3.7x |
| geometric mean of steps |  |  |  | 7.5x |

### b03_lua

| b03_lua | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1356 | 95.7 | 14x |
| lua simple | 500 | 0.05 | 0.01 | 5.0x |
| lua loop | 100 | 0.38 | 0.19 | 2.1x |
| lua strings | 200 | 0.06 | 0.02 | 3.2x |
| geometric mean of steps |  |  |  | 4.7x |

### b04_web

| b04_web | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 2310 | 243.5 | 9.5x |
| create 1000 records | 1 | 26624 | 3298 | 8.1x |
| login | 1 | 612.1 | 9.21 | 66x |
| data page | 20 | 70.4 | 7.87 | 8.9x |
| api records page | 20 | 928.8 | 131.4 | 7.1x |
| api records search | 20 | 550.0 | 72.5 | 7.6x |
| record view page | 20 | 196.8 | 28.1 | 7.0x |
| csv download | 3 | 8424 | 2905 | 2.9x |
| geometric mean of steps |  |  |  | 9.2x |

### Scenarios

| scenario | perl ms | go ms | go faster | parity |
| --- | ---: | ---: | ---: | ---: |
| b01_records | 62014 | 14800 | 4.2x | same |
| b02_import | 23760 | 3993 | 6.0x | same |
| b03_lua | 1808 | 940.4 | 1.9x | same |
| b04_web | 90966 | 18390 | 4.9x | same |
| total | 178548 | 38123 | 4.7x |  |
| geometric mean of all steps |  |  | 5.9x |  |

### Units

| unit | perl | spread | go | spread | go faster | output |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| lua/simple | 6.5 µs | ±3% | 7.5 µs | ±2% | 0.9x | same |
| lua/loop | 170.4 µs | ±2% | 175.7 µs | ±3% | 1.0x | same |
| lua/strings | 12.9 µs | ±9% | 12.8 µs | ±3% | 1.0x | same |
| tt/table_100 | 690.2 µs | ±4% | 133.1 µs | ±1% | 5.2x | same |
| json/encode_1000 | 240.6 µs | ±7% | 1.56 ms | ±2% | 0.2x | same |
| json/decode_1000 | 560.2 µs | ±4% | 2.19 ms | ±3% | 0.3x | same |
| csv/parse_1000 | 1.50 ms | ±11% | 208.6 µs | ±1% | 7.2x | same |
| csv/write_1000 | 1.79 ms | ±11% | 306.8 µs | ±2% | 5.8x | same |
| date/parse_cldr | 156.0 µs | ±6% | 1.5 µs | ±3% | 104x | same |
| date/format_cldr | 54.2 µs | ±7% | 1.4 µs | ±4% | 40x | same |
| filter/parse_json | 60.3 µs | ±11% | 8.2 µs | ±1% | 7.4x | same |
| password/hash | 11.5 µs | ±12% | 1.3 µs | ±1% | 9.1x | same |
| password/match | 24.7 µs | ±1% | 1.6 µs | ±3% | 15x | same |
| db/count | 4.53 ms | ±4% | 2.30 ms | ±3% | 2.0x | same |
| db/page_50 | 133.92 ms | ±5% | 14.14 ms | ±8% | 9.5x | same |
| db/quick_search | 150.18 ms | ±8% | 22.84 ms | ±28% | 6.6x | same |
| db/record_get | 58.96 ms | ±14% | 6.58 ms | ±13% | 9.0x | same |
| db/filter_view | 80.74 ms | ±8% | 12.84 ms | ±8% | 6.3x | same |
| db/group_enum | 14.19 ms | ±5% | 4.79 ms | ±3% | 3.0x | same |
| db/csv_100 | 262.79 ms | ±2% | 193.78 ms | ±3% | 1.4x | same |
| calc/eval_column | 68.0 µs | ±10% | 25.2 µs | ±2% | 2.7x | same |
| db/record_update | 141.93 ms | ±6% | 18.07 ms | ±5% | 7.9x | same |
| geometric mean |  |  |  |  | 4.0x |  |

(perl 5.40.5, csv Text::CSV_XS, json JSON::Backend::XS, go1.27.3)
