### b01_records

| b01_records | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 2502 | 277.4 | 9.0x |
| create 1000 records | 1 | 30452 | 4670 | 6.5x |
| record get | 50 | 83.5 | 8.97 | 9.3x |
| record update | 20 | 76.8 | 8.43 | 9.1x |
| count all | 20 | 9.15 | 2.85 | 3.2x |
| page of 50 sorted | 20 | 214.4 | 20.9 | 10x |
| page of 500 all columns | 3 | 160.4 | 50.2 | 3.2x |
| filter and | 20 | 109.1 | 18.9 | 5.8x |
| filter or dates | 20 | 79.3 | 21.0 | 3.8x |
| quick search | 10 | 186.4 | 46.0 | 4.1x |
| group by enum | 10 | 26.5 | 11.5 | 2.3x |
| group by curval | 10 | 28.8 | 12.4 | 2.3x |
| bar graph sum | 10 | 29.4 | 9.40 | 3.1x |
| line graph by date | 10 | 67.9 | 11.6 | 5.9x |
| csv download | 3 | 3723 | 1488 | 2.5x |
| update-cached | 2 | 8864 | 750.1 | 12x |
| geometric mean of steps |  |  |  | 5.0x |

### b02_import

| b02_import | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1299 | 88.3 | 15x |
| import 1000 rows | 1 | 22143 | 4006 | 5.5x |
| count imported | 1 | 20.4 | 5.14 | 4.0x |
| geometric mean of steps |  |  |  | 6.9x |

### b03_lua

| b03_lua | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1429 | 108.2 | 13x |
| lua simple | 500 | 0.05 | 0.01 | 5.2x |
| lua loop | 100 | 0.57 | 0.16 | 3.7x |
| lua strings | 200 | 0.06 | 0.02 | 3.3x |
| geometric mean of steps |  |  |  | 5.4x |

### b04_web

| b04_web | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 2232 | 250.3 | 8.9x |
| create 1000 records | 1 | 25668 | 4250 | 6.0x |
| login | 1 | 657.4 | 8.63 | 76x |
| data page | 20 | 93.8 | 12.6 | 7.4x |
| api records page | 20 | 1279 | 218.9 | 5.8x |
| api records search | 20 | 728.2 | 138.2 | 5.3x |
| record view page | 20 | 208.2 | 34.0 | 6.1x |
| csv download | 3 | 11109 | 3327 | 3.3x |
| geometric mean of steps |  |  |  | 8.1x |

### Scenarios

| scenario | perl ms | go ms | go faster | parity |
| --- | ---: | ---: | ---: | ---: |
| b01_records | 79818 | 14416 | 5.5x | same |
| b02_import | 23467 | 4558 | 5.1x | same |
| b03_lua | 1529 | 593.8 | 2.6x | same |
| b04_web | 108409 | 23081 | 4.7x | same |
| total | 213223 | 42648 | 5.0x |  |
| geometric mean of all steps |  |  | 5.9x |  |

