#!/usr/bin/env python3
"""Generate the 008_child_records scenarios from upstream
t/008_child_records.t (child records sharing values with their parent)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_008_child_records.py'

COLUMNS = ['string1', 'integer1', 'enum1', 'tree1', 'date1', 'daterange1', 'rag1', 'calc1']


def records(id_, calcs):
    """All records: parent, other and child, with the calc of the parent and
    child checked."""
    return {'search': {
        'id': id_, 'sheet': 'main', 'observe': COLUMNS, 'relations': True,
        'expect': {'count': '3', 'rows': [{'calc1': calcs[0]}, {}, {'calc1': calcs[1]}]},
    }}


def find(name, ref, **kw):
    step = {'op': 'find', 'as': name, 'clear': True, 'current_id': ref}
    step.update(kw)
    return {'held': step}


def set_write(name, values, alerts=False):
    return [
        {'held': {'op': 'set', 'as': name, 'values': values}},
        {'held': {'op': 'write', 'as': name, 'options': {'no_alerts': 0 if alerts else 1}}},
    ]


def alerts_sent(id_, n):
    return {'db': {'id': id_, 'op': 'count', 'table': 'alert_send', 'expect': {'count': str(n)}}}


def can_child(column, value):
    return {'column': {'op': 'update', 'sheet': 'main', 'column': column, 'set': {'can_child': value}}}


def main_scenario():
    steps = [
        {'sheet': {'as': 'main', 'multivalue': 1}},
        {'layout': {'id': 'no children initially', 'op': 'has_children', 'sheet': 'main',
                    'expect': {'has_children': 0}}},
        {'search': {'id': 'initial records', 'sheet': 'main', 'expect': {'count': '2'}}},
        {'held': {'op': 'single', 'as': 'parent', 'sheet': 'main'}},
        {'held': {'op': 'new', 'as': 'child', 'sheet': 'main', 'parent_id': '@held:parent'}},
        {'held': {'id': 'child with no unique columns', 'op': 'write', 'as': 'child', 'expect_error': '.'}},
        # Only a unique daterange: affects the daterange and the dependent calc
        can_child('daterange1', 1),
        {'clear_layouts': {}},
        {'layout': {'id': 'children after child column', 'op': 'has_children', 'sheet': 'main',
                    'expect': {'has_children': 1}}},
    ]
    steps += set_write('child', {'daterange1': ['2011-10-10', '2015-10-10']})
    steps.append(records('after first child write', ['2012', '2011']))
    # Update the parent and check relevant changes in the child
    steps.append(find('parent', '@held:parent'))
    steps += set_write('parent', {'daterange1': ['2000-01-01', '2000-02-02'], 'string1': 'foo2',
                                  'enum1': '2', 'tree1': '5', 'date1': '2017-04-05'})
    steps.append(records('after parent write', ['2000', '2011']))
    # Multivalue: a second value, then back to a single value
    steps.append(find('parent', '@held:parent'))
    steps += set_write('parent', {'enum1': ['2', '3']})
    steps.append(records('after multivalue parent write', ['2000', '2011']))
    steps.append(find('parent', '@held:parent'))
    steps += set_write('parent', {'enum1': '3'})
    steps.append(records('after single value parent write', ['2000', '2011']))
    # Change the unique field
    steps += [can_child('daterange1', 0), can_child('string1', 1), {'clear_layouts': {}}]
    steps.append(find('child', '@held:child'))
    steps += set_write('child', {'string1': 'foo3'})
    steps.append(records('after changing unique field', ['2000', '2000']))
    # Parent and child IDs of each record, as a single fetch, by record ID
    # (the upstream test uses the current IDs as record IDs) and in bulk
    steps += [
        {'held': {'id': 'child parent id', 'op': 'find', 'as': 'c2', 'current_id': '@held:child',
                  'observe_parent': True, 'child_record_ids': True}},
        {'held': {'id': 'parent child ids', 'op': 'find', 'as': 'p2', 'current_id': '@held:parent',
                  'observe_parent': True, 'child_record_ids': True}},
        {'held': {'id': 'child parent id by record id', 'op': 'find', 'as': 'c2', 'clear': True,
                  'record_id': 3, 'observe_parent': True, 'child_record_ids': True}},
        {'held': {'id': 'parent child ids by record id', 'op': 'find', 'as': 'p2', 'clear': True,
                  'record_id': 1, 'observe_parent': True, 'child_record_ids': True}},
    ]
    # Update of a child record that has been deleted
    steps.append(find('p3', '@held:parent'))
    steps += set_write('p3', {'date1': '1980-10-05'})
    steps += [
        find('c3', '@held:child', id='child value written by parent', observe=['date1'],
             expect={'values': {'date1': '1980-10-05'}}),
        {'held': {'op': 'delete', 'as': 'c3'}},
        find('p3', '@held:parent'),
    ]
    steps += set_write('p3', {'date1': '1985-10-05'})
    steps += [
        find('c3', '@held:c3', id='deleted child not updated', find_options={'deleted': 1},
             observe=['date1'], expect={'values': {'date1': '1980-10-05'}}),
        find('p3', '@held:parent', id='parent updated', observe=['date1'],
             expect={'values': {'date1': '1985-10-05'}}),
        {'held': {'op': 'restore', 'as': 'c3'}},
    ]
    # Child record retrieved directly even with no child fields (string1 is
    # the only one left)
    steps += [
        can_child('string1', 0),
        {'clear_layouts': {}},
        find('c4', '@held:child', id='child without child fields', observe=['string1'],
             expect={'values': {'string1': 'foo3'}}),
        find('p4', '@held:parent'),
    ]
    steps += set_write('p4', {'string1': 'Foobar'})
    steps.append(find('c4', '@held:c4', id='child updated via parent without child fields',
                      observe=['string1'], expect={'values': {'string1': 'Foobar'}}))
    return {
        '_generator': GEN,
        'name': '008_child_records',
        'source': ['t/008_child_records.t'],
        'description': 'Child records: unique values, values copied from the parent, deleted children',
        'steps': steps,
    }


def calc_scenario():
    steps = []
    for calc_depend in (0, 1):
        if calc_depend:
            steps.append({'reset': {}})
        code = ("function evaluate (L1string1, L1integer1) \n return string.sub(L1string1, 1, 3) .. L1integer1 \n end"
                if calc_depend else
                "function evaluate (L1string1) \n return 'XX' .. string.sub(L1string1, 1, 3) \n end")
        label = 'calc_depend %d' % calc_depend
        count = '2' if calc_depend else '1'
        steps += [
            {'sheet': {'as': 'main', 'data': [{'string1': 'Foo', 'integer1': 50}],
                       'calc_code': code, 'calc_return_type': 'string'}},
            can_child('string1' if calc_depend else 'integer1', 1),
            {'clear_layouts': {}},
            {'held': {'id': 'initial calc (%s)' % label, 'op': 'find', 'as': 'parent', 'current_id': 1,
                      'observe': ['calc1'], 'expect': {'values': {'calc1': 'Foo50' if calc_depend else 'XXFoo'}}}},
            {'held': {'op': 'new', 'as': 'child', 'sheet': 'main', 'parent_id': '@held:parent'}},
        ]
        steps += set_write('child', {'string1': 'Bar'} if calc_depend else {'integer1': 100}, alerts=True)
        steps += [
            find('child', '@held:child', id='calc for child (%s)' % label, observe=['calc1'],
                 expect={'values': {'calc1': 'Bar50' if calc_depend else 'XXFoo'}}),
            {'view': {'op': 'create', 'sheet': 'main', 'as': 'view1', 'name': 'view1', 'global': 1,
                      'columns': ['calc1']}},
            # The calc can be different in the child, so the child is in the view
            {'search': {'id': 'parent and child in view (%s)' % label, 'sheet': 'main', 'view_ref': 'view1',
                        'observe': ['calc1'], 'expect': {'count': count}}},
            {'alert': {'sheet': 'main', 'view': 'view1', 'user': 'superadmin', 'frequency': 24}},
            alerts_sent('no alerts initially (%s)' % label, 0),
        ]
        steps += set_write('parent', {'string1': 'Baz'}, alerts=True)
        steps += [
            alerts_sent('alerts after parent write (%s)' % label, count),
            {'search': {'id': 'after parent write (%s)' % label, 'sheet': 'main',
                        'view_ref': 'view1', 'observe': ['calc1']}},
            {'db': {'op': 'delete', 'table': 'alert_send'}},
        ]
        # A new parent value that does not affect the calc, then one that does
        steps.append(find('parent', '@held:parent'))
        steps += set_write('parent', {'string1': 'Bazbar'}, alerts=True)
        steps += [
            alerts_sent('no alerts for unchanged calc (%s)' % label, 0),
            {'search': {'id': 'unchanged calc (%s)' % label, 'sheet': 'main',
                        'view_ref': 'view1', 'observe': ['calc1']}},
        ]
        steps += set_write('parent', {'string1': 'Baybar'}, alerts=True)
        steps += [
            alerts_sent('alerts for changed calc (%s)' % label, count),
            {'search': {'id': 'changed calc (%s)' % label, 'sheet': 'main',
                        'view_ref': 'view1', 'observe': ['calc1']}},
        ]
    return {
        '_generator': GEN,
        'name': '008_child_records_calc',
        'source': ['t/008_child_records.t'],
        'description': 'Calc values of child records and the alerts sent when the parent changes',
        'steps': steps,
    }


def main():
    for doc in (main_scenario(), calc_scenario()):
        write_scenario(os.path.join(OUT, doc['name'] + '.yaml'), doc)


if __name__ == '__main__':
    main()
