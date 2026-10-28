#!/usr/bin/env python3
"""Generate the 004_aggregate scenarios from upstream t/004_aggregate.t
(aggregate totals of records: sums, recalc aggregates and grouped views, with
and without multivalue fields and filters)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_004_aggregate.py'
CALC = "function evaluate (L1integer1, L1integer2) \n return (L1integer1%s / L1integer2%s) * 100 \n end"


def col(name, **set):
    return {'column': {'op': 'update', 'sheet': 'main', 'column': name, 'set': set}}


def scenario(mv):
    steps = [{'sheet': {
        'as': 'main', 'multivalue': mv, 'column_count': {'integer': 2},
        'calc_code': CALC % (('[1]', '[1]') if mv else ('', '')),
        'data': [
            {'string1': 'foo1', 'integer1': 25, 'integer2': 50, 'enum1': [1, 2] if mv else 1, 'person1': 1},
            {'string1': 'foo1', 'integer1': 50, 'integer2': 500, 'enum1': 2, 'person1': 1},
            {'string1': 'foo2', 'integer1': 60, 'integer2': 60, 'enum1': 2, 'person1': 1},
            {'string1': 'foo2', 'integer1': 70, 'integer2': 35, 'enum1': 3, 'person1': 1},
        ]}}]
    steps.append({'search': {'id': 'no aggregate results initially', 'sheet': 'main',
                             'aggregate': ['integer1', 'calc1'], 'expect': {'count': '4', 'aggregate': None}}})
    steps += [col('integer1', aggregate='sum'), col('calc1', aggregate='sum'), {'clear_layouts': {}}]
    for wf in (0, 1):
        view = {'name': 'Aggregate view', 'columns': ['string1', 'integer1', 'calc1', 'person1'],
                'sorts': [{'name': 'enum1', 'type': 'asc'}]}
        if wf:
            view['filter'] = {'condition': 'OR', 'rules': [
                {'name': 'enum1', 'type': 'string', 'operator': 'equal', 'value': 'foo1'},
                {'name': 'enum1', 'type': 'string', 'operator': 'equal', 'value': 'foo2'},
            ]}
        n = 'filter %d: ' % wf
        steps.append({'search': {'id': n + 'sum aggregates', 'sheet': 'main', 'view': view,
                                 'observe': ['string1', 'integer1', 'calc1', 'person1'],
                                 'aggregate': ['integer1', 'calc1'],
                                 'expect': {'count': '3' if wf else '4',
                                            'aggregate': {'integer1': '135' if wf else '205',
                                                          'calc1': '160' if wf else '360'}}}})
        # Recalc aggregate type, whereby calc values are recalculated based
        # on other aggregate fields. Not all required columns are in the view
        steps += [
            {'column': {'id': n + 'cannot set recalc without all required aggregate fields', 'op': 'update',
                        'sheet': 'main', 'column': 'calc1', 'set': {'aggregate': 'recalc'},
                        'expect_error': 'column integer2 does not have an aggregate defined'}},
            {'clear_layouts': {}},
            col('integer2', aggregate='sum'), {'clear_layouts': {}},
            col('calc1', aggregate='recalc'), {'clear_layouts': {}},
            {'column': {'id': n + 'cannot remove aggregate with recalc in effect', 'op': 'update',
                        'sheet': 'main', 'column': 'integer2', 'set': {'aggregate': ''},
                        'expect_error': 'aggregate on this column cannot be removed'}},
            {'clear_layouts': {}},
            {'search': {'id': n + 'recalc aggregate', 'sheet': 'main', 'view': view,
                        'aggregate': ['integer1', 'integer2', 'calc1'],
                        'expect': {'count': '3' if wf else '4',
                                   'aggregate': {'integer1': '135' if wf else '205',
                                                 'integer2': '610' if wf else '645',
                                                 'calc1': '22' if wf else '32'}}}},
            col('calc1', aggregate='sum'), col('integer2', aggregate=''), {'clear_layouts': {}},
        ]
        # Multivalue field that is grouped: records are counted in each
        # group, but only once for the overall aggregate
        gview = dict(view, groups=['enum1'])
        rows = [{'integer1': '25'}, {'integer1': '135' if mv else '110'}]
        if not wf:
            rows.append({'integer1': '70'})
        steps.append({'search': {'id': n + 'grouped by enum', 'sheet': 'main', 'view': gview,
                                 'observe': ['string1', 'integer1', 'calc1', 'person1'],
                                 'aggregate': ['integer1', 'calc1'],
                                 'expect': {'rows': rows,
                                            'aggregate': {'integer1': '135' if wf else '205',
                                                          'calc1': '160' if wf else '360'}}}})
    write_scenario(os.path.join(OUT, '004_aggregate_mv%d.yaml' % mv), {
        '_generator': GEN,
        'name': '004_aggregate_mv%d' % mv,
        'source': ['t/004_aggregate.t'],
        'description': 'Aggregate (totals) results of records, including recalculated aggregates and grouped '
                       'views, with and without a filter (multivalue %d)' % mv,
        'steps': steps,
    })


def large():
    data = [{'string1': 'Foo', 'integer1': 10, 'integer2': 10} for _ in range(300)]
    steps = [
        {'sheet': {'as': 'main', 'column_count': {'integer': 2}, 'calc_code': CALC % ('', ''), 'data': data}},
        col('integer1', aggregate='sum'), col('calc1', aggregate='sum'), {'clear_layouts': {}},
        {'search': {'id': 'paged results with aggregate', 'sheet': 'main', 'rows': 50, 'page': 1,
                    'aggregate': ['integer1', 'calc1'],
                    'expect': {'pages': '6', 'aggregate': {'integer1': '3000', 'calc1': '30000'}}}},
    ]
    write_scenario(os.path.join(OUT, '004_aggregate_large.yaml'), {
        '_generator': GEN,
        'name': '004_aggregate_large',
        'source': ['t/004_aggregate.t'],
        'description': 'Aggregates of more records than a page: paging does not affect the results',
        'steps': steps,
    })


def main():
    for mv in (0, 1):
        scenario(mv)
    large()


if __name__ == '__main__':
    main()
