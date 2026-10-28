#!/usr/bin/env python3
"""Generate the 012_graph_historical scenario from upstream
t/012_graph_historical.t (historical trend graphs)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario, rule  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_012_graph_historical.py'

# (created, enum1, integer1, integer1 of the curval record). GADS-dev: the
# main sheet has a curval, each version also writing a version of its
# curval record, and the test loops over multivalue 0..1
RECORDS = [
    [
        ('2018-02-01 11:00:00', 7, 12, 110),
        ('2018-10-01 12:00:00', 8, 1, 125),
        ('2019-03-15 18:00:00', 9, 4, 155),
        ('2019-06-14 01:00:00', 8, 10, 140),
    ],
    [
        ('2018-06-20 20:00:00', 8, 23, 280),
        ('2018-07-31 00:00:00', 9, 5, 220),
        ('2018-11-12 23:59:00', 7, 7, 245),
        ('2019-04-01 14:50:00', 7, 19, 265),
    ],
    [
        ('2017-05-13 16:00:00', 9, 1, 310),
        ('2018-10-01 19:00:00', 7, 0, 330),
        ('2019-06-02 22:00:00', 9, 54, 365),
        ('2019-06-02 23:00:00', 7, 37, 380),
    ],
]

MONTHS_12 = [
    'June 2018', 'July 2018', 'August 2018', 'September 2018',
    'October 2018', 'November 2018', 'December 2018', 'January 2019',
    'February 2019', 'March 2019', 'April 2019', 'May 2019', 'June 2019',
]

DAYS = ['%02d May 2019' % d for d in range(14, 32)] + ['%02d June 2019' % d for d in range(1, 15)]

GRAPHS = [
    dict(name='String x-axis with count - standard 12 month range',
         x_axis='enum1', x_axis_range='-12', y_axis_stack='count',
         labels=['foo3', 'foo2', 'foo1'],
         data=[
             [1, 2, 2, 2, 1, 0, 0, 0, 0, 1, 1, 1, 0],
             [1, 0, 0, 0, 1, 1, 1, 1, 1, 0, 0, 0, 1],
             [1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2],
         ], xlabels=MONTHS_12),
    dict(name='Including quick search',
         x_axis='enum1', x_axis_range='-12', y_axis_stack='count',
         labels=['foo3', 'foo2', 'foo1'], search='foo2',
         data=[
             [0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 0],
             [0, 0, 0, 0, 1, 1, 1, 1, 1, 0, 0, 0, 1],
             [1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0],
         ], xlabels=MONTHS_12),
    dict(name='Quick search with curval value',
         x_axis='enum1', x_axis_range='-12', y_axis='cv:integer1', y_axis_link='curval1', y_axis_stack='sum',
         labels=['foo3', 'foo2', 'foo1'], search='foo1',
         data=[
             [310, 530, 530, 530, 220, 0, 0, 0, 0, 0, 0, 0, 0],
             [280, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
             [0, 0, 0, 0, 330, 575, 575, 575, 575, 575, 595, 595, 645],
         ], xlabels=MONTHS_12),
    # Should use same record IDs for each period
    dict(name='Including filter',
         x_axis='enum1', x_axis_range='-12', y_axis_stack='count',
         labels=['foo3', 'foo2', 'foo1'], view=('integer1', '10'),
         data=[
             [0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 0],
             [0, 0, 0, 0, 1, 1, 1, 1, 1, 0, 0, 0, 1],
             [1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0],
         ], xlabels=MONTHS_12),
    dict(name='Curval values with filter',
         x_axis='enum1', x_axis_range='-12', y_axis='cv:integer1', y_axis_link='curval1', y_axis_stack='sum',
         labels=['foo3', 'foo2', 'foo1'], view=('integer1', '10', 'greater'),
         data=[
             [310, 530, 530, 530, 220, 0, 0, 0, 0, 0, 0, 0, 0],
             [280, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
             [0, 0, 0, 0, 330, 575, 575, 575, 575, 575, 595, 595, 645],
         ], xlabels=MONTHS_12),
    dict(name='String x-axis with y-axis sum - standard 12 month range',
         x_axis='enum1', x_axis_range='-12', y_axis='integer1', y_axis_stack='sum',
         labels=['foo3', 'foo2', 'foo1'],
         data=[
             [1, 6, 6, 6, 5, 0, 0, 0, 0, 4, 4, 4, 0],
             [23, 0, 0, 0, 1, 1, 1, 1, 1, 0, 0, 0, 10],
             [12, 12, 12, 12, 0, 7, 7, 7, 7, 7, 19, 19, 56],
         ], xlabels=MONTHS_12),
    dict(name='String x-axis with y-axis sum - 1 month range',
         x_axis='enum1', x_axis_range='-1', y_axis='integer1', y_axis_stack='sum',
         labels=['foo3', 'foo2', 'foo1'],
         data=[
             [4] * 31 + [0],
             [0] * 31 + [10],
             [19] * 19 + [56] * 13,
         ], xlabels=DAYS),
    dict(name='String x-axis with y-axis count - custom range',
         x_axis='enum1', x_axis_range='custom', x_axis_grouping='month',
         frm='2018-01-05', to='2018-03-10',
         y_axis='integer1', y_axis_stack='count',
         labels=['foo3', 'foo1'],
         data=[[1, 1, 1], [0, 1, 1]],
         xlabels=['January 2018', 'February 2018', 'March 2018']),
]


def s(v):
    if isinstance(v, list):
        return [s(x) for x in v]
    return str(v)


def scenario(mv):
    steps = [
        {'sheet': {'as': 'cv', 'instance_id': 2, 'multivalue': mv, 'data': []}},
        {'sheet': {'as': 'main', 'data': [], 'multivalue': mv, 'curval': 'cv', 'curval_fields': ['string1']}},
        {'column': {'op': 'update', 'sheet': 'main', 'column': 'curval1',
                    'set': {'show_add': 1, 'value_selector': 'noshow'}, 'write_options': {'no_alerts': 1}}},
    ]
    for i, rec in enumerate(RECORDS):
        name = 'r%d' % i
        cname = 'c%d' % i
        steps.append({'held': {'op': 'new', 'as': name, 'sheet': 'main'}})
        steps.append({'held': {'op': 'new', 'as': cname, 'sheet': 'cv'}})
        for created, enum, integer, cint in rec:
            steps.append({'held': {'op': 'set', 'as': cname, 'values': {'integer1': cint}}})
            steps.append({'held': {'op': 'write', 'as': cname,
                                   'options': {'version_datetime': created, 'no_alerts': 1}}})
            steps.append({'held': {'op': 'set', 'as': name, 'values': {
                'enum1': enum, 'integer1': integer, 'curval1': [1 + 2 * i]}}})  # curval and main records interleave
            steps.append({'held': {'op': 'write', 'as': name,
                                   'options': {'version_datetime': created, 'no_alerts': 1}}})
    steps.append({'db': {'id': 'number of records', 'op': 'count', 'table': 'current',
                         'where': {'instance_id': 1}, 'expect': {'count': '3'}}})
    steps.append({'db': {'id': 'number of versions', 'op': 'count', 'table': 'record',
                         'where': {'current_id': [2, 4, 6]}, 'expect': {'count': '12'}}})
    for g in GRAPHS:
        step = {
            'id': g['name'],
            'sheet': 'main',
            'title': g['name'],
            'type': 'bar',
            'trend': 'aggregate',
            'x_axis_range': g['x_axis_range'],
            'x_axis': g['x_axis'],
            'y_axis': g.get('y_axis'),
            'y_axis_stack': g['y_axis_stack'],
        }
        if 'y_axis_link' in g:
            step['y_axis_link'] = g['y_axis_link']
        if 'x_axis_grouping' in g:
            step['x_axis_grouping'] = g['x_axis_grouping']
        if 'frm' in g:
            step['from'] = g['frm']
            step['to'] = g['to']
        if 'search' in g:
            step['search'] = g['search']
        if 'view' in g:
            col, value = g['view'][:2]
            op = g['view'][2] if len(g['view']) > 2 else 'equal'
            step['view'] = {'filter': {'rules': [rule(col, value, op)]}}
            step['columns'] = ['integer1']
        step['expect'] = {'points': s(g['data']), 'xlabels': g['xlabels'], 'labels': g['labels']}
        steps.append({'graph': step})
    return {
        '_generator': GEN,
        'name': '012_graph_historical_mv%d' % mv,
        'source': ['t/012_graph_historical.t'],
        'description': 'Historical trend graphs (multivalue %d)' % mv,
        'fixed_time': '2019-06-14 01:00:00',
        'steps': steps,
    }


def main():
    for mv in (0, 1):
        doc = scenario(mv)
        write_scenario(os.path.join(OUT, doc['name'] + '.yaml'), doc)


if __name__ == '__main__':
    main()
