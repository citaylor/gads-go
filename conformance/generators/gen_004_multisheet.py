#!/usr/bin/env python3
"""Generate the 004_multisheet scenarios from upstream t/004_multisheet.t.

Records in a second sheet take values from linked records in a first sheet.
The test runs for each combination of multivalue settings of the 2 sheets,
each with its own database.
"""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario, rule  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_004_multisheet.py'

COLUMNS = ['string1', 'integer1', 'enum1', 'tree1', 'date1', 'daterange1',
           'file1', 'person1', 'curval1', 'rag1', 'calc1']

LINKED = dict(string1='Baz', integer1='', enum1='foo1', tree1='', date1='',
              daterange1='2010-10-10 to 2012-10-10', file1='', person1='',
              curval1='Foo, foo1', rag1='b_red', calc1='2010')
MAIN = dict(string1='', integer1='', enum1='foo2', tree1='', date1='',
            daterange1='2010-10-15 to 2013-10-10', file1='', person1='',
            curval1='Bar, foo2', rag1='b_red', calc1='2010')

DR_RULE = rule('daterange1', '2011-10-10', 'contains', type='daterange')

FILTERS = [
    ('Basic - ascending', [DR_RULE], 'asc', [LINKED, MAIN]),
    ('Basic - descending', [DR_RULE], 'desc', [MAIN, LINKED]),
    ('Curval search of ID in parent record', [rule('curval1', '1')], 'desc', [LINKED]),
    ('Curval search of ID in main record', [rule('curval1', '2')], 'desc', [MAIN]),
    ('Curval search of string sub-field in parent record', [rule('curval1_string1', 'Foo')], 'desc', [LINKED]),
    ('Curval search of enum sub-field in parent record', [rule('curval1_enum1', 'foo1')], 'desc', [LINKED]),
]


def scenario(mv1, mv2):
    steps = [
        {'sheet': {'as': 'curval', 'instance_id': 3}},
        # Throw alignment of record IDs with current IDs
        {'held': {'op': 'find', 'as': 'c', 'sheet': 'curval', 'current_id': 1}},
        {'held': {'op': 'set', 'as': 'c', 'values': {'integer1': 150}}},
        {'held': {'op': 'write', 'as': 'c'}},
        {'sheet': {'as': 'sheet1', 'instance_id': 1, 'data': [], 'multivalue': mv1,
                   'curval': 'curval', 'curval_fields': ['string1', 'enum1']}},
        {'sheet': {'as': 'sheet2', 'instance_id': 2, 'data': [], 'multivalue': mv2,
                   'curval': 'curval', 'curval_fields': ['string1', 'enum1']}},
        {'clear_layouts': {}},
    ]
    for c in ('daterange1', 'enum1', 'curval1'):
        steps.append({'column': {'op': 'update', 'sheet': 'sheet2', 'column': c,
                                 'set': {'link_parent': 'sheet1:' + c}}})
    steps += [
        {'held': {'op': 'new', 'as': 'r1', 'sheet': 'sheet1'}},
        {'held': {'op': 'set', 'as': 'r1', 'values': {
            'daterange1': ['2010-10-10', '2012-10-10'], 'enum1': [7], 'curval1': [1]}}},
        {'held': {'op': 'write', 'as': 'r1'}},
        {'held': {'op': 'new', 'as': 'r2', 'sheet': 'sheet2'}},
        {'held': {'op': 'set', 'as': 'r2', 'values': {
            'daterange1': ['2010-10-15', '2013-10-10'], 'enum1': [14], 'curval1': [2]}}},
        {'held': {'op': 'write', 'as': 'r2'}},
        # A new record with the date blank, linked to the first sheet's record
        {'held': {'op': 'new', 'as': 'r3', 'sheet': 'sheet2', 'linked_id': '@held:r1'}},
        {'held': {'op': 'set', 'as': 'r3', 'values': {'string1': 'Baz'}}},
        {'held': {'op': 'write', 'as': 'r3'}},
        {'held': {'op': 'write_linked_id', 'as': 'r3', 'linked': '@held:r1'}},
    ]
    for name, rules, sort, values in FILTERS:
        steps.append({'search': {
            'id': name,
            'sheet': 'sheet2',
            'view': {
                'filter': {'rules': rules},
                'columns': COLUMNS,
                'sorts': [{'name': 'daterange1', 'type': sort}],
            },
            'observe': COLUMNS,
            'single': True,
            'expect': {'count': str(len(values)), 'rows': values},
        }})
    steps += [
        {'held': {
            'id': 'retrieve record with linked field by current ID',
            'op': 'find', 'as': 's', 'sheet': 'sheet2', 'current_id': '@held:r3',
            'observe': COLUMNS, 'expect': {'values': LINKED},
        }},
        {'held': {
            'id': 'retrieve record with linked field by record ID',
            'op': 'find', 'as': 's', 'clear': True, 'record_id': '@held_record:r3',
            'observe': COLUMNS, 'expect': {'values': LINKED},
        }},
    ]
    return {
        '_generator': GEN,
        'name': '004_multisheet_mv%d%d' % (mv1, mv2),
        'source': ['t/004_multisheet.t'],
        'description': 'Values from linked records in another sheet (multivalue sheet1=%d sheet2=%d)' % (mv1, mv2),
        'steps': steps,
    }


def main():
    for mv1, mv2 in ((0, 0), (0, 1), (1, 0), (1, 1)):
        doc = scenario(mv1, mv2)
        write_scenario(os.path.join(OUT, doc['name'] + '.yaml'), doc)


if __name__ == '__main__':
    main()
