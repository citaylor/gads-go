#!/usr/bin/env python3
"""Generate the 017_filter scenario from upstream t/017_filter.t (filter
JSON/hash conversion and change tracking)."""
import copy
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_017_filter.py'

H1 = {'rules': [{'id': 1, 'type': 'string', 'value': 'string1', 'operator': 'equal'}], 'condition': 'AND'}
H2 = {'rules': [{'id': 2, 'type': 'string', 'value': 'string2', 'operator': 'equal'}], 'condition': 'OR'}


def f(id, op, observe=None, expect=None, **kw):
    d = {'id': id, 'op': op, 'as': 'f'}
    d.update(copy.deepcopy(kw))
    if observe:
        d['observe'] = observe
    if expect:
        d['expect'] = copy.deepcopy(expect)
    return {'filter_held': d}


def main():
    steps = [
        f('hash from json', 'new', ['as_hash'], {'as_hash': H1}, as_json=H1),
        f('json from hash', 'new', ['as_json'], {'as_json': H1}, as_hash=H1),
        f('hash after same json', 'set_json', ['as_hash', 'changed'], {'as_hash': H1, 'changed': 0}, value=H1),
        f('hash after different json', 'set_json', ['as_hash', 'changed', 'column_ids'],
          {'as_hash': H2, 'changed': 1, 'column_ids': '2'}, value=H2),
        f('back by hash', 'set_hash', ['as_json', 'changed', 'column_ids'],
          {'as_json': H1, 'changed': 1, 'column_ids': '1'}, value=H1),
        # Start with a blank filter, add filter, and check changed
        f('blank filter', 'new'),
        f('build default', 'get', ['as_json'], {'as_json': {}}),
        f('changed after update after new', 'set_hash', ['changed'], {'changed': 1}, value=H2),
        # Creation of filter and then immediate change
        f('new from json', 'new', as_json=H1),
        f('new filter changed', 'set_json', ['changed'], {'changed': 1}, value=H2),
    ]
    write_scenario(os.path.join(OUT, '017_filter.yaml'), {
        '_generator': GEN,
        'name': '017_filter',
        'source': ['t/017_filter.t'],
        'description': 'Filter conversion between JSON and hash, column IDs and change tracking',
        'steps': steps,
    })


if __name__ == '__main__':
    main()
