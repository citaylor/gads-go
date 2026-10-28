#!/usr/bin/env python3
"""Generate the 012_graphs scenarios from upstream t/012_graphs.t.

The main test loops over multivalue 0..1, each with its own database, and
then tests a graph of a large number of records in a new database.
"""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario, rule  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_012_graphs.py'

CURVAL_LABELS = [
    'Foo, 50, foo1, , 2014-10-10, 2012-02-10 to 2013-06-15, , , c_amber, 2012',
    'Bar, 132, foo2, , 2009-01-02, 2008-05-04 to 2008-07-14, , , b_red, 2008',
]


def s(v):
    """Convert expected numbers to strings, as observations are plain."""
    if isinstance(v, list):
        return [s(x) for x in v]
    return str(v)


def data(mv):
    return [
        {
            'string1': 'Foo',
            'date1': '2013-10-10',
            'daterange1': ['2014-03-21', '2015-03-01'],
            'tree1': 'tree1',
            'curval1': 1,
            'integer2': 8,
        }, {
            'string1': ['Bar', 'FooBar'] if mv else 'Bar',
            'date1': '2014-10-10',
            'daterange1': ['2010-01-04', '2011-06-03'],
            'integer1': 150,
            'enum1': 7,
            'tree1': 'tree1',
            'curval1': 2,
            'integer2': 80,
        }, {
            'string1': 'Bar',
            'integer1': 35,
            'enum1': 8,
            'tree1': 'tree1',
            'curval1': 1,
            'integer2': 24,
        }, {
            'string1': 'FooBar',
            'date1': '2016-10-10',
            'daterange1': ['2009-01-04', '2017-06-03'],
            'integer1': 20,
            'enum1': [8, 9] if mv else 8,
            'tree1': 'tree1',
            'curval1': 2,
            'integer2': 13,
        },
    ]


def graphs(mv):
    return [
        dict(name='String x-axis, integer sum y-axis', type='bar',
             x_axis='string1', y_axis='integer1', y_axis_stack='sum',
             data=[[50, 10, 35 if mv else 20]]),
        dict(name='String x-axis, multi-integer sum y-axis', type='bar',
             x_axis='string1', y_axis='calc2', y_axis_stack='sum',
             data=[[96, 24, 63 if mv else 39]], xlabels=['Bar', 'Foo', 'FooBar']),
        dict(name='String x-axis, multi-integer sum y-axis, filtered', type='bar',
             x_axis='string1', y_axis='calc2', y_axis_stack='sum',
             data=[[72, 39]], xlabels=['Bar', 'FooBar'],
             rules=[rule('calc2', '20', 'greater')]),
        dict(name='Integer x-axis, count y-axis', type='bar',
             x_axis='integer2', y_axis='string1', y_axis_stack='count',
             data=[[1, 1, 2]], xlabels=['13', '24', '8']),
        dict(name='String x-axis, integer sum y-axis as percent', type='bar',
             x_axis='string1', y_axis='integer1', y_axis_stack='sum', as_percent=1,
             data=[[53, 11, 37]] if mv else [[63, 13, 25]]),
        dict(name='Pie as percent', type='pie',
             x_axis='string1', y_axis='integer1', y_axis_stack='sum', as_percent=1,
             data=[[['Bar', 53], ['Foo', 11], ['FooBar', 37]]] if mv
             else [[['Bar', 63], ['Foo', 13], ['FooBar', 25]]]),
        dict(name='Pie with blank value', type='pie',
             x_axis='date1', y_axis='string1', y_axis_stack='count',
             data=[[['2013-10-10', 1], ['2014-10-10', 1], ['2016-10-10', 1], ['&lt;no value&gt;', 1]]]),
        dict(name='String x-axis, integer sum y-axis with view filter', type='bar',
             x_axis='string1', y_axis='integer1', y_axis_stack='sum',
             data=[[15, 10, 15]] if mv else [[15, 10]],
             rules=[rule('enum1', 'foo1')]),
        dict(name='Date range x-axis, integer sum y-axis', type='bar',
             x_axis='daterange1', x_axis_grouping='year', y_axis='integer1', y_axis_stack='sum',
             data=[[20, 35, 35, 20, 20, 30, 30, 20, 20]]),
        dict(name='Date range x-axis, integer sum y-axis, limited time period by dates', type='bar',
             x_axis='daterange1', x_axis_grouping='year', y_axis='integer1', y_axis_stack='sum',
             frm='2013-08-15', to='2016-06-15',
             data=[[20, 30, 30, 20]], xlabels=['2013', '2014', '2015', '2016']),
        dict(name='Date range x-axis, integer sum y-axis, limited time period by length', type='bar',
             x_axis='daterange1', x_axis_range=6, y_axis='integer1', y_axis_stack='sum',
             data=[[30, 30, 30, 20, 20, 20, 20]],
             xlabels=['January 2015', 'February 2015', 'March 2015', 'April 2015', 'May 2015', 'June 2015', 'July 2015']),
        dict(name='Date range x-axis, integer sum y-axis, limited time period by length negative', type='bar',
             x_axis='daterange1', x_axis_range=-6, y_axis='integer1', y_axis_stack='sum',
             data=[[30, 30, 30, 30, 30, 30, 30]],
             xlabels=['July 2014', 'August 2014', 'September 2014', 'October 2014', 'November 2014', 'December 2014', 'January 2015']),
        dict(name='Date range x-axis from curval, integer sum y-axis', type='bar',
             x_axis='curval:daterange1', x_axis_link='curval1', x_axis_grouping='year',
             y_axis='integer1', y_axis_stack='sum',
             data=[[35, 0, 0, 0, 45, 45]]),
        dict(name='Date range x-axis from curval, integer sum y-axis, group by curval', type='bar',
             x_axis='curval:daterange1', x_axis_link='curval1', x_axis_grouping='year',
             y_axis='integer1', y_axis_stack='sum', group_by='curval1',
             data=[[0, 0, 0, 0, 45, 45], [35, 0, 0, 0, 0, 0]], labels=CURVAL_LABELS),
        dict(name='Date x-axis, integer count y-axis', type='bar',
             x_axis='date1', x_axis_grouping='year', y_axis='string1', y_axis_stack='count',
             data=[[1, 1, 0, 1]]),
        dict(name='Date x-axis, integer count y-axis, limited range', type='bar',
             x_axis='date1', x_axis_grouping='year', y_axis='string1', y_axis_stack='count',
             frm='2014-01-15', to='2016-12-15',
             data=[[1, 0, 1]], xlabels=['2014', '2015', '2016']),
        dict(name='Date x-axis, integer sum y-axis, limited range by length', type='bar',
             x_axis='date1', x_axis_range=120, y_axis='integer1', y_axis_stack='sum',
             data=[[0, 20, 0, 0, 0, 0, 0, 0, 0, 0, 0]],
             xlabels=[str(y) for y in range(2015, 2026)]),
        dict(name='Date x-axis, integer sum y-axis, limited range by length, grouped', type='bar',
             x_axis='date1', x_axis_range=-120, y_axis='integer1', y_axis_stack='sum', group_by='enum1',
             data=[[0, 0, 0, 0, 0, 0, 0, 0, 10, 15, 0]],
             xlabels=[str(y) for y in range(2005, 2016)], labels=['foo1']),
        dict(name='Date x-axis, integer sum y-axis, limited range by dates, grouped', type='bar',
             x_axis='date1', x_axis_grouping='year', x_axis_range='custom',
             frm='2012-01-15', to='2018-12-15',
             y_axis='integer1', y_axis_stack='sum', group_by='enum1',
             data=[[0, 0, 0, 0, 20, 0, 0], [0, 0, 0, 0, 20, 0, 0], [0, 10, 15, 0, 0, 0, 0]] if mv
             else [[0, 0, 0, 0, 20, 0, 0], [0, 10, 15, 0, 0, 0, 0]],
             xlabels=[str(y) for y in range(2012, 2019)],
             labels=['foo3', 'foo2', 'foo1'] if mv else ['foo2', 'foo1']),
        dict(name='String x-axis, sum y-axis, group by enum', type='bar',
             x_axis='string1', y_axis='integer1', y_axis_stack='sum', group_by='enum1',
             data=[[0, 0, 20], [35, 0, 20], [15, 10, 15]] if mv else [[35, 0, 20], [15, 10, 0]]),
        dict(name='String x-axis, sum y-axis, group by enum as percent', type='bar',
             x_axis='string1', y_axis='integer1', y_axis_stack='sum', group_by='enum1', as_percent=1,
             data=[[0, 0, 36], [70, 0, 36], [30, 100, 27]] if mv else [[70, 0, 100], [30, 100, 0]]),
        dict(name='Filter on multi-value enum', type='bar',
             x_axis='string1', y_axis='integer1', y_axis_stack='count',
             data=[[1, 1]],
             rules=[rule('enum1', 'foo2'), rule('enum1', 'foo3')], condition='OR'),
        dict(name='Curval on x-axis', type='bar',
             x_axis='curval1', y_axis='integer1', y_axis_stack='sum',
             data=[[35, 45]]),
        dict(name='Field from curval on x-axis', type='bar',
             x_axis='curval:string1', x_axis_link='curval1', y_axis='integer1', y_axis_stack='sum',
             data=[[35, 45]], xlabels=['Bar', 'Foo']),
        dict(name='Enum field from curval on x-axis', type='bar',
             x_axis='curval:enum1', x_axis_link='curval1', y_axis='integer1', y_axis_stack='sum',
             data=[[45, 35]], xlabels=['foo1', 'foo2']),
        dict(name='Enum field from curval on x-axis, enum on y, with filter', type='bar',
             x_axis='curval:enum1', x_axis_link='curval1', y_axis='tree1', y_axis_stack='count',
             data=[[1, 1]], xlabels=['foo1', 'foo2'],
             rules=[rule('enum1', 'foo1')]),
        dict(name='Enum field from curval on x-axis, with filter on whole curval', type='bar',
             x_axis='curval:enum1', x_axis_link='curval1', y_axis='tree1', y_axis_stack='count',
             data=[[2]], xlabels=['foo1'],
             rules=[rule('curval1', '1')]),
        dict(name='Curval on x-axis grouped by enum', type='bar',
             x_axis='curval1', y_axis='integer1', y_axis_stack='sum', group_by='enum1',
             data=[[20, 0], [20, 35], [15, 10]] if mv else [[20, 35], [15, 10]]),
        dict(name='Enum on x-axis, filter by enum', type='bar',
             x_axis='enum1', y_axis='string1', y_axis_stack='count',
             data=[[2, 2, 1]] if mv else [[2, 2]],
             rules=[rule('tree1', 'tree1')]),
        dict(name='Curval on x-axis, filter by enum', type='bar',
             x_axis='curval1', y_axis='string1', y_axis_stack='count',
             data=[[1, 1]],
             rules=[rule('enum1', 'foo1')]),
        dict(name='Graph grouped by curvals', type='bar',
             x_axis='string1', y_axis='integer1', y_axis_stack='sum', group_by='curval1',
             data=[[35, 10, 0], [15, 0, 35]] if mv else [[35, 10, 0], [15, 0, 20]],
             labels=CURVAL_LABELS),
        dict(name='Linked value on x-axis, count', type='bar',
             x_axis='integer1', y_axis='string1', y_axis_stack='count',
             data=[[1, 1, 1, 1]], xlabels=['10', '15', '20', '35']),
        dict(name='Linked value on x-axis (multiple linked), count', type='bar',
             x_axis='integer1', y_axis='string1', y_axis_stack='count',
             data=[[1, 1, 1, 1]], xlabels=['10', '20', '35', '55'], child2=55),
        dict(name='Linked value on x-axis (same value in normal/linked), sum', type='bar',
             x_axis='integer1', y_axis='calc1', y_axis_stack='sum',
             data=[[4024, 2009, 0]], xlabels=['15', '20', '35'], child=15),
        dict(name='All columns x-axis, sum y-axis', type='bar',
             x_axis=None, y_axis='integer1', y_axis_stack='sum',
             data=[[25]], view_columns=['integer1'],
             rules=[rule('enum1', 'foo1')]),
        dict(name='String on x-axis, group by autocur', type='bar',
             x_axis='string1', y_axis_stack='count', group_by='autocur1',
             data=[[2, 0], [0, 1], [1, 1]] if mv else [[1, 0], [0, 1], [1, 1]],
             sheet='curval', labels=['FooBar', 'Foo', 'Bar'], xlabels=['Bar', 'Foo']),
        dict(name='Autocur on x-axis', type='bar',
             x_axis='autocur1', y_axis_stack='count',
             data=[[2, 1, 2]] if mv else [[2, 1, 1]],
             sheet='curval', xlabels=['Bar', 'Foo', 'FooBar']),
    ]


def scenario(mv):
    steps = [
        {'sheet': {'as': 'curval', 'instance_id': 2, 'multivalue': mv}},
        # Edit to a curval record, to make sure that only the latest version
        # is used in the graphs
        {'held': {'op': 'find', 'as': 'cr', 'sheet': 'curval', 'current_id': 2}},
        {'held': {'op': 'set', 'as': 'cr', 'values': {'integer1': 132}}},
        {'held': {'op': 'write', 'as': 'cr', 'no_alerts': True}},
        {'sheet': {
            'as': 'main',
            'data': data(mv),
            'curval': 'curval',
            'multivalue': mv,
            'column_count': {'integer': 2},
            'multivalue_columns': ['enum', 'string'],
        }},
        {'add_autocur': {
            'sheet': 'curval',
            'refers_to': 'main',
            'related_field': 'curval1',
            'curval_fields': ['string1'],
        }},
        {'column': {
            'sheet': 'main',
            'type': 'calc',
            'name': 'calc2',
            'return_type': 'integer',
            'code': 'function evaluate (L1integer2) \n return {L1integer2, L1integer2 * 2} \nend',
            'multivalue': 1,
        }},
        {'held': {'op': 'find', 'as': 'r', 'sheet': 'main', 'current_id': 4}},
        {'held': {'op': 'set', 'as': 'r', 'values': {'integer1': 15, 'integer2': 8}}},
        {'held': {'op': 'write', 'as': 'r', 'no_alerts': True}},
        # Linked record sheet, which will contain the integer1 value for the
        # first record of the first sheet
        {'sheet': {'as': 'other', 'instance_id': 3, 'data': []}},
        # The column cache of existing layouts does not include the new sheet
        {'clear_layouts': {}},
        {'column': {'op': 'update', 'sheet': 'main', 'column': 'integer1', 'set': {'link_parent': 'other:integer1'}}},
        {'column': {'op': 'update', 'sheet': 'main', 'column': 'enum1', 'set': {'link_parent': 'other:enum1'}}},
        {'held': {'op': 'new', 'as': 'child', 'sheet': 'other'}},
        {'held': {'op': 'set', 'as': 'child', 'values': {'integer1': 10, 'enum1': 'foo1'}}},
        {'held': {'op': 'write', 'as': 'child', 'no_alerts': True}},
        {'held': {'op': 'single', 'as': 'parent', 'sheet': 'main'}},
        {'held': {'op': 'write_linked_id', 'as': 'parent', 'linked': '@held:child'}},
    ]
    child_value = 10
    for g in graphs(mv):
        want_child = g.get('child', 10)
        if want_child != child_value:
            steps.append({'held': {'op': 'set', 'as': 'child', 'values': {'integer1': want_child}}})
            steps.append({'held': {'op': 'write', 'as': 'child', 'no_alerts': True}})
            child_value = want_child
        if g.get('child2'):
            steps += [
                {'held': {'op': 'new', 'as': 'child2', 'sheet': 'other'}},
                {'held': {'op': 'set', 'as': 'child2', 'values': {'integer1': g['child2']}}},
                {'held': {'op': 'write', 'as': 'child2', 'no_alerts': True}},
                {'held': {'op': 'find', 'as': 'parent2', 'sheet': 'main', 'current_id': 4, 'clear': True}},
                {'held': {'op': 'write_linked_id', 'as': 'parent2', 'linked': '@held:child2'}},
            ]
        step = {
            'id': g['name'],
            'sheet': g.get('sheet', 'main'),
            'title': g['name'],
            'type': g['type'],
            'x_axis': g['x_axis'],
            'y_axis': g.get('y_axis'),
            'y_axis_stack': g['y_axis_stack'],
        }
        for k in ('x_axis_link', 'x_axis_grouping', 'x_axis_range', 'group_by', 'as_percent'):
            if k in g:
                step[k] = g[k]
        if 'frm' in g:
            step['from'] = g['frm']
            step['to'] = g['to']
        if 'rules' in g:
            step['view'] = {'filter': {'rules': g['rules'], 'condition': g.get('condition', 'AND')}}
            step['columns'] = g.get('view_columns', [])
        expect = {'points': s(g['data'])}
        if 'xlabels' in g:
            expect['xlabels'] = g['xlabels']
        if 'labels' in g:
            expect['labels'] = g['labels']
        step['expect'] = expect
        steps.append({'graph': step})
        if g.get('child2'):
            steps += [
                {'held': {'op': 'write_linked_id', 'as': 'parent2', 'linked': None}},
                {'held': {'op': 'purge_version', 'as': 'parent2'}},
                {'held': {'op': 'delete', 'as': 'child2'}},
                {'held': {'op': 'purge', 'as': 'child2'}},
            ]
    return {
        '_generator': GEN,
        'name': '012_graphs_mv%d' % mv,
        'source': ['t/012_graphs.t'],
        'description': 'Graph data for many graph types (multivalue %d)' % mv,
        'fixed_time': '2015-01-01 12:00:00',
        'steps': steps,
    }


def large():
    return {
        '_generator': GEN,
        'name': '012_graphs_large',
        'source': ['t/012_graphs.t'],
        'description': 'Graph of a large number of records',
        'fixed_time': '2015-01-01 12:00:00',
        'steps': [
            {'sheet': {'as': 'main', 'data': [{'string1': 'foobar', 'integer1': 2} for _ in range(1000)]}},
            {'graph': {
                'id': 'large number of records',
                'sheet': 'main',
                'title': 'Test graph',
                'type': 'bar',
                'x_axis': 'string1',
                'y_axis': 'integer1',
                'y_axis_stack': 'sum',
                'expect': {'points': [['2000']]},
            }},
        ],
    }


def main():
    for mv in (0, 1):
        doc = scenario(mv)
        write_scenario(os.path.join(OUT, doc['name'] + '.yaml'), doc)
    doc = large()
    write_scenario(os.path.join(OUT, doc['name'] + '.yaml'), doc)


if __name__ == '__main__':
    main()
