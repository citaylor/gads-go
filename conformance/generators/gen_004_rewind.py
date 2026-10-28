#!/usr/bin/env python3
"""Generate the 004_rewind scenarios from upstream t/004_rewind.t.

The test loops over 4 multivalue settings, each with its own database, so
each becomes a separate scenario:

    multivalue 0 - neither
    multivalue 1 - main only
    multivalue 2 - curval only
    multivalue 3 - both
"""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_004_rewind.py'

CURVAL_DATA = [{'string1': 'Bar%d' % i} for i in range(1, 5)]
# All fields, which span multiple database fetches (GADS-dev), with a blank
# curval to check a subrecord that is completely missing
DATA = [{'string1': 'Foo1', 'integer1': 10, 'enum1': 7, 'tree1': 10, 'curval1': None,
         'date1': '2024-10-10', 'daterange1': ['2025-02-10', '2025-06-15'], 'person1': 1}]
OBSERVE = ['string1', 'integer1', 'enum1', 'tree1', 'curval1', 'date1', 'daterange1', 'person1']
V2014 = {'string1': 'Foo1', 'integer1': '10', 'enum1': 'foo1', 'tree1': 'tree1', 'curval1': '',
         'date1': '2024-10-10', 'daterange1': '2025-02-10 to 2025-06-15', 'person1': 'User1, User1'}
V2015 = {'string1': 'Foo2', 'integer1': '20', 'enum1': 'foo2', 'tree1': 'tree2', 'curval1': 'Bar2',
         'date1': '2024-11-10', 'daterange1': '2025-03-10 to 2025-07-15', 'person1': 'User2, User2'}
V2016 = {'string1': 'Foo3', 'integer1': '30', 'enum1': 'foo3', 'tree1': 'tree3', 'curval1': 'Bar3',
         'date1': '2024-12-10', 'daterange1': '2025-04-10 to 2025-08-15', 'person1': 'User3, User3'}

PREVIOUS_2014 = '2015-01-01 12:00:00'
PREVIOUS_2015 = '2016-01-01 12:00:00'


def scenario(mv):
    steps = [
        {'sheet': {
            'as': 'curval',
            'instance_id': 2,
            'multivalue': 1 if mv >= 2 else 0,
            'data': CURVAL_DATA,
        }},
        {'sheet': {
            'as': 'main',
            'data': DATA,
            'multivalue': 1 if mv in (1, 3) else 0,
            'curval': 'curval',
            'curval_fields': ['string1'],
        }},
        {'column': {'op': 'update', 'sheet': 'main', 'column': 'integer1', 'set': {'aggregate': 'sum'}}},
        # Historical view of only the one record, with Records and Record
        {'search': {
            'id': 'initial previous date (2014)',
            'sheet': 'main',
            'rewind': PREVIOUS_2014,
            'observe': OBSERVE,
            'expect': {'count': '1', 'rows': [V2014]},
        }},
        {'held': {
            'id': 'initial single previous date (2014)',
            'op': 'find', 'as': 'i', 'sheet': 'main', 'current_id': 5, 'rewind': PREVIOUS_2014,
            'observe': OBSERVE, 'expect': {'values': V2014},
        }},
        {'search': {
            'id': 'count on initial creation',
            'sheet': 'main',
            'no_results': True,
            'expect': {'count': '1'},
        }},
        {'held': {'op': 'single', 'as': 'r', 'sheet': 'main'}},
        # Make 2 further writes for subsequent 2 years
        {'set_time': {'time': '2015-10-10 01:00:00'}},
        {'held': {'op': 'set', 'as': 'r', 'values': {'string1': 'Foo2', 'integer1': '20', 'enum1': '8', 'tree1': '11', 'curval1': '2',
                                                         'date1': '2024-11-10', 'daterange1': ['2025-03-10', '2025-07-15'],
                                                         'person1': '2'}}},
        {'held': {'op': 'write', 'as': 'r'}},
        {'set_time': {'time': '2016-10-10 01:00:00'}},
        {'held': {'op': 'set', 'as': 'r', 'values': {'string1': 'Foo3', 'integer1': '30', 'enum1': '9', 'tree1': '12', 'curval1': '3',
                                                         'date1': '2024-12-10', 'daterange1': ['2025-04-10', '2025-08-15'],
                                                         'person1': '3'}}},
        {'held': {'op': 'write', 'as': 'r'}},
        # Update to the early curval record, should not be seen in the tests
        # with the dates used
        {'held': {'op': 'find', 'as': 'c', 'sheet': 'main', 'current_id': 1}},
        {'held': {'op': 'set', 'as': 'c', 'values': {'curval:string1': 'Bar1a'}}},
        {'held': {'op': 'write', 'as': 'c', 'no_alerts': True}},
        # And a new record for the third year
        {'held': {'op': 'remove_id', 'as': 'r'}},
        {'held': {'op': 'set', 'as': 'r', 'values': {'string1': 'Foo10', 'integer1': '100', 'enum1': '8', 'tree1': '10', 'curval1': '4',
                                                         'date1': '2024-12-15', 'daterange1': ['2025-05-10', '2025-09-15'],
                                                         'person1': '4'}}},
        {'held': {'op': 'write', 'as': 'r'}},
        {'search': {
            'id': 'count for today after second write',
            'sheet': 'main',
            'no_results': True,
            'expect': {'count': '2'},
        }},
        {'search': {
            'id': 'previous date (2014)',
            'sheet': 'main',
            'rewind': PREVIOUS_2014,
            'observe': OBSERVE,
            'aggregate': ['integer1'],
            'expect': {'count': '1', 'rows': [V2014], 'aggregate': {'integer1': '10'}},
        }},
        {'search': {
            'id': 'previous date (2015)',
            'sheet': 'main',
            'rewind': PREVIOUS_2015,
            'observe': OBSERVE,
            'aggregate': ['integer1'],
            'expect': {'count': '1', 'rows': [V2015], 'aggregate': {'integer1': '20'}},
        }},
        {'search': {
            'id': 'current date',
            'sheet': 'main',
            'observe': OBSERVE,
            'aggregate': ['integer1'],
            'expect': {'count': '2', 'aggregate': {'integer1': '130'}},
            'expect_first': V2016,
        }},
        {'held': {
            'id': 'single retrieve current',
            'op': 'find', 'as': 's', 'sheet': 'main', 'current_id': 5,
            'observe': OBSERVE, 'versions': True,
            'expect': {
                'values': V2016,
                'versions': ['2016-10-10', '2015-10-10', '2014-10-10'],
            },
        }},
        {'held': {
            'id': 'first version with rewind',
            'op': 'find', 'as': 'h', 'sheet': 'main', 'record_id': 5, 'rewind': PREVIOUS_2015,
            'observe': OBSERVE, 'versions': True,
            'expect': {
                'values': V2014,
                'versions': ['2015-10-10', '2014-10-10'],
            },
        }},
        {'held': {
            'id': 'second version with rewind',
            'op': 'find', 'as': 'h', 'record_id': 6, 'clear': True,
            'observe': OBSERVE,
            'expect': {'values': V2015},
        }},
        {'held': {
            'id': 'cannot retrieve version after rewind',
            'op': 'find', 'as': 'h', 'record_id': 7, 'clear': True,
            'expect_error': 'Requested record not found',
        }},
        {'held': {
            'id': 'current version with rewind',
            'op': 'find', 'as': 'h', 'current_id': 5, 'clear': True,
            'observe': OBSERVE,
            'expect': {'values': V2015},
        }},
        {'held': {'op': 'set', 'as': 'h', 'values': {'string1': 'Bar'}}},
        {'held': {
            'id': 'unable to write historic retrieval',
            'op': 'write', 'as': 'h',
            'expect_error': '.',
        }},
        {'graph': {
            'id': 'graph for current date',
            'sheet': 'main',
            'as': 'rewind',
            'title': 'Rewind graph',
            'type': 'bar',
            'x_axis': 'string1',
            'y_axis': 'integer1',
            'y_axis_stack': 'sum',
            'expect': {'xlabels': ['Foo10', 'Foo3'], 'points': [['100', '30']]},
        }},
        {'graph': {
            'id': 'graph for previous date',
            'sheet': 'main',
            'graph_ref': 'rewind',
            'rewind': PREVIOUS_2015,
            'expect': {'xlabels': ['Foo2'], 'points': [['20']]},
        }},
    ]
    return {
        '_generator': GEN,
        'name': '004_rewind_mv%d' % mv,
        'source': ['t/004_rewind.t'],
        'description': 'Rewind of records, versions and graphs (multivalue setting %d)' % mv,
        'fixed_time': '2014-10-10 01:00:00',
        'steps': steps,
    }


def main():
    for mv in range(4):
        doc = scenario(mv)
        write_scenario(os.path.join(OUT, doc['name'] + '.yaml'), doc)


if __name__ == '__main__':
    main()
