#!/usr/bin/env python3
"""Generate the 011_alerts scenarios from upstream t/011_alerts.t (alerts
queued when records change, and the alert cache as views change)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario, rule  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_011_alerts.py'

BASE = dict(date1='2014-10-10', daterange1=['2014-03-21', '2015-03-01'], enum1='foo1', tree1='tree1', curval1=1)
DATA = [
    dict(BASE, string1=''), dict(BASE, string1=''), dict(BASE, string1=''), dict(BASE, string1=''),
    dict(BASE, string1='Foo', daterange1=['2010-01-04', '2011-06-03']),
    dict(BASE, string1='FooBar', date1='2015-10-10', daterange1=['2009-01-04', '2017-06-03'], curval1=2),
    dict(BASE, string1='FooBar', date1='2015-10-10', daterange1=['2009-01-04', '2017-06-03'], curval1=2),
    dict(string1='FooBar', date1='2015-10-10', daterange1=['2009-01-04', '2017-06-03'], enum1='foo1', tree1='tree1'),
    dict(string1='Disappear'),
    dict(string1='FooFooBar', date1='2010-10-10'),
] + [dict(daterange1=['2009-01-04', '2017-06-03'])] * 6 + [
    dict(curval1=1, daterange1=['2014-01-04', '2017-06-03']),
    dict(curval1=1, daterange1=['2014-01-04', '2017-06-03']),
    dict(enum1='foo1'),
    dict(curval1=1, enum1='foo1', daterange1=['2014-01-04', '2017-06-03']),
]


def f(name, rules, columns, current_id, update, alerts, **kw):
    d = dict(name=name, rules=rules, columns=columns, current_id=current_id, update=update, alerts=alerts)
    d.update(kw)
    return d


FILTERS = [
    f('Calc with record created date', None, ['Created calc'], 3, [('string1', 'foobar')], 1),
    f('View filtering on record created date', [rule('_created', '2014-10-20', 'greater')],
      ['string1'], 4, [('string1', 'FooFoo')], 1),
    f('View filtering on record updated date', [rule('_version_datetime', '2014-10-20', 'greater')],
      ['date1'], 5, [('string1', 'FooFoo2')], 2),
    f('View filtering on record updated person', [rule('_version_user', 'User5, User5')],
      ['date1'], 6, [('string1', 'FooFoo3')], 2),
    # Same record as previous test: the user making the update will not have
    # changed, so only the new record alerts
    f('View filtering on record updated person - unchanged', [rule('_version_user', 'User5, User5')],
      ['date1'], 6, [('string1', 'FooFoo4')], 1),
    f('Update of record in no filter view', None, [], 3, [('string1', 'xyz')], 1),
    f('Record appears in view', [rule('string1', 'Nothing to see here')],
      ['string1'], 3, [('string1', 'Nothing to see here')], 2),
    f('Global view', [rule('string1', 'Nothing to see here2')],
      ['string1'], 3, [('string1', 'Nothing to see here2')], 2, global_view=1),
    f('Group view', [rule('string1', 'Nothing to see here3')],
      ['string1'], 3, [('string1', 'Nothing to see here3')], 2, group_view=1),
    f('Update to row in view', [rule('string1', 'Foo'), rule('date1', '2000-01-04', 'greater', type='date')],
      ['date1'], 7, [('date1', '2014-10-15'), ('string1', 'Foo')], 2),
    f('Update to row not in view', [rule('string1', 'FooBar')],
      ['string1'], 8, [('date1', '2014-10-15')], 0),
    f('Update to row, one column in view and one not', [rule('string1', 'FooBar', 'begins_with')],
      ['string1'], 9, [('string1', 'FooBar2'), ('date1', '2017-10-15')], 2),
    f('Update to row, changes to 2 columns both in view', [rule('string1', 'FooBar', 'begins_with')],
      ['string1', 'date1'], 10, [('string1', 'FooBar2'), ('date1', '2017-10-15')], 3),
    f('Disappears from view', [rule('string1', 'Disappear')], ['string1'], 11, [('string1', 'Gone')], 1),
    f('Change of filter of column not in view', [rule('string1', 'FooFooBar')],
      ['date1'], 12, [('string1', 'Gone')], 1),
    f('Change of calc field in view', None, ['calc1'], 13, [('daterange1', ['2010-10-10', '2011-10-10'])], 2),
    f('Change of calc field forces record into view', [rule('calc1', '2014')],
      ['calc1'], 14, [('daterange1', ['2014-10-10', '2015-10-10'])], 2),
    f('Change of calc field makes no change to record not in view', [rule('calc1', '2015')],
      ['calc1'], 15, [('daterange1', ['2014-10-10', '2015-10-10'])], 0),
    f('Change of rag field in view', None, ['rag1'], 16, [('daterange1', ['2012-10-10', '2013-10-10'])], 2),
    f('Change of rag field forces record into view', [rule('rag1', 'c_amber')],
      ['rag1'], 17, [('daterange1', ['2012-10-10', '2015-10-10'])], 2),
    f('Change of rag field makes no difference to record not in view', [rule('rag1', 'c_amber')],
      ['rag1'], 18, [('daterange1', ['2013-10-10', '2015-10-10'])], 0),
    # One alert when the new record means that 2014 appears in the autocur,
    # then another when the existing record is edited to also appear in it
    f('Change of autocur/calc in other table as a result of curval change', [rule('calc1', '2014', 'contains')],
      ['calc1'], 19, [('curval1', 2), ('daterange1', ['2014-01-04', '2017-06-03'])], 2,
      alert_sheet='curval', alert_current_id=2),
    # 2 changes cause alerts, but both are the same so only one is written
    f('Change of autocur in other table as a result of curval change', None,
      ['autocur1'], 20, [('curval1', 2), ('daterange1', ['2014-01-04', '2017-06-03'])], 1,
      alert_sheet='curval', alert_current_id=2),
    # Previous values should not make any difference
    f('View with previous values set', [rule('enum1', 'foo2')], ['enum1'], 21, [('enum1', 8)], 2,
      global_view=1, previous_values='positive'),
    # Different fields of the same curval, each with a "value" join, along
    # with a "value" join for the main table, to test the join order
    f('View to test multiple fields with "value" database key',
      [rule('curval1_enum1', 'foo1'), rule('enum1', 'foo', 'begins_with'), rule('curval1_tree1', None, 'is_empty')],
      ['string1'], 22, [('enum1', 8), ('curval1', 1), ('daterange1', ['2014-01-04', '2017-06-03'])], 1,
      global_view=1, condition='AND'),
    # 7 new records appear in the view: the records referring to curval
    # record 1, which all appear when it is updated
    f('Change of curval sub-field in filter', [rule('curval1_string1', 'Bar')],
      ['string1'], 1, [('string1', 'Bar')], 7, alert_current_id=[3, 4, 5, 6, 7, 19, 20], update_sheet='curval'),
]

# Alerts queued for the new record and then for the updated record of each
# filter (adding up to its alerts as in the upstream test)
SPLIT = [(1, 0), (1, 0), (1, 1), (1, 1), (1, 0), (1, 0), (1, 1), (1, 1), (1, 1), (1, 1), (0, 0), (1, 1), (1, 2), (0, 1), (0, 1), (1, 1), (1, 1), (0, 0), (1, 1), (1, 1), (0, 0), (None, 2), (None, 1), (1, 1), (1, 0), (None, 7)]

CURVAL_CALC = """function evaluate (L2autocur1)
    return_value = ''
    for _, v in pairs(L2autocur1) do
        if v.field_values.L1daterange1.from.year == 2014 then
            return_value = return_value .. v.field_values.L1daterange1.from.year
        end
    end
    return return_value
end"""


def records_scenario():
    steps = [
        {'sheet': {'as': 'curval', 'instance_id': 2, 'calc_return_type': 'string'}},
        {'sheet': {'as': 'main', 'data': DATA, 'curval': 'curval'}},
        {'add_autocur': {'sheet': 'curval', 'refers_to': 'main', 'related_field': 'curval1',
                         'curval_fields': ['daterange1']}},
        {'column': {'op': 'update', 'sheet': 'curval', 'column': 'calc1', 'set': {'code': CURVAL_CALC}}},
        # The upstream test sets the permissions of this column after writing
        # it, so they are never written
        {'column': {'op': 'create', 'sheet': 'main', 'type': 'calc', 'name': 'Created calc',
                    'return_type': 'date', 'permissions': 'none',
                    'code': "function evaluate (_created)\n    return _created.epoch\nend"}},
        {'clear_layouts': {}},
    ]
    # First write all the views and alerts
    for i, flt in enumerate(FILTERS):
        sheet = flt.get('alert_sheet', 'main')
        view = {'op': 'create', 'sheet': sheet, 'as': 'v%d' % i, 'name': flt['name'], 'columns': flt['columns']}
        if flt['rules']:
            view['filter'] = {'rules': flt['rules']}
            if 'condition' in flt:
                view['filter']['condition'] = flt['condition']
            if 'previous_values' in flt:
                view['filter']['previous_values'] = flt['previous_values']
        else:
            view['filter'] = {}
        if flt.get('global_view') or flt.get('group_view'):
            view['global'] = 1
        if flt.get('group_view'):
            view['group'] = 'default'
        steps.append({'view': view})
        # A different user to that doing the updates
        steps.append({'alert': {'sheet': sheet, 'view': 'v%d' % i, 'user': 'normal1', 'frequency': 24}})
    steps.append({'set_time': {'time': '2014-11-10 01:00:00'}})
    # Then update all the values, checking alerts as we go
    for i, flt in enumerate(FILTERS):
        alert_id = i + 1
        sheet = flt.get('update_sheet', 'main')
        cid = flt.get('alert_current_id', flt['current_id'])
        values = [{c: v} for c, v in flt['update']]
        steps += [
            # Clear out any existing alerts, for a fair count
            {'db': {'op': 'delete', 'table': 'alert_send', 'where': {'current_id': cid, 'alert_id': alert_id}}},
            {'record': {'op': 'create', 'sheet': sheet, 'user': 'normal2', 'as': 'new%d' % i, 'values': values,
                        'options': {'no_alerts': 0}}},
        ]
        # Alerts for the new record (not for the autocur tests, where the
        # new record affects another record)
        if 'alert_current_id' not in flt:
            steps.append({'db': {'id': 'alerts for new record: %s' % flt['name'], 'op': 'count',
                                 'table': 'alert_send',
                                 'where': {'current_id': '@record:new%d' % i, 'alert_id': alert_id},
                                 'expect': {'count': str(SPLIT[i][0])}}})
        steps += [
            {'record': {'op': 'update', 'sheet': sheet, 'user': 'normal2', 'current_id': flt['current_id'],
                        'values': values, 'options': {'no_alerts': 0}}},
            {'db': {'id': 'alerts for updated record: %s' % flt['name'], 'op': 'count', 'table': 'alert_send',
                    'where': {'current_id': cid, 'alert_id': alert_id}, 'expect': {'count': str(SPLIT[i][1])}}},
        ]
    return {
        '_generator': GEN,
        'name': '011_alerts_records',
        'source': ['t/011_alerts.t'],
        'description': 'Alerts queued when new and existing records change, for views of many types',
        'fixed_time': '2014-10-10 01:00:00',
        'steps': steps,
    }


def count(id_, n, table='alert_cache', **where):
    step = {'id': id_, 'op': 'count', 'table': table, 'expect': {'count': str(n)}}
    if where:
        step['where'] = where
    return {'db': step}


def view_updates_scenario():
    data = [{'string1': 'Foo', 'date1': '2014-10-10'}] * 5 + [{'string1': 'Bar', 'date1': '2014-10-10'}] * 2
    return {
        '_generator': GEN,
        'name': '011_alerts_view_updates',
        'source': ['t/011_alerts.t'],
        'description': 'The alert cache as the columns and filter of a view change',
        'fixed_time': '2014-10-10 01:00:00',
        'steps': [
            {'sheet': {'as': 'main', 'data': data}},
            {'view': {'op': 'create', 'sheet': 'main', 'as': 'v', 'name': 'view1', 'columns': ['date1']}},
            {'alert': {'sheet': 'main', 'view': 'v', 'user': 'superadmin', 'frequency': 24}},
            count('alerts inserted', 7),
            {'view': {'op': 'update', 'sheet': 'main', 'ref': 'v', 'columns': ['string1', 'date1']}},
            count('column added', 14),
            {'view': {'op': 'update', 'sheet': 'main', 'ref': 'v', 'columns': ['string1']}},
            count('column removed', 7),
            {'view': {'op': 'update', 'sheet': 'main', 'ref': 'v', 'set_filter': {'rules': [rule('string1', 'Foo')]}}},
            count('filter added', 5),
            # A change of filter of a view instantiated from scratch, as a
            # hash then as JSON
            {'view': {'op': 'update', 'sheet': 'main', 'ref': 'v', 'set_filter': {'rules': [rule('string1', 'Bar')]}}},
            count('filter changed (hash)', 2),
            {'view': {'op': 'update', 'sheet': 'main', 'ref': 'v',
                      'set_filter_json': {'rules': [rule('string1', 'Foo')]}}},
            count('filter changed (json)', 5),
        ],
    }


def curuser_scenario():
    steps = []
    for curuser_type in ('person', 'string'):
        if curuser_type == 'string':
            steps.append({'reset': {}})
        if curuser_type == 'person':
            data = [{'string1': 'Foo', 'person1': 1}, {'string1': 'Bar', 'person1': 1},
                    {'string1': 'Foo', 'person1': 4}, {'string1': 'Foo', 'person1': None},
                    {'string1': 'Bar', 'person1': None}]
            cols = ['string1', 'person1']
            curuser_col = 'person1'
            two_rules = [rule('person1', '[CURUSER]'), rule('string1', 'Foo')]
            update = {'string1': 'FooBar'}
            no_curuser = [rule('string1', 'Foo')]
        else:
            data = [{'integer1': '100', 'string1': 'User1, User1'}, {'integer1': '200', 'string1': 'User1, User1'},
                    {'integer1': '100', 'string1': 'User4, User4'}, {'integer1': '100', 'string1': None},
                    {'integer1': '200', 'string1': None}]
            cols = ['integer1', 'string1']
            curuser_col = 'string1'
            two_rules = [rule('string1', '[CURUSER]'), rule('integer1', 100)]
            update = {'integer1': 150}
            no_curuser = [rule('integer1', '100')]
        t = curuser_type
        steps += [
            {'sheet': {'as': 'main', 'data': data}},
            {'view': {'op': 'create', 'sheet': 'main', 'as': 'v', 'name': 'view1', 'global': 1, 'columns': cols}},
            {'alert': {'sheet': 'main', 'view': 'v', 'user': 'superadmin', 'frequency': 24}},
            count('alerts inserted (%s)' % t, 10),
            {'view': {'op': 'update', 'sheet': 'main', 'ref': 'v',
                      'set_filter': {'rules': [rule(curuser_col, '[CURUSER]')]}}},
            count('CURUSER filter added, user1 (%s)' % t, 4, user_id='@user:superadmin'),
            count('CURUSER filter added, user2 (%s)' % t, 0, user_id=2),
            {'alert': {'sheet': 'main', 'view': 'v', 'user': 'normal1', 'frequency': 24}},
            count('second alert, user1 (%s)' % t, 4, user_id='@user:superadmin'),
            count('second alert, normal1 (%s)' % t, 2, user_id='@user:normal1'),
            count('second alert, no user (%s)' % t, 0, user_id=None),
            {'view': {'op': 'update', 'sheet': 'main', 'ref': 'v', 'set_filter': {'rules': two_rules}}},
            count('filter changed, user1 (%s)' % t, 2, user_id='@user:superadmin'),
            count('filter changed, normal1 (%s)' % t, 2, user_id='@user:normal1'),
            count('filter changed, no user (%s)' % t, 0, user_id=None),
            # An update causing a search of views with CURUSER
            {'record': {'op': 'update', 'sheet': 'main', 'current_id': 1, 'values': update,
                        'options': {'no_alerts': 0}}},
            {'view': {'op': 'update', 'sheet': 'main', 'ref': 'v', 'set_filter': {'rules': no_curuser}}},
            {'db': {'id': 'CURUSER filter removed, user rows (%s)' % t, 'op': 'select', 'table': 'alert_cache',
                    'columns': ['user_id']}},
            count('CURUSER filter removed, no user (%s)' % t, 4, user_id=None),
        ]
    return {
        '_generator': GEN,
        'name': '011_alerts_curuser',
        'source': ['t/011_alerts.t'],
        'description': 'Alert caches for each user of views filtered by the current user',
        'fixed_time': '2014-10-10 01:00:00',
        'steps': steps,
    }


def calc_scenario():
    code = "function evaluate (L1daterange1) \n if L1daterange1 == null then return end \n return L1daterange1.%s.year\nend"
    steps = [
        {'sheet': {'as': 'main'}},
        {'view': {'op': 'create', 'sheet': 'main', 'as': 'v', 'name': 'view1', 'global': 1, 'columns': ['calc1']}},
        {'alert': {'sheet': 'main', 'view': 'v', 'user': 'superadmin', 'frequency': 24}},
        count('alerts inserted', 2),
        count('no alerts to send', 0, table='alert_send'),
        # Same result (extra space to ensure an update)
        {'column': {'op': 'update', 'sheet': 'main', 'column': 'calc1', 'set': {'code': code % 'from' + ' '}}},
        count('calc update with no change', 0, table='alert_send'),
        # A different result for one record (the other has the same end year)
        {'column': {'op': 'update', 'sheet': 'main', 'column': 'calc1', 'set': {'code': code % 'to'}}},
        count('calc update with change', 1, table='alert_send'),
        {'db': {'op': 'delete', 'table': 'alert_send'}},
        {'view': {'op': 'update', 'sheet': 'main', 'ref': 'v',
                  'set_filter': {'rules': [rule('calc1', '2011', 'greater')]}}},
        count('alert caches after filter', 1),
        {'column': {'op': 'update', 'sheet': 'main', 'column': 'calc1', 'set': {'code': code % 'from'}}},
        count('calc update with filter', 1, table='alert_send'),
    ]
    # Changes as a result of the current date. Lua does not see the mocked
    # time, so the years are hard-coded in the code
    for viewtype in ('normal', 'group', 'global'):
        steps.append({'reset': {}})
        view = {'op': 'create', 'sheet': 'main', 'as': 'v', 'name': 'view1', 'columns': ['calc1'],
                'global': 1 if viewtype in ('group', 'global') else 0,
                'filter': {'rules': [rule('calc1', '1')]}}
        if viewtype == 'group':
            view['group'] = 'default'
        steps += [
            {'sheet': {'as': 'main', 'calc_code':
                       "function evaluate (L1daterange1) \nif L1daterange1.from.year < 2010 then return 1 else return 0 end\nend"}},
            {'view': view},
            {'alert': {'sheet': 'main', 'view': 'v', 'user': 'superadmin', 'frequency': 24}},
            count('initial alert cache (%s)' % viewtype, 1),
            count('initial alert send (%s)' % viewtype, 0, table='alert_send'),
            # Wind the date forward 4 years: a record now appears in the view
            {'db': {'op': 'update', 'table': 'calc', 'set': {'code':
                    "function evaluate (L1daterange1) \nif L1daterange1.from.year < 2014 then return 1 else return 0 end\nend"}}},
            # From a layout without a user, as in overnight updates
            {'column': {'op': 'update_cached', 'sheet': 'main', 'column': 'calc1', 'layout_user': True,
                        'user': 'none'}},
            count('alert cache after date change (%s)' % viewtype, 2),
            count('alert send after date change (%s)' % viewtype, 1, table='alert_send'),
        ]
    return {
        '_generator': GEN,
        'name': '011_alerts_calc',
        'source': ['t/011_alerts.t'],
        'description': 'Alerts after changes of calc code, and of values that depend on the current date',
        'fixed_time': '2014-10-10 01:00:00',
        'steps': steps,
    }


def bulk_scenario():
    data = [{'string1': 'Bar'}] + [{'string1': 'Foo'}] * 1000
    sent = {'op': 'count', 'table': 'alert_send', 'where': {'alert_id': 1, 'layout_id': '@column:main:string1'}}
    return {
        '_generator': GEN,
        'name': '011_alerts_bulk',
        'source': ['t/011_alerts.t'],
        'description': 'Bulk alerts as for code updates, and no alerts for a sheet without alerts',
        'fixed_time': '2014-10-10 01:00:00',
        'steps': [
            {'sheet': {'as': 'main', 'data': data}},
            {'db': dict(sent, id='alerts before', expect={'count': '0'})},
            {'view': {'op': 'create', 'sheet': 'main', 'as': 'v', 'name': 'view1', 'columns': ['string1'],
                      'filter': {'rules': [rule('string1', 'Foo')]}}},
            {'alert': {'sheet': 'main', 'view': 'v', 'user': 'superadmin', 'frequency': 24}},
            # Not all current IDs, otherwise current IDs are not searched
            {'alert_send': {'sheet': 'main', 'current_ids': list(range(1, 1001)), 'columns': ['string1']}},
            # 1001 records, minus the one left out, minus the first not in view
            {'db': dict(sent, id='bulk alerts inserted', expect={'count': '999'})},
            # A record on another sheet without alerts. Tests a previous bug
            # where views with alerts were searched across all tables
            {'db': {'op': 'delete', 'table': 'alert_cache'}},
            {'sheet': {'as': 'sheet2', 'instance_id': 2, 'curval_offset': 6}},
            {'record': {'op': 'create', 'sheet': 'sheet2', 'values': {'string1': 'FooBar'},
                        'options': {'no_alerts': 0}}},
            {'db': {'op': 'delete', 'table': 'alert_cache'}},
            count('no alerts for sheet with no alerts', 0),
        ],
    }


def main():
    for doc in (records_scenario(), view_updates_scenario(), curuser_scenario(), calc_scenario(), bulk_scenario()):
        write_scenario(os.path.join(OUT, doc['name'] + '.yaml'), doc)


if __name__ == '__main__':
    main()
