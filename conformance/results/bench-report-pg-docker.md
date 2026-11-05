### b01_records

| b01_records | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 2324 | 244.3 | 9.5x |
| create 1000 records | 1 | 25790 | 4013 | 6.4x |
| record get | 50 | 68.9 | 5.12 | 13x |
| record update | 20 | 55.3 | 5.10 | 11x |
| count all | 20 | 4.67 | 1.30 | 3.6x |
| page of 50 sorted | 20 | 137.8 | 21.3 | 6.5x |
| page of 500 all columns | 3 | 135.0 | 49.0 | 2.8x |
| filter and | 20 | 90.4 | 26.4 | 3.4x |
| filter or dates | 20 | 63.5 | 18.3 | 3.5x |
| quick search | 10 | 149.7 | 36.0 | 4.2x |
| group by enum | 10 | 21.9 | 8.80 | 2.5x |
| group by curval | 10 | 20.0 | 8.32 | 2.4x |
| bar graph sum | 10 | 25.2 | 5.81 | 4.3x |
| line graph by date | 10 | 60.0 | 7.43 | 8.1x |
| csv download | 3 | 2357 | 2003 | 1.2x |
| update-cached | 2 | 5373 | 564.1 | 9.5x |
| geometric mean of steps |  |  |  | 4.8x |

### b02_import

| b02_import | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1284 | 79.8 | 16x |
| import 1000 rows | 1 | 22254 | 3701 | 6.0x |
| count imported | 1 | 16.8 | 2.70 | 6.2x |
| geometric mean of steps |  |  |  | 8.4x |

### b03_lua

| b03_lua | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1383 | 95.6 | 14x |
| lua simple | 500 | 0.05 | 0.01 | 5.1x |
| lua loop | 100 | 0.40 | 0.21 | 1.9x |
| lua strings | 200 | 0.06 | 0.02 | 2.6x |
| geometric mean of steps |  |  |  | 4.4x |

### b04_web

| b04_web | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 2270 | 252.4 | 9.0x |
| create 1000 records | 1 | 25660 | 4002 | 6.4x |
| login | 1 | 580.9 | 7.04 | 82x |
| data page | 20 | 67.7 | 8.43 | 8.0x |
| api records page | 20 | 941.6 | 172.9 | 5.4x |
| api records search | 20 | 551.7 | 92.1 | 6.0x |
| record view page | 20 | 196.9 | 30.4 | 6.5x |
| csv download | 3 | 8370 | 3576 | 2.3x |
| geometric mean of steps |  |  |  | 8.1x |

### Scenarios

| scenario | perl ms | go ms | go faster | parity |
| --- | ---: | ---: | ---: | ---: |
| b01_records | 59835 | 14601 | 4.1x | same |
| b02_import | 23628 | 4301 | 5.5x | same |
| b03_lua | 1552 | 660.1 | 2.4x | same |
| b04_web | 89334 | 21857 | 4.1x | same |
| total | 174349 | 41420 | 4.2x |  |
| geometric mean of all steps |  |  | 5.7x |  |

### Units

| unit | perl | spread | go | spread | go faster | output |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| lua/simple | 6.6 µs | ±11% | 7.4 µs | ±3% | 0.9x | same |
| lua/loop | 164.3 µs | ±2% | 181.9 µs | ±3% | 0.9x | same |
| lua/strings | 12.0 µs | ±2% | 13.1 µs | ±1% | 0.9x | same |
| tt/table_100 | 681.2 µs | ±7% | 132.0 µs | ±2% | 5.2x | same |
| json/encode_1000 | 231.1 µs | ±4% | 1.56 ms | ±1% | 0.1x | same |
| json/decode_1000 | 582.5 µs | ±3% | 2.18 ms | ±2% | 0.3x | same |
| csv/parse_1000 | 1.71 ms | ±9% | 207.9 µs | ±1% | 8.2x | same |
| csv/write_1000 | 1.97 ms | ±10% | 307.9 µs | ±1% | 6.4x | same |
| date/parse_cldr | 157.7 µs | ±4% | 1.5 µs | ±2% | 106x | same |
| date/format_cldr | 53.3 µs | ±2% | 1.4 µs | ±3% | 39x | same |
| filter/parse_json | 55.3 µs | ±2% | 8.1 µs | ±1% | 6.8x | same |
| password/hash | 11.0 µs | ±12% | 1.3 µs | ±2% | 8.7x | same |
| password/match | 21.8 µs | ±8% | 1.6 µs | ±2% | 13x | same |
| db/count | 4.95 ms | ±7% | 1.38 ms | ±4% | 3.6x | same |
| db/page_50 | 132.55 ms | ±6% | 20.19 ms | ±9% | 6.6x | same |
| db/quick_search | 139.16 ms | ±8% | 43.86 ms | ±17% | 3.2x | same |
| db/record_get | 57.59 ms | ±8% | 4.10 ms | ±9% | 14x | same |
| db/filter_view | 83.03 ms | ±6% | 30.29 ms | ±9% | 2.7x | same |
| db/group_enum | 14.37 ms | ±5% | 5.43 ms | ±5% | 2.6x | same |
| db/csv_100 | 258.30 ms | ±2% | 435.51 ms | ±2% | 0.6x | same |
| calc/eval_column | 63.4 µs | ±11% | 25.3 µs | ±4% | 2.5x | same |
| db/record_update | 138.64 ms | ±4% | 19.44 ms | ±3% | 7.1x | same |
| geometric mean |  |  |  |  | 3.6x |  |

(perl 5.40.5, csv Text::CSV_XS, json JSON::Backend::XS, go1.27.3)
