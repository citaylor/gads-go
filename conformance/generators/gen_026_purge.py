#!/usr/bin/env python3
"""Generate the 026_purge scenario from upstream t/026_purge.t (purging the
historic values of a field)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_026_purge.py'

COLS = [
    ('tree1', 'enum', None, 'tree1'),
    ('string1', 'string', 'Foo2', 'Foo2'),
    ('integer1', 'intgr', 72, '72'),
    ('date1', 'date', '2024-10-11', '2024-10-11'),
    ('enum1', 'enum', 2, 'foo2'),
    ('daterange1', 'daterange', ['2010-02-11', '2015-06-16'], '2010-02-11 to 2015-06-16'),
    ('person1', 'person', 2, 'User2, User2'),
    ('file1', 'file', {'name': 'file2.txt', 'mimetype': 'text/plain', 'content': 'Text file2'}, 'file2.txt'),
]


def values(id, name, table, expect=None):
    cols = ['record_id', 'from', 'to', 'value'] if table == 'daterange' else ['record_id', 'value']
    d = {'id': id, 'op': 'select', 'table': table,
         'where': {'layout_id': '@column:main:%s' % name},
         'columns': cols + ['purged_by', 'purged_on']}
    if expect is not None:
        d['expect'] = expect
    return {'db': d}


def main():
    steps = [{'sheet': {'as': 'main', 'column_count': {'string': 2}, 'data': [{
        'string1': 'Foo', 'string2': 'Keep', 'integer1': 50, 'date1': '2014-10-10', 'enum1': 1,
        'daterange1': ['2012-02-10', '2013-06-15'], 'person1': 1, 'tree1': 'tree1',
        'file1': {'name': 'file1.txt', 'mimetype': 'text/plain', 'content': 'Text file1'},
    }]}}]
    for name, table, new, as_string in COLS:
        steps.append({'held': {'op': 'find', 'as': 'r', 'sheet': 'main', 'current_id': 1}})
        if new is not None:
            steps.append({'held': {'op': 'set', 'as': 'r', 'values': {name: new}}})
            steps.append({'held': {'op': 'write', 'as': 'r'}})
        steps += [
            {'held': {'id': '%s initial value' % name, 'op': 'get', 'as': 'r', 'observe': [name],
                      'expect': {'values': {name: as_string}}}},
            values('%s values before purge' % name, name, table),
            {'historic_purge': {'sheet': 'main', 'current_id': 1, 'columns': [name]}},
            {'held': {'id': '%s value purged' % name, 'op': 'find', 'as': 'r', 'sheet': 'main', 'current_id': 1,
                      'observe': [name],
                      'expect': {'values': {name: 'Purged' if table == 'file' else ''}}}},
            values('%s values after purge' % name, name, table),
            values('string2 values after purging %s' % name, 'string2', 'string'),
        ]
    # GADS-dev: purge of a whole record version, the current record rolled
    # back to the previous version
    versions = lambda id, n: [  # noqa: E731
        {'db': {'id': id + ' record versions', 'op': 'select', 'table': 'record', 'where': {'current_id': 1},
                'columns': ['id'], 'expect': [{'id': str(i)} for i in range(1, n + 1)]}},
        {'db': {'id': id + ' current version', 'op': 'select', 'table': 'current', 'where': {'id': 1},
                'columns': ['current_version_id'], 'expect': [{'current_version_id': str(n)}]}},
    ]
    steps.append({'held': {'op': 'find', 'as': 'r', 'sheet': 'main', 'current_id': 1}})
    steps += versions('before version purge', 8)
    steps.append({'held': {'id': 'purge version', 'op': 'purge_version', 'as': 'r'}})
    steps += versions('after version purge', 7)
    write_scenario(os.path.join(OUT, '026_purge.yaml'), {
        '_generator': GEN,
        'name': '026_purge',
        'source': ['t/026_purge.t'],
        'description': 'Purging the historic values of a field blanks all versions, recording who purged them',
        'steps': steps,
    })


if __name__ == '__main__':
    main()
