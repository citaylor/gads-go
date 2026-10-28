#!/usr/bin/env python3
"""Generate the 019_remember scenario from upstream t/019_remember.t
(loading remembered values from the user's last record)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario, rule  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_019_remember.py'

DATA = {
    'string1': 'Bar', 'integer1': 99, 'date1': '2009-01-02', 'enum1': 'foo1', 'tree1': 'tree1',
    'daterange1': ['2008-05-04', '2008-07-14'], 'person1': 1,
}
EXPECTED = {'string1': 'Bar', 'integer1': '99', 'date1': '2009-01-02', 'enum1': 'foo1', 'tree1': 'tree1',
            'daterange1': '2008-05-04 to 2008-07-14', 'person1': 'User1, User1'}
USERINPUT = ['string1', 'integer1', 'enum1', 'tree1', 'date1', 'daterange1', 'file1', 'person1']
COLS = list(DATA)


def remembered(id, as_, expected):
    return [
        {'held': {'op': 'new', 'as': as_, 'sheet': 'main'}},
        {'held': {'id': id, 'op': 'load_remembered_values', 'as': as_, 'datum': COLS, 'datum_props': ['as_string'],
                  'expect': {'datum': {c: {'as_string': expected[c]} for c in COLS}}}},
    ]


def main():
    steps = [{'sheet': {'as': 'main', 'data': [DATA]}}]
    steps += [{'column': {'op': 'update', 'sheet': 'main', 'column': c, 'set': {'remember': 1}}} for c in USERINPUT]
    steps.append({'clear_layouts': {}})
    steps += remembered('remembered values', 'r1', EXPECTED)
    # A user can still load previous values from a record they no longer
    # have access to
    steps += [
        {'view': {'sheet': 'main', 'name': 'Limit to view', 'is_admin': 1, 'as': 'limit',
                  'filter': {'rules': [rule('string1', 'Foobar')]}}},
        {'user': {'op': 'view_limits', 'user': 'superadmin', 'views': ['limit']}},
    ]
    steps += remembered('remembered values with view limit', 'r2', EXPECTED)
    steps.append({'user': {'op': 'view_limits', 'user': 'superadmin', 'views': []}})
    # Loading a deleted record returns blank
    steps += [
        {'db': {'id': 'last record', 'op': 'select', 'table': 'user_lastrecord',
                'columns': ['record_id', 'instance_id', 'user_id'], 'expect': [{'record_id': 1}]}},
        {'held': {'op': 'find', 'as': 'd', 'sheet': 'main', 'current_id': 1}},
        {'held': {'op': 'delete', 'as': 'd'}},
        {'held': {'op': 'clear', 'as': 'd'}},
        {'held': {'op': 'initialise', 'as': 'd'}},
        {'held': {'id': 'deleted record blank', 'op': 'load_remembered_values', 'as': 'd', 'datum': COLS,
                  'datum_props': ['as_string'], 'expect': {'datum': {c: {'as_string': ''} for c in COLS}}}},
    ]
    write_scenario(os.path.join(OUT, '019_remember.yaml'), {
        '_generator': GEN,
        'name': '019_remember',
        'source': ['t/019_remember.t'],
        'description': "Remembered values loaded from the user's last record, despite view limits but not once deleted",
        'steps': steps,
    })


if __name__ == '__main__':
    main()
