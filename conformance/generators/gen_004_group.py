#!/usr/bin/env python3
"""Generate the 004_group scenarios from upstream t/004_group.t (grouped
views). The main test loops over multivalue 0..1; the remaining blocks each
use their own database."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_004_group.py'

ALL = ['string1', 'integer1', 'enum1', 'tree1', 'date1', 'daterange1', 'file1',
       'person1', 'curval1', 'curval2', 'rag1', 'calc1']
NON_NUMERIC = ['string1', 'enum1', 'tree1', 'date1', 'daterange1', 'file1',
               'person1', 'curval1', 'curval2', 'rag1']
VIEW_COLS = ['string1', 'integer1', 'calc1', 'date1', 'daterange1', 'enum1', 'tree1', 'curval1', 'curval2']

# The first value in ascending order of each column (GADS-dev sorts the
# grouped view by the grouped column)
FIRST_ROW = dict(string1='foo1', integer1='25', enum1='foo1', tree1='tree2',
                 date1='2008-10-10', rag1='b_red', calc1='50', curval1='Bar', curval2='Bar',
                 daterange1='2000-01-02 to 2001-03-03', person1='User1, User1')


def data(mv):
    return [
        dict(string1='foo1', integer1=25, date1='2011-10-10', daterange1=['2000-01-02', '2001-03-03'],
             enum1=8, tree1=12, curval1=1, curval2=2, person1=1),
        dict(string1='foo1', integer1=50, date1='2012-10-10', daterange1=['2004-01-02', '2005-03-03'],
             enum1=[7, 9] if mv else 7, tree1=12, curval1=1, curval2=2, person1=1),
        dict(string1='foo2', integer1=60, date1='2009-10-10', daterange1=['2007-01-02', '2007-03-03'],
             enum1=8, tree1=11, curval1=2, curval2=1, person1=1),
        dict(string1='foo2', integer1=70, date1='2008-10-10', daterange1=['2001-01-02', '2001-03-03'],
             enum1=8, tree1=11, curval1=2, curval2=1, person1=1),
    ]


def expected(mv):
    return [
        dict(string1='foo1', integer1='75', calc1='150', date1='2 unique', daterange1='2 unique',
             enum1='3 unique' if mv else '2 unique', tree1='1 unique', curval1='1 unique', curval2='1 unique'),
        dict(string1='foo2', integer1='130', calc1='260', date1='2 unique', daterange1='2 unique',
             enum1='1 unique', tree1='1 unique', curval1='1 unique', curval2='1 unique'),
    ]


def main_scenario(mv):
    steps = [
        {'sheet': {'as': 'curval', 'instance_id': 2, 'multivalue': mv}},
        {'sheet': {'as': 'main', 'data': data(mv), 'curval': 'curval', 'curval_fields': ['string1'],
                   # 2 curvals to test child fields of the same parent
                   'column_count': {'curval': 2},
                   'multivalue': mv,
                   'calc_code': "function evaluate (L1integer1) \n return L1integer1[1] * 2 \n end" if mv
                   else "function evaluate (L1integer1) \n return L1integer1 * 2 \n end"}},
    ]
    for c in NON_NUMERIC:
        steps.append({'column': {'op': 'update', 'sheet': 'main', 'column': c, 'set': {'group_display': 'unique'}}})
    steps += [
        {'add_autocur': {'sheet': 'curval', 'refers_to': 'main', 'related_field': 'curval1',
                         'curval_fields': ['string1']}},
        {'search': {
            'id': 'group by string',
            'sheet': 'main',
            # A sort with an actual value, not a unique count
            'view': {'columns': VIEW_COLS, 'groups': ['string1'], 'sorts': [{'name': 'integer1', 'type': 'asc'}]},
            'observe': VIEW_COLS,
            'group_counts': True,
            'expect': {'rows': expected(mv), 'id_counts': ['2', '2']},
        }},
        # Grouped column removed from view still gets added
        {'search': {
            'id': 'grouped column not in view',
            'sheet': 'main',
            'view': {'columns': ['integer1'], 'groups': ['string1'], 'sorts': [{'name': 'string1', 'type': 'asc'}]},
            'observe': ['string1', 'integer1'],
            'expect': {'rows': [{k: e[k] for k in ('string1', 'integer1')} for e in expected(mv)]},
        }},
    ]
    # Grouping on all fields for completeness
    for c in ALL:
        if c == 'file1':
            continue
        steps.append({'search': {
            'id': 'group by %s' % c,
            'sheet': 'main',
            'view': {'columns': [c], 'groups': [c], 'sorts': [{'name': c, 'type': 'asc'}]},
            'observe': [c],
            'expect_first': {c: FIRST_ROW[c]},
        }})
    steps += [
        # Remove permissions and check grouped column not in view (the view
        # was written while the column could still be read)
        {'view': {'op': 'create', 'sheet': 'main', 'as': 'grouped', 'name': 'Group view',
                  'columns': ['integer1'], 'groups': ['string1'], 'sorts': [{'name': 'enum1', 'type': 'asc'}]}},
        {'column': {'op': 'update', 'sheet': 'main', 'column': 'string1', 'set': {'permissions': {'default': []}}}},
        {'search': {
            'id': 'grouped column without permission',
            'sheet': 'main',
            'view_ref': 'grouped',
            'columns_render': True,
            'expect': {'count': '4', 'ids': [None, None], 'columns_render': ['integer1']},
        }},
        {'column': {'op': 'update', 'sheet': 'curval', 'column': 'autocur1', 'set': {'group_display': 'unique'}}},
        {'search': {
            'id': 'group by string with autocur',
            'sheet': 'curval',
            'view': {'columns': ['autocur1'], 'groups': ['string1']},
            'observe': ['autocur1'],
            'expect': {'rows': [{'autocur1': '2 unique'}, {'autocur1': '2 unique'}]},
        }},
        {'search': {
            'id': 'group by curval',
            'sheet': 'main',
            'view': {'columns': ['integer1'], 'groups': ['curval1']},
            'observe': ['integer1'],
            'expect': {'rows': [{'integer1': '75'}, {'integer1': '130'}]},
        }},
        {'search': {
            'id': 'group by curval subfield',
            'sheet': 'main',
            'view': {'columns': ['curval1', 'integer1'], 'groups': ['curval1_string1', 'curval2_string1'],
                     'sorts': [{'name': 'curval2_string1', 'type': 'desc'}]},
            'observe': ['integer1'],
            'expect': {'rows': [{'integer1': '130'}, {'integer1': '75'}]},
        }},
        # Sorting directly on the Records object: a subfield without its
        # parent errors, with its parent sorts as the view
        {'search': {
            'id': 'sort subfield without parent',
            'sheet': 'main',
            'view': {'columns': ['curval1', 'integer1'], 'groups': ['curval1_string1', 'curval2_string1'],
                     'sorts': [{'name': 'curval2_string1', 'type': 'desc'}]},
            'sort': [{'sheet': 'curval', 'name': 'string1', 'type': 'desc'}],
            'observe': ['integer1'],
            # The runners report the first error caught in the step (as
            # Log::Report), here from resolving the subfield name of the
            # view sort
            'expect_error': r'from different table without parent|Unknown column curval\d_string1',
        }},
        {'search': {
            'id': 'sort subfield with parent',
            'sheet': 'main',
            'view': {'columns': ['curval1', 'integer1'], 'groups': ['curval1_string1', 'curval2_string1'],
                     'sorts': [{'name': 'curval2_string1', 'type': 'desc'}]},
            'sort': [{'sheet': 'curval', 'name': 'string1', 'type': 'desc', 'parent': 'curval2'}],
            'observe': ['integer1'],
            'expect': {'rows': [{'integer1': '130'}, {'integer1': '75'}]},
        }},
    ]
    return {
        '_generator': GEN,
        'name': '004_group_mv%d' % mv,
        'source': ['t/004_group.t'],
        'description': 'Grouped views (multivalue %d)' % mv,
        'steps': steps,
    }


def columns_scenario():
    view = {'columns': ['string1', 'integer1'], 'groups': ['enum1']}
    steps = [
        {'sheet': {'as': 'main'}},
        {'search': {'id': 'columns in group view', 'sheet': 'main', 'view': view, 'columns_render': True,
                    'no_results': True, 'expect': {'columns_render': ['enum1', 'integer1']}}},
        {'search': {'id': 'columns with additional filter', 'sheet': 'main', 'view': view, 'columns_render': True,
                    'no_results': True, 'additional_filters': [{'name': 'string1', 'value': 'Foo'}],
                    'expect': {'columns_render': ['string1', 'integer1', 'enum1']}}},
        {'column': {'op': 'update', 'sheet': 'main', 'column': 'string1', 'set': {'group_display': 'unique'}}},
        {'search': {'id': 'columns with unique count', 'sheet': 'main', 'view': view, 'columns_render': True,
                    'no_results': True, 'expect': {'columns_render': ['enum1', 'string1', 'integer1']}}},
    ]
    return {
        '_generator': GEN,
        'name': '004_group_columns',
        'source': ['t/004_group.t'],
        'description': 'Columns returned for grouped views',
        'steps': steps,
    }


def large_scenario():
    rows = []
    for count in range(1, 301):
        i = int(str(count)[-1])
        rows.append({'string1': 'Foo%d' % i, 'integer1': i * 10})
    return {
        '_generator': GEN,
        'name': '004_group_large',
        'source': ['t/004_group.t'],
        'description': 'Grouped view of more records than a page',
        'steps': [
            {'sheet': {'as': 'main', 'data': rows}},
            {'search': {
                'id': 'group large number of records',
                'sheet': 'main',
                'rows': 50, 'page': 1,
                'view': {'columns': ['string1', 'integer1'], 'groups': ['string1'],
                         'sorts': [{'name': 'string1', 'type': 'asc'}]},
                'observe': ['string1', 'integer1'],
                'group_counts': True,
                'expect': {
                    'pages': '1',
                    'rows': [{'string1': 'Foo%d' % i, 'integer1': str(i * 300)} for i in range(10)],
                    'id_counts': ['30'] * 10,
                },
            }},
        ],
    }


def recalc_scenario():
    return {
        '_generator': GEN,
        'name': '004_group_recalc',
        'source': ['t/004_group.t'],
        'description': 'Recalculated aggregate calc in grouped view',
        'steps': [
            {'sheet': {'as': 'main', 'multivalue': 1, 'column_count': {'integer': 2},
                       'calc_code': "function evaluate (L1integer1, L1integer2) \n return (L1integer1[1] / L1integer2[1]) * 100 \n end",
                       'data': [
                           {'string1': 'foo1', 'integer1': 250, 'integer2': 500},
                           {'string1': 'foo1', 'integer1': 50, 'integer2': 50},
                           {'string1': 'foo2', 'integer1': 3, 'integer2': 4},
                           {'string1': 'foo2', 'integer1': 120, 'integer2': 240},
                       ]}},
            {'column': {'op': 'update', 'sheet': 'main', 'column': 'integer1', 'set': {'aggregate': 'sum'}}},
            {'column': {'op': 'update', 'sheet': 'main', 'column': 'integer2', 'set': {'aggregate': 'sum'}}},
            {'column': {'op': 'update', 'sheet': 'main', 'column': 'calc1', 'set': {'aggregate': 'recalc'}}},
            {'search': {
                'id': 'group with recalc',
                'sheet': 'main',
                'view': {'columns': ['string1', 'calc1'], 'groups': ['string1'],
                         'sorts': [{'name': 'string1', 'type': 'asc'}]},
                'observe': ['string1', 'calc1'],
                'expect': {'rows': [{'string1': 'foo1', 'calc1': '55'}, {'string1': 'foo2', 'calc1': '50'}]},
            }},
        ],
    }


def main():
    docs = [main_scenario(0), main_scenario(1), columns_scenario(), large_scenario(), recalc_scenario()]
    for doc in docs:
        write_scenario(os.path.join(OUT, doc['name'] + '.yaml'), doc)


if __name__ == '__main__':
    main()
