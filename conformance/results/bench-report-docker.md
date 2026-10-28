### b01_records

| b01_records | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1318 | 28.4 | 46x |
| create 1000 records | 1 | 17684 | 597.8 | 30x |
| record get | 50 | 62.5 | 1.49 | 42x |
| record update | 20 | 58.8 | 1.35 | 43x |
| count all | 20 | 151.1 | 0.64 | 236x |
| page of 50 sorted | 20 | 434.8 | 4.91 | 89x |
| page of 500 all columns | 3 | 632.1 | 37.9 | 17x |
| filter and | 20 | 429.5 | 252.4 | 1.7x |
| filter or dates | 20 | 404.2 | 12.3 | 33x |
| quick search | 10 | 1276 | 21.3 | 60x |
| group by enum | 10 | 507.6 | 2.74 | 185x |
| group by curval | 10 | 599.1 | 3.14 | 191x |
| bar graph sum | 10 | 307.2 | 1.89 | 163x |
| line graph by date | 10 | 349.3 | 2.75 | 127x |
| csv download | 3 | 2537 | 198.2 | 13x |
| update-cached | 2 | 5495 | 222.0 | 25x |
| geometric mean of steps |  |  |  | 48x |

### b02_import

| b02_import | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 684.1 | 5.38 | 127x |
| import 1000 rows | 1 | 13603 | 525.1 | 26x |
| count imported | 1 | 165.2 | 0.69 | 238x |
| geometric mean of steps |  |  |  | 92x |

### b03_lua

| b03_lua | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 773.9 | 6.94 | 112x |
| lua simple | 500 | 0.06 | 0.01 | 7.0x |
| lua loop | 100 | 0.40 | 0.18 | 2.2x |
| lua strings | 200 | 0.06 | 0.02 | 3.7x |
| geometric mean of steps |  |  |  | 8.9x |

### b04_web

| b04_web | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1245 | 27.8 | 45x |
| create 1000 records | 1 | 16385 | 576.0 | 28x |
| login | 1 | 572.0 | 2.56 | 224x |
| data page | 20 | 212.5 | 5.29 | 40x |
| api records page | 20 | 1097 | 41.4 | 27x |
| api records search | 20 | 1632 | 30.1 | 54x |
| record view page | 20 | 196.8 | 11.5 | 17x |
| csv download | 3 | 8728 | 396.4 | 22x |
| geometric mean of steps |  |  |  | 40x |

### Scenarios

| scenario | perl ms | go ms | go faster | parity |
| --- | ---: | ---: | ---: | ---: |
| b01_records | 103811 | 7646 | 14x | same |
| b02_import | 14457 | 538.6 | 27x | same |
| b03_lua | 862.8 | 41.4 | 21x | same |
| b04_web | 108613 | 3593 | 30x | same |
| total | 227744 | 11820 | 19x |  |
| geometric mean of all steps |  |  | 39x |  |

### Units

| unit | perl | go | go faster |
| --- | ---: | ---: | ---: |
| lua/simple | 6.3 µs | 6.9 µs | 0.9x |
| lua/loop | 174.9 µs | 195.8 µs | 0.9x |
| lua/strings | 12.9 µs | 12.7 µs | 1.0x |
| tt/table_100 | 683.0 µs | 135.4 µs | 5.0x |
| json/encode_1000 | 184.4 µs | 1.16 ms | 0.2x |
| json/decode_1000 | 607.6 µs | 2.28 ms | 0.3x |
| csv/parse_1000 | 1.64 ms | 207.7 µs | 7.9x |
| csv/write_1000 | 1.05 ms | 254.9 µs | 4.1x |
| date/parse_cldr | 169.2 µs | 1.3 µs | 126x |
| date/format_cldr | 63.5 µs | 857 ns | 74x |
| filter/parse_json | 60.3 µs | 7.9 µs | 7.6x |
| password/hash | 9.6 µs | 1.2 µs | 7.8x |
| password/match | 19.3 µs | 1.7 µs | 12x |
| geometric mean |  |  | 3.8x |

(perl 5.40.5, csv Text::CSV_XS, json JSON::Backend::XS, go1.27.3)
