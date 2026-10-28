#!/usr/bin/env python3
"""Generate the 003_search_changed scenario from upstream
t/003_search_changed.t (search for values changed after a date)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_003_search_changed.py'


def main():
    writes = []
    for i, (s, e, dr) in enumerate([('foo1', 1, ['2019-01-01', '2019-02-01']),
                                    ('foo2', 2, ['2020-01-01', '2020-02-01']),
                                    ('foo3', 3, ['2021-01-01', '2021-02-01'])]):
        for j in range(3):
            month = i * 3 + j + 1
            writes.append(({'string1': s, 'enum1': e, 'daterange1': dr}, '2014-%02d-01 12:00:00' % month))
    steps = [{'sheet': {'as': 'main'}}]
    for n, (values, dt) in enumerate(writes):
        steps.append({'set_time': {'time': dt}})
        if n == 0:
            steps.append({'held': {'op': 'new', 'as': 'r', 'sheet': 'main'}})
        else:
            steps.append({'held': {'op': 'find', 'as': 'r', 'sheet': 'main', 'clear': True, 'current_id': '@held:r'}})
        steps.append({'held': {'op': 'set', 'as': 'r', 'values': values}})
        steps.append({'held': {'id': 'write %d' % (n + 1), 'op': 'write', 'as': 'r', 'options': {'no_alerts': 1}}})
    steps.append({'set_time': {'time': '2014-01-14 12:00:00'}})
    tests = [('2014-09-15', 0), ('2014-08-15', 0), ('2014-07-15', 0), ('2014-06-15', 1), ('2014-05-15', 1),
             ('2014-01-15', 1), ('2013-12-15', 1), ('CURDATE + 86400', 1)]
    for date, count in tests:
        for col in ('enum1', 'string1', 'calc1'):
            steps.append({'search': {
                'id': 'changed after %s %s' % (date, col),
                'sheet': 'main',
                'view': {'name': 'Test view', 'columns': ['string1', col]},
                'filter': {'rules': [{'name': col, 'type': 'date', 'value': date, 'operator': 'changed_after'}]},
                'expect': {'count': str(count)},
            }})
    write_scenario(os.path.join(OUT, '003_search_changed.yaml'), {
        '_generator': GEN,
        'name': '003_search_changed',
        'source': ['t/003_search_changed.t'],
        'description': 'Search for values that have changed after a date',
        'steps': steps,
    })


if __name__ == '__main__':
    main()
