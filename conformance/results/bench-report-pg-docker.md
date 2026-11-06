### b01_records

| b01_records | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 2495 | 248.9 | 10x |
| create 1000 records | 1 | 27828 | 3547 | 7.8x |
| record get | 50 | 72.4 | 7.50 | 9.7x |
| record update | 20 | 60.7 | 7.57 | 8.0x |
| count all | 20 | 4.72 | 2.25 | 2.1x |
| page of 50 sorted | 20 | 151.6 | 16.8 | 9.0x |
| page of 500 all columns | 3 | 141.1 | 50.8 | 2.8x |
| filter and | 20 | 99.4 | 15.7 | 6.3x |
| filter or dates | 20 | 67.9 | 16.8 | 4.0x |
| quick search | 10 | 168.6 | 37.7 | 4.5x |
| group by enum | 10 | 19.7 | 8.32 | 2.4x |
| group by curval | 10 | 21.0 | 8.49 | 2.5x |
| bar graph sum | 10 | 27.1 | 6.30 | 4.3x |
| line graph by date | 10 | 63.3 | 7.14 | 8.9x |
| csv download | 3 | 2812 | 1943 | 1.4x |
| update-cached | 2 | 6371 | 589.1 | 11x |
| geometric mean of steps |  |  |  | 4.9x |

### b02_import

| b02_import | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1336 | 79.0 | 17x |
| import 1000 rows | 1 | 23451 | 3329 | 7.0x |
| count imported | 1 | 19.4 | 5.46 | 3.6x |
| geometric mean of steps |  |  |  | 7.5x |

### b03_lua

| b03_lua | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1432 | 100.6 | 14x |
| lua simple | 500 | 0.05 | 0.01 | 5.4x |
| lua loop | 100 | 0.38 | 0.18 | 2.1x |
| lua strings | 200 | 0.06 | 0.02 | 3.4x |
| geometric mean of steps |  |  |  | 4.9x |

### b04_web

| b04_web | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 2399 | 238.5 | 10x |
| create 1000 records | 1 | 27348 | 3582 | 7.6x |
| login | 1 | 650.9 | 9.73 | 67x |
| data page | 20 | 70.5 | 8.36 | 8.4x |
| api records page | 20 | 984.5 | 137.0 | 7.2x |
| api records search | 20 | 584.2 | 75.1 | 7.8x |
| record view page | 20 | 208.9 | 28.2 | 7.4x |
| csv download | 3 | 8831 | 2992 | 3.0x |
| geometric mean of steps |  |  |  | 9.2x |

### Scenarios

| scenario | perl ms | go ms | go faster | parity |
| --- | ---: | ---: | ---: | ---: |
| b01_records | 67783 | 15185 | 4.5x | same |
| b02_import | 25106 | 4175 | 6.0x | same |
| b03_lua | 1900 | 974.3 | 1.9x | same |
| b04_web | 95230 | 19149 | 5.0x | same |
| total | 190018 | 39484 | 4.8x |  |
| geometric mean of all steps |  |  | 6.0x |  |

### Units

| unit | perl | spread | go | spread | go faster | output |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| lua/simple | 6.9 µs | ±5% | 7.6 µs | ±3% | 0.9x | same |
| lua/loop | 197.7 µs | ±8% | 178.4 µs | ±4% | 1.1x | same |
| lua/strings | 12.1 µs | ±11% | 13.9 µs | ±2% | 0.9x | same |
| tt/table_100 | 750.3 µs | ±8% | 141.9 µs | ±4% | 5.3x | same |
| json/encode_1000 | 262.6 µs | ±4% | 1.56 ms | ±2% | 0.2x | same |
| json/decode_1000 | 483.6 µs | ±2% | 2.22 ms | ±1% | 0.2x | same |
| csv/parse_1000 | 1.54 ms | ±3% | 217.1 µs | ±1% | 7.1x | same |
| csv/write_1000 | 1.81 ms | ±4% | 316.5 µs | ±1% | 5.7x | same |
| date/parse_cldr | 185.4 µs | ±10% | 1.5 µs | ±2% | 121x | same |
| date/format_cldr | 53.0 µs | ±7% | 1.4 µs | ±2% | 39x | same |
| filter/parse_json | 56.5 µs | ±7% | 8.3 µs | ±3% | 6.8x | same |
| password/hash | 8.8 µs | ±9% | 1.3 µs | ±2% | 6.8x | same |
| password/match | 22.4 µs | ±10% | 1.7 µs | ±3% | 13x | same |
| db/count | 4.74 ms | ±4% | 2.29 ms | ±4% | 2.1x | same |
| db/page_50 | 145.48 ms | ±3% | 14.75 ms | ±8% | 9.9x | same |
| db/quick_search | 144.34 ms | ±5% | 23.44 ms | ±28% | 6.2x | same |
| db/record_get | 64.24 ms | ±7% | 6.74 ms | ±15% | 9.5x | same |
| db/filter_view | 84.54 ms | ±8% | 13.44 ms | ±11% | 6.3x | same |
| db/group_enum | 15.77 ms | ±5% | 5.03 ms | ±8% | 3.1x | same |
| db/csv_100 | 281.96 ms | ±6% | 202.85 ms | ±2% | 1.4x | same |
| calc/eval_column | 63.0 µs | ±2% | 25.9 µs | ±5% | 2.4x | same |
| db/record_update | 150.26 ms | ±2% | 18.21 ms | ±2% | 8.3x | same |
| geometric mean |  |  |  |  | 4.0x |  |

(perl 5.40.5, csv Text::CSV_XS, json JSON::Backend::XS, go1.27.3)
