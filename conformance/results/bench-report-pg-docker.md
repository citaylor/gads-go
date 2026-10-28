### b01_records

| b01_records | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 2063 | 216.2 | 9.5x |
| create 1000 records | 1 | 22009 | 3476 | 6.3x |
| record get | 50 | 69.2 | 10.4 | 6.7x |
| record update | 20 | 55.7 | 10.1 | 5.5x |
| count all | 20 | 7.06 | 3.42 | 2.1x |
| page of 50 sorted | 20 | 134.8 | 24.8 | 5.4x |
| page of 500 all columns | 3 | 138.2 | 47.0 | 2.9x |
| filter and | 20 | 88.4 | 24.8 | 3.6x |
| filter or dates | 20 | 62.1 | 27.6 | 2.3x |
| quick search | 10 | 150.5 | 58.6 | 2.6x |
| group by enum | 10 | 25.0 | 8.70 | 2.9x |
| group by curval | 10 | 23.7 | 10.0 | 2.4x |
| bar graph sum | 10 | 25.9 | 8.67 | 3.0x |
| line graph by date | 10 | 56.5 | 6.92 | 8.2x |
| csv download | 3 | 2328 | 676.7 | 3.4x |
| update-cached | 2 | 5281 | 563.8 | 9.4x |
| geometric mean of steps |  |  |  | 4.2x |

### b02_import

| b02_import | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1209 | 76.2 | 16x |
| import 1000 rows | 1 | 19241 | 3197 | 6.0x |
| count imported | 1 | 20.4 | 4.91 | 4.2x |
| geometric mean of steps |  |  |  | 7.4x |

### b03_lua

| b03_lua | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 1336 | 92.5 | 14x |
| lua simple | 500 | 0.05 | 0.01 | 5.9x |
| lua loop | 100 | 0.39 | 0.19 | 2.0x |
| lua strings | 200 | 0.06 | 0.02 | 3.3x |
| geometric mean of steps |  |  |  | 4.9x |

### b04_web

| b04_web | runs | perl median ms | go median ms | go faster |
| --- | ---: | ---: | ---: | ---: |
| 1:sheet | 1 | 2071 | 215.7 | 9.6x |
| create 1000 records | 1 | 22074 | 3421 | 6.5x |
| login | 1 | 609.9 | 7.22 | 84x |
| data page | 20 | 81.6 | 11.6 | 7.0x |
| api records page | 20 | 1064 | 175.6 | 6.1x |
| api records search | 20 | 607.0 | 115.8 | 5.2x |
| record view page | 20 | 188.4 | 31.1 | 6.1x |
| csv download | 3 | 9684 | 2186 | 4.4x |
| geometric mean of steps |  |  |  | 8.7x |

### Scenarios

| scenario | perl ms | go ms | go faster | parity |
| --- | ---: | ---: | ---: | ---: |
| b01_records | 55523 | 10855 | 5.1x | same |
| b02_import | 20474 | 3699 | 5.5x | same |
| b03_lua | 1418 | 549.5 | 2.6x | same |
| b04_web | 92897 | 17456 | 5.3x | same |
| total | 170311 | 32560 | 5.2x |  |
| geometric mean of all steps |  |  | 5.4x |  |

