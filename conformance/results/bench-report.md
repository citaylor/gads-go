### b01_records

| b01_records | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1490 | 28.1 | 53x |
| create 1000 records | 1 | 18959 | 614.9 | 31x |
| record get | 50 | 62.3 | 1.38 | 45x |
| record update | 20 | 59.9 | 1.36 | 44x |
| count all | 20 | 162.3 | 0.69 | 234x |
| page of 50 sorted | 20 | 448.6 | 5.46 | 82x |
| page of 500 all columns | 3 | 708.1 | 39.6 | 18x |
| filter and | 20 | 443.9 | 256.6 | 1.7x |
| filter or dates | 20 | 443.4 | 13.8 | 32x |
| quick search | 10 | 1369 | 20.5 | 67x |
| group by enum | 10 | 497.2 | 2.68 | 186x |
| group by curval | 10 | 655.9 | 3.21 | 204x |
| bar graph sum | 10 | 343.9 | 1.86 | 185x |
| line graph by date | 10 | 404.5 | 3.51 | 115x |
| csv download | 3 | 2709 | 191.3 | 14x |
| update-cached | 2 | 6042 | 226.3 | 27x |
| geometric mean of steps |  |  |  | 49x |

### b02_import

| b02_import | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 734.0 | 6.40 | 115x |
| import 1000 rows | 1 | 16142 | 528.8 | 31x |
| count imported | 1 | 167.1 | 0.69 | 243x |
| geometric mean of steps |  |  |  | 95x |

### b03_lua

| b03_lua | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 804.1 | 7.22 | 111x |
| lua simple | 500 | 0.05 | 0.01 | 5.8x |
| lua loop | 100 | 0.55 | 0.16 | 3.4x |
| lua strings | 200 | 0.06 | 0.02 | 3.5x |
| geometric mean of steps |  |  |  | 9.3x |

### b04_web

| b04_web | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1358 | 25.8 | 53x |
| create 1000 records | 1 | 18186 | 606.6 | 30x |
| login | 1 | 612.7 | 3.19 | 192x |
| data page | 20 | 229.3 | 4.85 | 47x |
| api records page | 20 | 1259 | 42.5 | 30x |
| api records search | 20 | 1739 | 31.3 | 56x |
| record view page | 20 | 227.8 | 12.5 | 18x |
| csv download | 3 | 9759 | 426.1 | 23x |
| geometric mean of steps |  |  |  | 42x |

### Scenarios

| scenario | perl ms | go ms | go faster | parity |
| --- | ---: | ---: | ---: | ---: |
| b01_records | 111264 | 7903 | 14x | same |
| b02_import | 17047 | 544.3 | 31x | same |
| b03_lua | 900.1 | 41.0 | 22x | same |
| b04_web | 119708 | 3770 | 32x | same |
| total | 248920 | 12258 | 20x |  |
| geometric mean of all steps |  |  | 41x |  |

### Units

| unit | perl | go | go faster |
| --- | ---: | ---: | ---: |
| lua/simple | 6.7 µs | 7.4 µs | 0.9x |
| lua/loop | 155.3 µs | 164.3 µs | 0.9x |
| lua/strings | 12.6 µs | 12.8 µs | 1.0x |
| tt/table_100 | 691.0 µs | 141.5 µs | 4.9x |
| json/encode_1000 | 11.16 ms | 1.17 ms | 9.6x |
| json/decode_1000 | 29.11 ms | 2.31 ms | 13x |
| csv/parse_1000 | 55.76 ms | 209.5 µs | 266x |
| csv/write_1000 | 10.32 ms | 263.5 µs | 39x |
| date/parse_cldr | 178.0 µs | 1.4 µs | 129x |
| date/format_cldr | 51.5 µs | 865 ns | 59x |
| filter/parse_json | 393.5 µs | 8.5 µs | 47x |
| password/hash | 8.9 µs | 1.3 µs | 7.0x |
| password/match | 19.6 µs | 1.7 µs | 11x |
| geometric mean |  |  | 12x |

(perl 5.32.1, csv Text::CSV_PP, json JSON::Backend::PP, go1.27.2)
