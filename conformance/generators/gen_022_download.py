#!/usr/bin/env python3
"""Generate the 022_download scenario from upstream t/022_download.t (CSV
download of records, unaffected by records written during the download),
plus quoting of special values."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_022_download.py'


def main():
    edit = '2024-01-10 14:00:00'
    lines = ['%d,"%s","User1, User1",foobar,%d\n' % (i, edit, i) for i in range(1, 1001)]
    steps = [
        {'sheet': {'as': 'main', 'data': [{'string1': 'foobar', 'integer1': i} for i in range(1, 1001)]}},
        {'download': {
            'id': 'download',
            'sheet': 'main',
            'view': {'name': 'Test', 'columns': ['_version_datetime', '_version_user', 'string1', 'integer1'],
                     'sorts': [{'name': 'integer1', 'type': 'asc'}]},
            # A record added part way through has no impact on the download
            'insert_after': {'line': 500, 'values': {'string1': 'foobar', 'integer1': 800}},
            'expect': {'header': 'ID,"Last edited time","Last edited by",string1,integer1\n', 'lines': lines},
        }},
        # Quoting of special values
        {'reset': {}},
        {'sheet': {'as': 'main', 'data': [
            {'string1': 'plain', 'integer1': 1},
            {'string1': 'with, comma', 'integer1': 2},
            {'string1': 'with "quotes"', 'integer1': 3},
            {'string1': 'line\nbreak', 'integer1': 4},
            {'string1': '', 'integer1': 5},
            {'string1': 'tab\there', 'integer1': 6},
            {'string1': 'café ünïcode', 'integer1': 7},
            {'string1': '0', 'integer1': 8},
        ]}},
        {'download': {
            'id': 'quoting',
            'sheet': 'main',
            'view': {'name': 'Quoting', 'columns': ['string1', 'integer1', 'enum1', 'daterange1'],
                     'sorts': [{'name': 'integer1', 'type': 'asc'}]},
        }},
        {'download': {'id': 'no download permission', 'sheet': 'main', 'user': 'normal1', 'layout_user': True,
                      'expect_error': 'You do not have permission to download data'}},
    ]
    write_scenario(os.path.join(OUT, '022_download.yaml'), {
        '_generator': GEN,
        'name': '022_download',
        'source': ['t/022_download.t'],
        'description': 'CSV download of records, unaffected by records written during the download, and quoting of values',
        'fixed_time': '2024-01-10 14:00:00',
        'steps': steps,
    })


if __name__ == '__main__':
    main()
