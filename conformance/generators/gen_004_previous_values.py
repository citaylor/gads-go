#!/usr/bin/env python3
"""Generate the 004_previous_values scenarios from upstream
t/004_previous_values.t (searching historical values of records)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario, rule  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_004_previous_values.py'

VALUES = [
    dict(field='string', begin_set='Foobar1', begin_string='Foobar1', end_set='Foobar2', end_string='Foobar2'),
    dict(field='enum', begin_set='foo1', begin_string='foo1', end_set=8, end_string='foo2'),
    dict(field='tree', begin_set='tree1', begin_string='tree1', end_set=14, end_string='tree2'),
    dict(field='integer', begin_set=45, begin_string='45', end_set=55, end_string='55'),
    dict(field='date', begin_set='2010-01-02', begin_string='2010-01-02', end_set='2010-03-06', end_string='2010-03-06'),
    dict(field='daterange', begin_set=['2012-04-02', '2012-05-10'], begin_string='2012-04-02 to 2012-05-10',
         end_set=['2012-06-01', '2012-07-10'], end_string='2012-06-01 to 2012-07-10'),
]

TESTS = [
    dict(field='integer1', before=340, after=450, value=420, operator='less', normal=0, previous=1),
    dict(field='integer1', before=700, after=450, value=600, operator='greater', normal=0, previous=1),
    dict(field='integer1', before=340, after=450, value=340, operator='less_or_equal', normal=0, previous=1),
    dict(field='integer1', before=340, after=450, value=340, operator='not_equal', normal=1, previous=0),
    dict(field='integer1', before=None, after=100, operator='is_empty', normal=0, previous=1),
    dict(field='integer1', before=100, after=None, operator='is_not_empty', normal=0, previous=1),
    dict(field='string1', before='apples', after='oranges', value='apples', operator='not_equal', normal=1, previous=0),
    dict(field='string1', before='apples', after='oranges', value='pple', operator='contains', normal=0, previous=1),
    dict(field='string1', before='apples', after='oranges', value='pple', operator='not_contains', normal=1, previous=0),
    dict(field='string1', before='apples', after='oranges', value='appl', operator='not_begins_with', normal=1, previous=0),
    dict(field='string1', before=None, after='Foobar', operator='is_empty', normal=0, previous=1, empty_defined=1),
    dict(field='string1', before='Foobar', after=None, operator='is_not_empty', normal=0, previous=1, empty_defined=1),
    dict(field='string1', before=None, after='Foobar', operator='is_empty', normal=0, previous=1, empty_defined=0),
    dict(field='string1', before='Foobar', after=None, operator='is_not_empty', normal=0, previous=1, empty_defined=0),
    dict(field='enum1', before=[1, 2], after=3, value='foo2', operator='not_equal', normal=1, previous=0),
    # Limiting previous value searches to a timeframe
    dict(field='enum1', before=1, after=3, value='foo1', operator='equal', normal=0, previous=1, last_edited=0),
    dict(field='enum1', before=1, after=3, value='foo1', operator='equal', normal=0, previous=0, last_edited=1),
    dict(field='enum1', before=[1, 2], after=3, value='foo1', operator='not_equal', normal=1, previous=0),
]


def count(id_, sheet, flt, n):
    return {'search': {'id': id_, 'sheet': sheet, 'view': {'name': 'Test view', 'filter': flt},
                       'no_results': True, 'expect': {'count': str(n)}}}


def fields_scenario():
    data1 = {v['field'] + '1': v['begin_set'] for v in VALUES}
    data2 = {v['field'] + '2': v['begin_set'] for v in VALUES}
    cc = {v['field']: 2 for v in VALUES}
    steps = [
        {'sheet': {'as': 'curval', 'instance_id': 2}},
        {'sheet': {'as': 'main', 'instance_id': 1, 'curval': 'curval', 'column_count': cc,
                   'data': [data1, data2]}},
        {'held': {'id': 'initial values record1', 'op': 'single', 'as': 'r1', 'sheet': 'main',
                  'observe': sorted(data1),
                  'expect': {'values': {v['field'] + '1': v['begin_string'] for v in VALUES}}}},
        {'held': {'op': 'set', 'as': 'r1', 'values': {v['field'] + '1': v['end_set'] for v in VALUES}}},
        {'held': {'op': 'write', 'as': 'r1', 'options': {'no_alerts': 1}}},
        {'held': {'id': 'written values', 'op': 'find', 'as': 'r1', 'clear': True, 'current_id': '@held:r1',
                  'observe': sorted(data1),
                  'expect': {'values': {v['field'] + '1': v['end_string'] for v in VALUES}}}},
    ]
    for v in VALUES:
        f = v['field'] + '1'
        steps.append(count('normal search on old value - %s' % v['field'], 'main',
                           {'rules': [rule(f, v['begin_string'])]}, 0))
        steps.append(count('search previous values - %s' % v['field'], 'main',
                           {'rules': [rule(f, v['begin_string'], previous_values='positive')]}, 1))
    return {
        '_generator': GEN,
        'name': '004_previous_values_fields',
        'source': ['t/004_previous_values.t'],
        'description': 'Searching previous values of each field type',
        'steps': steps,
    }


def operators_scenario():
    steps = []
    for i, t in enumerate(TESTS):
        label = '%d %s %s' % (i + 1, t['field'], t['operator'])
        if i:
            steps.append({'reset': {}})
        steps += [
            {'set_time': {'time': '2014-01-01 01:00:00'}},
            {'sheet': {'as': 'main', 'multivalue': 1, 'column_count': {'string': 2},
                       'data': [{t['field']: t['before']}]}},
            {'set_time': {'time': '2014-06-01 01:00:00'}},
            {'record': {'op': 'update', 'sheet': 'main', 'current_id': 1,
                        'values': {t['field']: t['after']}, 'options': {'no_alerts': 1}}},
            {'set_time': {'time': '2015-01-01 01:00:00'}},
            {'record': {'op': 'update', 'sheet': 'main', 'current_id': 1,
                        'values': {'string2': 'Blah'}, 'options': {'no_alerts': 1}}},
        ]
        if 'empty_defined' in t:
            val = '' if t['empty_defined'] else None
            steps.append({'db': {'op': 'update', 'table': 'string', 'where': {'value': [None, '']},
                                 'set': {'value': val, 'value_index': val}}})
        r = rule(t['field'], t.get('value'), t['operator'])
        steps.append(count('normal - %s' % label, 'main', {'rules': [r]}, t['normal']))
        if t.get('last_edited'):
            flt = {'rules': [r, rule('_version_datetime', '2014-03-01', 'greater', type='date')],
                   'previous_values': 'positive'}
        else:
            flt = {'rules': [dict(r, previous_values='positive')]}
        steps.append(count('inc previous - %s' % label, 'main', flt, t['previous']))
    return {
        '_generator': GEN,
        'name': '004_previous_values_operators',
        'source': ['t/004_previous_values.t'],
        'description': 'Operators used when searching previous values',
        'steps': steps,
    }


def vd(op, value):
    return rule('_version_datetime', value, op)


def groups_scenario():
    steps = [
        {'set_time': {'time': '2014-01-01 01:00:00'}},
        {'sheet': {'as': 'main', 'multivalue': 1, 'data': [{'integer1': 10}]}},
        {'set_time': {'time': '2014-09-01 01:00:00'}},
        {'record': {'op': 'update', 'sheet': 'main', 'current_id': 1, 'values': {'string1': 'foobar'},
                    'options': {'no_alerts': 1}}},
        {'set_time': {'time': '2015-01-02 01:00:00'}},
        {'record': {'op': 'update', 'sheet': 'main', 'current_id': 1, 'values': {'integer1': 20},
                    'options': {'no_alerts': 1}}},
        {'set_time': {'time': '2016-01-01 01:00:00'}},
        {'record': {'op': 'update', 'sheet': 'main', 'current_id': 1, 'values': {'integer1': 30},
                    'options': {'no_alerts': 1}}},
    ]
    for test in ('normal', 'inrange', 'outrange'):
        for negative in (0, 1):
            flt = {'rules': [
                rule('integer1', '20', 'not_equal' if negative else 'equal'),
                vd('greater', '2014-10-01' if test == 'inrange' else '2014-06-01'),
                vd('less', '2015-06-01' if test == 'inrange' else '2014-10-01'),
            ], 'operator': 'AND'}
            if test != 'normal':
                flt['previous_values'] = 'positive'
            expected = 1 if test == 'inrange' else 0
            if negative and test != 'normal':
                expected = 0 if expected else 1
            steps.append(count('group include previous (%s) negative %d' % (test, negative), 'main', flt, expected))
    for inrange in (0, 1):
        frm = '2013-06-01' if inrange else '2014-10-01'
        flt = {'rules': [
            {'rules': [rule('integer1', '10'), vd('greater', frm), vd('less', '2015-06-01')],
             'previous_values': 'positive'},
            {'rules': [rule('integer1', '10', 'not_equal'), vd('greater', frm), vd('less', '2015-06-01')],
             'previous_values': 'positive'},
        ], 'operator': 'AND'}
        steps.append(count('value changed in period inrange %d' % inrange, 'main', flt, inrange))
    for match in ('positive', 'negative'):
        flt = {'rules': [
            {'rules': [rule('integer1', '20'), vd('less', '2015-06-01')], 'previous_values': match},
        ]}
        steps.append(count('negative previous value group (%s)' % match, 'main', flt,
                           0 if match == 'negative' else 1))
    for inrange in (0, 1):
        flt = {'rules': [
            {'rules': [rule('integer1', '20'), vd('less', '2014-12-31')], 'previous_values': 'negative'},
            {'rules': [rule('integer1', '20'), vd('greater', '2015-01-01'), vd('less', '2015-12-31')],
             'previous_values': 'positive'},
        ], 'operator': 'AND'}
        steps.append(count('change in period %d' % inrange, 'main', flt, 1))
    return {
        '_generator': GEN,
        'name': '004_previous_values_groups',
        'source': ['t/004_previous_values.t'],
        'description': 'Previous values searches of rule groups limited by edit date',
        'steps': steps,
    }


def main():
    for doc in (fields_scenario(), operators_scenario(), groups_scenario()):
        write_scenario(os.path.join(OUT, doc['name'] + '.yaml'), doc)


if __name__ == '__main__':
    main()
