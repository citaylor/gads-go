### b01_records

| b01_records | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 2325 | 247.3 | 9.4x |
| create 1000 records | 1 | 27791 | 4179 | 6.7x |
| record get | 50 | 73.1 | 5.10 | 14x |
| record update | 20 | 58.6 | 5.24 | 11x |
| count all | 20 | 8.47 | 1.41 | 6.0x |
| page of 50 sorted | 20 | 232.1 | 21.5 | 11x |
| page of 500 all columns | 3 | 161.7 | 45.4 | 3.6x |
| filter and | 20 | 94.7 | 34.0 | 2.8x |
| filter or dates | 20 | 70.7 | 37.8 | 1.9x |
| quick search | 10 | 154.4 | 53.7 | 2.9x |
| group by enum | 10 | 20.5 | 7.48 | 2.7x |
| group by curval | 10 | 22.1 | 8.63 | 2.6x |
| bar graph sum | 10 | 27.5 | 5.76 | 4.8x |
| line graph by date | 10 | 65.4 | 7.27 | 9.0x |
| csv download | 3 | 2952 | 2009 | 1.5x |
| update-cached | 2 | 5525 | 571.3 | 9.7x |
| geometric mean of steps |  |  |  | 5.0x |

### b02_import

| b02_import | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1295 | 84.7 | 15x |
| import 1000 rows | 1 | 24493 | 3846 | 6.4x |
| count imported | 1 | 18.2 | 2.49 | 7.3x |
| geometric mean of steps |  |  |  | 8.9x |

### b03_lua

| b03_lua | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1453 | 101.9 | 14x |
| lua simple | 500 | 0.05 | 0.01 | 4.5x |
| lua loop | 100 | 0.38 | 0.29 | 1.3x |
| lua strings | 200 | 0.06 | 0.02 | 3.2x |
| geometric mean of steps |  |  |  | 4.1x |

### b04_web

| b04_web | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 2385 | 241.9 | 9.9x |
| create 1000 records | 1 | 27346 | 4147 | 6.6x |
| login | 1 | 634.9 | 7.54 | 84x |
| data page | 20 | 76.7 | 8.69 | 8.8x |
| api records page | 20 | 988.7 | 179.4 | 5.5x |
| api records search | 20 | 569.3 | 90.1 | 6.3x |
| record view page | 20 | 210.6 | 32.7 | 6.4x |
| csv download | 3 | 8725 | 3682 | 2.4x |
| geometric mean of steps |  |  |  | 8.4x |

### Scenarios

| scenario | perl ms | go ms | go faster | parity |
| --- | ---: | ---: | ---: | ---: |
| b01_records | 66555 | 15952 | 4.2x | same |
| b02_import | 25862 | 4442 | 5.8x | same |
| b03_lua | 1624 | 696.8 | 2.3x | same |
| b04_web | 93828 | 22442 | 4.2x | same |
| total | 187870 | 43532 | 4.3x |  |
| geometric mean of all steps |  |  | 5.9x |  |

### Units

| unit | perl | spread | go | spread | go faster | output |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| lua/simple | 7.9 µs | ±8% | 7.9 µs | ±6% | 1.0x | same |
| lua/loop | 174.2 µs | ±4% | 277.5 µs | ±6% | 0.6x | same |
| lua/strings | 12.0 µs | ±7% | 13.7 µs | ±3% | 0.9x | same |
| tt/table_100 | 707.8 µs | ±11% | 135.2 µs | ±2% | 5.2x | same |
| json/encode_1000 | 247.6 µs | ±5% | 1.54 ms | ±2% | 0.2x | same |
| json/decode_1000 | 650.3 µs | ±7% | 2.20 ms | ±1% | 0.3x | same |
| csv/parse_1000 | 1.60 ms | ±9% | 206.4 µs | ±2% | 7.7x | same |
| csv/write_1000 | 2.07 ms | ±11% | 322.6 µs | ±4% | 6.4x | same |
| date/parse_cldr | 163.7 µs | ±12% | 1.5 µs | ±2% | 111x | same |
| date/format_cldr | 60.7 µs | ±6% | 1.4 µs | ±2% | 45x | same |
| filter/parse_json | 57.3 µs | ±4% | 8.2 µs | ±2% | 7.0x | same |
| password/hash | 8.6 µs | ±5% | 1.3 µs | ±1% | 6.8x | same |
| password/match | 24.3 µs | ±11% | 1.6 µs | ±2% | 15x | same |
| db/count | 4.95 ms | ±13% | 1.40 ms | ±3% | 3.5x | same |
| db/page_50 | 149.97 ms | ±19% | 21.56 ms | ±4% | 7.0x | same |
| db/quick_search | 148.73 ms | ±12% | 45.51 ms | ±20% | 3.3x | same |
| db/record_get | 58.49 ms | ±16% | 4.23 ms | ±10% | 14x | same |
| db/filter_view | 89.10 ms | ±9% | 30.71 ms | ±8% | 2.9x | same |
| db/group_enum | 16.95 ms | ±9% | 5.77 ms | ±6% | 2.9x | same |
| db/csv_100 | 293.34 ms | ±8% | 202.62 ms | ±36% | 1.4x | same |
| calc/eval_column | 61.0 µs | ±6% | 25.9 µs | ±3% | 2.4x | same |
| db/record_update | 152.36 ms | ±5% | 19.77 ms | ±4% | 7.7x | same |
| geometric mean |  |  |  |  | 3.8x |  |

(perl 5.40.5, csv Text::CSV_XS, json JSON::Backend::XS, go1.27.3)
