#!/usr/bin/env python3
"""Generate the 003_updates scenario from upstream t/003_updates.t (updates
to records keep the right number of records retrieved, autocur values,
forget_history, and changes of curval edits)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_003_updates.py'

UPDATE1 = [
    ([{'string1': 'Foo', 'integer1': 20, 'date1': '2010-10-10', 'daterange1': ['', ''], 'enum1': 8, 'tree1': 12,
       'curval1': 1, 'curval2': 1}],
     'Foo, 20, foo2, tree3, 2010-10-10, , , , a_grey, ; , 45, , , , , , , a_grey, ', ''),
    ([{'curval1': None, 'curval2': None}, {'curval1': None}], '', ''),
    ([{'string1': 'Bar', 'integer1': 30, 'date1': '2014-10-10', 'daterange1': ['2014-03-21', '2015-03-01'], 'enum1': 7,
       'tree1': 11, 'curval1': 1, 'curval2': 1}, {'curval1': [1, 2]}],
     'Bar, 30, foo1, tree2, 2014-10-10, 2014-03-21 to 2015-03-01, , , d_green, 2014; , 45, , , , , , , a_grey, ',
     ', 45, , , , , , , a_grey, '),
]
UPDATE2 = [('FooBar2', 'FooBar2, , , , , , , , a_grey, '), ('FooBar3', 'FooBar3, , , , , , , , a_grey, ')]


def counts(id, sheet):
    return {'search': {'id': id, 'sheet': sheet, 'expect': {'count': '2', 'ids': ['3', '4'] if sheet == 'main' else ['1', '2']}}}


def main():
    steps = [
        {'sheet': {'as': 'cv', 'instance_id': 2, 'data': [{'string1': 'FooBar1'}, {'string1': 'FooBar2'}]}},
        {'sheet': {'as': 'main', 'curval': 'cv', 'multivalue': 1, 'column_count': {'enum': 1, 'curval': 2},
                   'data': [{'string1': '', 'integer1': 10, 'date1': '', 'daterange1': ['2011-10-10', '2011-10-12'],
                             'enum1': 7, 'tree1': 10, 'curval1': 2, 'curval2': 1},
                            {'integer1': 45, 'curval1': 1}]}},
        {'add_autocur': {'sheet': 'cv', 'refers_to': 'main', 'related_field': 'curval1'}},
        {'held': {'id': 'initial main record', 'op': 'find', 'as': 'single', 'sheet': 'main', 'current_id': 3,
                  'observe': ['curval1', 'curval2'],
                  'expect': {'values': {'curval1': 'FooBar2, , , , , , , , a_grey, ',
                                        'curval2': 'FooBar1, , , , , , , , a_grey, '}}}},
        counts('initial count', 'main'),
        {'held': {'id': 'initial autocur', 'op': 'find', 'as': 'cvrec', 'sheet': 'cv', 'current_id': 1,
                  'observe': ['autocur1'], 'expect': {'values': {'autocur1': ', 45, , , , , , , a_grey, '}}}},
    ]
    for t, (updates, auto1, auto2) in enumerate(UPDATE1, 1):
        for n, update in enumerate(updates, 1):
            steps.append({'held': {'op': 'single', 'as': 'u', 'sheet': 'main', 'nth': n}})
            steps.append({'held': {'op': 'set', 'as': 'u', 'values': update}})
            steps.append({'held': {'id': 'update %d.%d' % (t, n), 'op': 'write', 'as': 'u', 'options': {'no_alerts': 1}}})
            steps.append(counts('count after update %d.%d' % (t, n), 'main'))
        steps.append({'held': {'id': 'autocur after update %d' % t, 'op': 'find', 'as': 'cvrec', 'sheet': 'cv', 'clear': True,
                               'current_id': 1, 'observe': ['autocur1'], 'expect': {'values': {'autocur1': auto1}}}})
        steps.append({'search': {'id': 'autocur records after update %d' % t, 'sheet': 'cv', 'observe': ['autocur1'],
                                 'expect': {'rows': [{'autocur1': auto1}, {'autocur1': auto2}]}}})
    for n, (value, cstring) in enumerate(UPDATE2, 1):
        steps.append({'held': {'op': 'set', 'as': 'cvrec', 'values': {'string1': value}}})
        steps.append({'held': {'id': 'curval update %d' % n, 'op': 'write', 'as': 'cvrec', 'options': {'no_alerts': 1}}})
        steps.append(counts('main count after curval update %d' % n, 'main'))
        steps.append(counts('curval count after curval update %d' % n, 'cv'))
        steps.append({'held': {'id': 'main after curval update %d' % n, 'op': 'find', 'as': 'single', 'sheet': 'main',
                               'clear': True, 'current_id': 3, 'observe': ['curval1', 'curval2'],
                               'expect': {'values': {'curval1': cstring, 'curval2': cstring}}}})
    # forget_history
    steps += [
        {'db': {'op': 'update', 'table': 'instance', 'where': {'id': 1}, 'set': {'forget_history': 1}}},
        {'clear_layouts': {}},
        {'db': {'id': 'versions before', 'op': 'count', 'table': 'record'}},
        {'held': {'id': 'old version date', 'op': 'find', 'as': 'f', 'sheet': 'main', 'current_id': 3, 'observe': ['_version_datetime']}},
        {'held': {'op': 'set', 'as': 'f', 'values': {'string1': 'Foobar'}}},
        {'set_time': {'time': '2015-10-10 01:00:00'}},
        {'held': {'id': 'write forgetting history', 'op': 'write', 'as': 'f', 'options': {'no_alerts': 1}}},
        {'db': {'id': 'no new version', 'op': 'count', 'table': 'record'}},
        {'held': {'id': 'new version date', 'op': 'find', 'as': 'f', 'clear': True, 'current_id': 3, 'observe': ['_version_datetime']}},
        {'held': {'op': 'find', 'as': 'fc', 'sheet': 'cv', 'current_id': 1}},
        {'held': {'op': 'set', 'as': 'fc', 'values': {'string1': 'Foobar2'}}},
        {'held': {'id': 'write other sheet', 'op': 'write', 'as': 'fc', 'options': {'no_alerts': 1}}},
        {'db': {'id': 'new version other sheet', 'op': 'count', 'table': 'record'}},
        {'db': {'op': 'update', 'table': 'instance', 'where': {'id': 1}, 'set': {'forget_history': 0}}},
        {'clear_layouts': {}},
        {'held': {'op': 'find', 'as': 'f', 'clear': True, 'current_id': 3}},
        {'held': {'op': 'set', 'as': 'f', 'values': {'string1': 'Foobar3'}}},
        {'held': {'id': 'write with history', 'op': 'write', 'as': 'f', 'options': {'no_alerts': 1}}},
        {'db': {'id': 'new version with history', 'op': 'count', 'table': 'record'}},
    ]
    # Changes of curval edits
    steps += [
        {'reset': {}},
        {'sheet': {'as': 'cv', 'instance_id': 2}},
        {'sheet': {'as': 'main', 'curval': 'cv', 'curval_fields': ['string1'], 'data': [{'curval1': [1, 2]}]}},
        {'column': {'op': 'update', 'sheet': 'main', 'column': 'curval1', 'set': {'show_add': 1, 'value_selector': 'noshow'},
                    'write_options': {'no_alerts': 1}}},
        {'held': {'id': 'initial curval', 'op': 'find', 'as': 'c', 'sheet': 'main', 'current_id': 3, 'observe': ['curval1'],
                  'expect': {'values': {'curval1': 'Foo; Bar'}}}},
    ]
    for id, value, changed in [('same ids', [1, 2], '0'),
                               ('same content', ['@field:string1=Foo&current_id=1', '@field:string1=Bar&current_id=2'], '0'),
                               ('changed content', ['@field:string1=Foobar&current_id=1', '@field:string1=Bar&current_id=2'], '1')]:
        steps.append({'held': {'op': 'set', 'as': 'c', 'values': {'curval1': value}}})
        steps.append({'held': {'id': id, 'op': 'get', 'as': 'c', 'datum': ['curval1'], 'datum_props': ['changed'],
                               'expect': {'datum': {'curval1': {'changed': changed}}}}})
    write_scenario(os.path.join(OUT, '003_updates.yaml'), {
        '_generator': GEN,
        'name': '003_updates',
        'source': ['t/003_updates.t'],
        'description': 'Updates keep the right number of records, autocur values, forget_history and curval edit changes',
        'fixed_time': '2014-10-10 01:00:00',
        'steps': steps,
    })


if __name__ == '__main__':
    main()
