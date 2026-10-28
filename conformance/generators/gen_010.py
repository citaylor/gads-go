#!/usr/bin/env python3
"""Generate scenarios from upstream t/010_createdby.t, t/010_createddate.t,
t/010_delete.t and t/010_permissions.t."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario, rule  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_010.py'


def createdby():
    meta = lambda edited: {'meta': {'edited_user': edited}}  # noqa: E731
    steps = [
        {'sheet': {'as': 'main', 'data': [{'string1': 'foobar1'}]}},
        {'search': {'id': 'initial record created', 'sheet': 'main', 'user': 'normal1', 'expect': {'count': '1'}}},
        # Update record as a different user
        {'held': {'op': 'single', 'as': 'r', 'sheet': 'main', 'user': 'normal1'}},
        {'held': {'op': 'set', 'as': 'r', 'values': {'string1': 'foobar2'}}},
        {'held': {'op': 'write', 'as': 'r', 'options': {'no_alerts': 1}}},
        {'held': {'id': 'editor and createdby retrieved as group', 'op': 'single', 'as': 'g', 'sheet': 'main',
                  'record_meta': True, 'observe': ['_created_user'],
                  'expect': dict(meta('User4, User4'), values={'_created_user': 'User1, User1'})}},
        {'held': {'id': 'editor and createdby retrieved as single', 'op': 'find', 'as': 's',
                  'current_id': '@held:g', 'record_meta': True, 'observe': ['_created_user'],
                  'expect': dict(meta('User4, User4'), values={'_created_user': 'User1, User1'})}},
    ]
    # More records than the standard page size of 100, then a final record
    # that will appear at one end of the sort
    for _ in range(200):
        steps.append({'record': {'op': 'create', 'sheet': 'main', 'user': 'normal1',
                                 'values': {'string1': 'foobar3'}, 'options': {'no_alerts': 1}}})
    steps.append({'record': {'op': 'create', 'sheet': 'main', 'user': 'normal2',
                             'values': {'string1': 'foobar3'}, 'options': {'no_alerts': 1}}})
    for sort, first in (('asc', '1'), ('desc', '202')):
        steps += [
            {'view': {'op': 'create', 'sheet': 'main', 'as': 'created_' + sort, 'name': 'Test view',
                      'sorts': [{'name': '_created_user', 'type': sort}]}},
            {'held': {'id': 'first record sorting by created user (%s)' % sort, 'op': 'single',
                      'as': 'first_' + sort, 'sheet': 'main', 'view_ref': 'created_' + sort,
                      'expect': {'current_id': first}}},
        ]
    steps += [
        {'view': {'op': 'create', 'sheet': 'main', 'as': 'by_user', 'name': 'Test view',
                  'filter': {'rules': [rule('_created_user', 'User1, User1')]}}},
        {'search': {'id': 'search by created user', 'sheet': 'main', 'view_ref': 'by_user',
                    'expect': {'count': '1', 'ids': ['1']}}},
        # Legacy records may not have createdby defined (e.g. uploaded)
        {'db': {'op': 'update', 'table': 'record', 'set': {'createdby': None}}},
        {'held': {'id': 'blank editor retrieved as group', 'op': 'single', 'as': 'g2', 'sheet': 'main',
                  'record_meta': True, 'observe': ['_created_user'],
                  'expect': dict(meta(''), values={'_created_user': ''})}},
        {'held': {'id': 'blank editor retrieved as single', 'op': 'find', 'as': 's2',
                  'current_id': '@held:g2', 'record_meta': True, 'observe': ['_created_user'],
                  'expect': dict(meta(''), values={'_created_user': ''})}},
    ]
    return {
        '_generator': GEN,
        'name': '010_createdby',
        'source': ['t/010_createdby.t'],
        'description': 'Record editor and created-by user: retrieval, sorting, searching and legacy blank values',
        'steps': steps,
    }


def delete():
    deleted = {'meta': {'deletedby': 'User1, User1', 'deleted': '2014-10-10T01:00:00'}}
    return {
        '_generator': GEN,
        'name': '010_delete',
        'source': ['t/010_delete.t'],
        'description': 'Deleting records and retrieving deleted records',
        'fixed_time': '2014-10-10 01:00:00',
        'steps': [
            {'sheet': {'as': 'main'}},
            {'search': {'id': 'quick search first record', 'sheet': 'main', 'search': 'foo1',
                        'expect': {'count': '1'}}},
            {'search': {'id': 'initial records', 'sheet': 'main', 'expect': {'count': '2'}}},
            {'held': {'op': 'single', 'as': 'd', 'sheet': 'main'}},
            {'held': {'op': 'delete', 'as': 'd'}},
            {'search': {'id': 'record deleted', 'sheet': 'main', 'expect': {'count': '1'}}},
            {'search': {'id': 'deleted record not in quick search', 'sheet': 'main', 'search': 'foo1',
                        'expect': {'count': '0', 'ids': []}}},
            {'held': {'id': 'deleted by current id', 'op': 'find', 'as': 'x', 'current_id': '@held:d',
                      'find_options': {'deleted': 1}, 'record_meta': True, 'expect': deleted}},
            {'held': {'id': 'deleted by record id', 'op': 'find', 'as': 'x', 'clear': True,
                      'record_id': '@held_record:d', 'find_options': {'deleted': 1}, 'record_meta': True,
                      'expect': deleted}},
            {'held': {'id': 'deleted from all records', 'op': 'single', 'as': 'y', 'sheet': 'main',
                      'is_deleted': True, 'record_meta': True, 'expect': deleted}},
        ],
    }


DATES = ['_version_datetime', '_created']


def dates(id_, name, expect, op='get', **kw):
    """Version and created datums (display string in local time and hour of
    the stored UTC value) and the calc of the version hour (local time).
    expect is (version, version hour, created, created hour, calc)."""
    version, vhour, created, chour, calc = expect
    step = {'id': id_, 'op': op, 'as': name, 'datum': DATES, 'datum_props': ['as_string', 'hours'],
            'observe': ['calc1'], 'expect': {
                'datum': {'_version_datetime': {'as_string': version, 'hours': [vhour]},
                          '_created': {'as_string': created, 'hours': [chour]}},
                'values': {'calc1': calc}}}
    step.update(kw)
    return {'held': step}


def createddate():
    steps = [
        {'sheet': {'as': 'main', 'data': [],
                   'calc_code': "function evaluate (_version_datetime)\n    return _version_datetime.hour\nend"}},
        # A new record in standard time
        {'set_time': {'time': '2014-01-01 12:00:00'}},
        {'held': {'op': 'new', 'as': 'r', 'sheet': 'main'}},
        {'held': {'op': 'set', 'as': 'r', 'values': {'string1': 'Foobar'}}},
        {'held': {'op': 'write', 'as': 'r', 'options': {'no_alerts': 1}}},
        dates('standard time', 'r', ('2014-01-01 12:00:00', '12', '2014-01-01 12:00:00', '12', '12'), op='find', clear=True, current_id=1),
        # Updated in daylight saving time
        {'set_time': {'time': '2014-06-01 14:00:00'}},
        {'held': {'op': 'set', 'as': 'r', 'values': {'string1': 'Foobar2'}}},
        dates('after daylight saving time update', 'r',
              ('2014-06-01 15:00:00', '14', '2014-01-01 12:00:00', '12', '15'), op='write', options={'no_alerts': 1}),
        dates('daylight saving time', 'r', ('2014-06-01 15:00:00', '14', '2014-01-01 12:00:00', '12', '15'), op='find', clear=True, current_id=1),
        # A new record created in daylight saving time
        {'set_time': {'time': '2014-06-01 16:00:00'}},
        {'held': {'op': 'new', 'as': 'r', 'sheet': 'main'}},
        {'held': {'op': 'set', 'as': 'r', 'values': {'string1': 'Foobar3'}}},
        dates('after daylight saving time create', 'r', ('2014-06-01 17:00:00', '16', '2014-06-01 17:00:00', '16', '17'), op='write', options={'no_alerts': 1}),
        dates('created in daylight saving time', 'r', ('2014-06-01 17:00:00', '16', '2014-06-01 17:00:00', '16', '17'), op='find', clear=True, current_id=2),
        # Stored in the database as UTC
        {'db': {'id': 'stored as UTC', 'op': 'select', 'table': 'record', 'columns': ['id', 'created'],
                'expect': [{'id': '1', 'created': '2014-01-01 12:00:00'}, {'id': '2', 'created': '2014-06-01 14:00:00'},
                           {'id': '3', 'created': '2014-06-01 16:00:00'}]}},
        # More records than the standard page size of 100, then a later one
        {'set_time': {'time': '2014-07-01 16:00:00'}},
    ]
    for _ in range(200):
        steps.append({'record': {'op': 'create', 'sheet': 'main', 'values': {'string1': 'foobar4'},
                                 'options': {'no_alerts': 1}}})
    steps += [
        {'set_time': {'time': '2014-08-01 16:00:00'}},
        {'record': {'op': 'create', 'sheet': 'main', 'values': {'string1': 'foobar4'}, 'options': {'no_alerts': 1}}},
        # The first record gets the latest version time but has the earliest
        # created time
        {'set_time': {'time': '2014-10-01 14:00:00'}},
        {'record': {'op': 'update', 'sheet': 'main', 'current_id': 1, 'values': {'string1': 'Foobar5'},
                    'options': {'no_alerts': 1}}},
    ]
    for sort, first in (('asc', '1'), ('desc', '203')):
        steps += [
            {'view': {'op': 'create', 'sheet': 'main', 'as': 'created_' + sort, 'name': 'Test view',
                      'sorts': [{'name': '_created', 'type': sort}]}},
            {'held': {'id': 'first record sorting by created (%s)' % sort, 'op': 'single',
                      'as': 'first_' + sort, 'sheet': 'main', 'view_ref': 'created_' + sort,
                      'expect': {'current_id': first}}},
        ]
    steps += [
        {'view': {'op': 'create', 'sheet': 'main', 'as': 'by_created', 'name': 'Test view',
                  'filter': {'rules': [rule('_created', '2014-01-01 12:00:00')]}}},
        {'search': {'id': 'search by created time', 'sheet': 'main', 'view_ref': 'by_created',
                    'expect': {'count': '1', 'ids': ['1']}}},
    ]
    return {
        '_generator': GEN,
        'name': '010_createddate',
        'source': ['t/010_createddate.t'],
        'description': 'Time zones of record created and version times, and sorting and searching by created time',
        'steps': steps,
    }


DATA_A = dict(string1='foo', integer1='100', enum1=7, tree1=10, date1='2010-10-10',
              daterange1=['2000-10-10', '2001-10-10'], curval1=1)
DATA_B = dict(string1='bar', integer1='200', enum1=8, tree1=11, date1='2011-10-10',
              daterange1=['2000-11-11', '2001-11-11'], curval1=2)
# User input columns of the main sheet, in layout order
USERINPUT = ['string1', 'integer1', 'enum1', 'tree1', 'date1', 'daterange1', 'file1', 'person1', 'curval1']
MAIN_COLUMNS = USERINPUT + ['rag1', 'calc1']
CURVAL_COLUMNS = ['string1', 'integer1', 'enum1', 'tree1', 'date1', 'daterange1', 'file1', 'person1',
                  'rag1', 'calc1']
ALL_PERMS = ['read', 'write_new', 'write_existing', 'approve_new', 'approve_existing',
             'write_new_no_approval', 'write_existing_no_approval']


def set_data(name, data, user_type):
    values = [[c, data.get(c)] for c in USERINPUT if user_type != 'limited' or c == 'string1']
    return {'held': {'op': 'set', 'as': name, 'values': [{k: v} for k, v in values]}}


def permissions_users():
    steps = [
        {'sheet': {'as': 'curval', 'instance_id': 2, 'no_groups': 1, 'users_to_create': ['superadmin'],
                   'site_id': 1}},
        {'sheet': {'as': 'main', 'data': [DATA_A], 'curval': 'curval', 'no_groups': 1,
                   'users_to_create': ['superadmin'], 'site_id': 1}},
    ]
    for u in ('read', 'limited', 'readwrite'):
        steps.append({'user': {'op': 'create', 'as': u, 'no_group': 1}})
    for g in ('read', 'limited', 'readwrite'):
        steps.append({'user': {'op': 'group', 'name': g, 'users': [g]}})
    steps += [
        {'db': {'id': 'groups created', 'op': 'count', 'table': 'group', 'expect': {'count': '3'}}},
        {'db': {'id': 'users added to groups', 'op': 'count', 'table': 'user_group', 'expect': {'count': '3'}}},
    ]
    # The limited group only has read/write access to one field in the main
    # sheet, and none in the curval sheet
    for sheet, cols in (('main', MAIN_COLUMNS), ('curval', CURVAL_COLUMNS)):
        for c in cols:
            perms = {'read': ['read'], 'readwrite': ALL_PERMS}
            if c == 'string1' and sheet == 'main':
                perms['limited'] = ALL_PERMS
            steps.append({'column': {'op': 'update', 'sheet': sheet, 'column': c, 'set': {'permissions': perms}}})
    for user_type in ('readwrite', 'read', 'limited'):
        u = {'user': user_type, 'layout_user': True}
        for with_columns in (0, 1):
            for sheet, can in (('main', user_type != 'read'), ('curval', user_type == 'readwrite')):
                steps.append({'layout': {
                    'id': '%s can write to %s (with columns %d)' % (user_type, sheet, with_columns),
                    'op': 'user_can', 'sheet': sheet, 'user': user_type, 'with_columns': with_columns,
                    'permissions': ['write_existing'], 'expect': {'write_existing': int(can)}}})
        # Access to all curval values, regardless of user permissions, and
        # with a filter applied
        steps += [
            {'column': {'id': 'all curval values (%s)' % user_type, 'op': 'get', 'sheet': 'main',
                        'column': 'curval1', 'filtered_values': True, 'all_values': True}},
            {'column': {'op': 'update', 'sheet': 'main', 'column': 'curval1',
                        'set': {'filter': {'rules': [{'name': 'string1', 'type': 'string', 'value': 'Foo',
                                                      'operator': 'equal'}]}}}},
            {'column': {'id': 'filtered curval values (%s)' % user_type, 'op': 'get', 'sheet': 'main',
                        'column': 'curval1', 'filtered_values': True}},
            {'column': {'op': 'update', 'sheet': 'main', 'column': 'curval1', 'set': {'filter': {}}}},
            {'search': dict(u, id='one record (%s)' % user_type, sheet='main', expect={'count': '1'})},
            {'held': dict(u, id='columns of record (%s)' % user_type, op='single', **{'as': 'rec_' + user_type},
                          sheet='main', columns_render=True)},
            {'held': dict(u, op='find', **{'as': 'existing_' + user_type}, sheet='main',
                          current_id='@held:rec_' + user_type)},
            {'held': dict(u, op='new', **{'as': 'blank_' + user_type}, sheet='main')},
        ]
        for kind in ('existing', 'blank'):
            name = '%s_%s' % (kind, user_type)
            label = '%s %s' % (user_type, kind)
            steps += [
                set_data(name, DATA_B, user_type),
                {'db': {'id': 'last record before (%s)' % label, 'op': 'max', 'table': 'record', 'column': 'id'}},
            ]
            if user_type == 'read':
                steps.append({'held': {'id': 'write fails (%s)' % label, 'op': 'write', 'as': name,
                                       'expect_error': '.'}})
                continue
            steps += [
                {'held': {'id': 'write (%s)' % label, 'op': 'write', 'as': name}},
                {'db': {'id': 'new version (%s)' % label, 'op': 'max', 'table': 'record', 'column': 'id'}},
                set_data(name, DATA_A, user_type),
                {'held': {'op': 'write', 'as': name}},
            ]
        if user_type != 'read':
            # Records without a user can be purged
            steps += [
                {'held': {'op': 'user', 'as': 'existing_' + user_type, 'user': 'none'}},
                {'held': {'op': 'delete', 'as': 'existing_' + user_type}},
                {'held': {'op': 'purge', 'as': 'existing_' + user_type}},
                {'search': {'id': 'record purged (%s)' % user_type, 'sheet': 'main', 'user': 'none',
                            'expect': {'count': '1'}}},
            ]
    return {
        '_generator': GEN,
        'name': '010_permissions_users',
        'source': ['t/010_permissions.t'],
        'description': 'Read, limited and read/write users: layout permissions, curval values and writing records',
        'steps': steps,
    }


def permissions_removed():
    rw = ['read', 'write_new', 'write_existing']
    in_view = {'db': {'op': 'count', 'table': 'view_layout', 'where': {'layout_id': '@column:main:string1'}}}
    cache = {'db': {'op': 'count', 'table': 'alert_cache', 'where': {'layout_id': '@column:main:string1'}}}
    filt = {'db': {'op': 'select', 'table': 'view', 'columns': ['id', 'filter'], 'columns_match': {'filter': 'Foo'}}}
    steps = []
    for test in ('single', 'all'):
        if test == 'all':
            steps.append({'reset': {}})
        steps += [
            {'sheet': {'as': 'main', 'site_id': 1}},
            {'user': {'op': 'group', 'name': 'group2'}},
            {'column': {'op': 'update', 'sheet': 'main', 'column': 'string1',
                        'set': {'permissions': {'default': rw, 'group2': rw}}}},
            {'view': {'op': 'create', 'sheet': 'main', 'name': 'Foo', 'columns': ['string1'],
                      'filter': {'rules': [rule('string1', 'Foo')]}, 'as': 'foo'}},
            {'alert': {'sheet': 'main', 'view': 'foo', 'user': 'superadmin', 'frequency': 24}},
            {'db': dict(filt['db'], id='filter contains Foo (%s)' % test, expect=[{'filter': 1}])},
            {'db': dict(cache['db'], id='alert cache contains string1 (%s)' % test, expect={'count': '1'})},
            {'db': dict(in_view['db'], id='string column in view (%s)' % test, expect={'count': '1'})},
        ]
        # Keep one set of read permissions
        perms = {'default': rw}
        if test == 'single':
            perms['group2'] = ['write_new', 'write_existing']
        steps += [
            {'column': {'op': 'update', 'sheet': 'main', 'column': 'string1', 'set': {'permissions': perms}}},
            {'db': dict(in_view['db'], id='column still in view (%s)' % test, expect={'count': '1'})},
            {'db': dict(filt['db'], id='filter still contains Foo (%s)' % test, expect=[{'filter': 1}])},
            {'db': dict(cache['db'], id='alert cache still contains string1 (%s)' % test, expect={'count': '1'})},
            {'clear_layouts': {}},
        ]
        # Now remove read from all groups
        perms = 'none' if test == 'all' else {'group2': ['write_new', 'write_existing']}
        steps += [
            {'column': {'op': 'update', 'sheet': 'main', 'column': 'string1', 'set': {'permissions': perms}}},
            {'db': dict(in_view['db'], id='column removed from view (%s)' % test, expect={'count': '0'})},
            {'db': dict(filt['db'], id='filter no longer contains Foo (%s)' % test, expect=[{'filter': 0}])},
            {'db': dict(cache['db'], id='alert cache no longer contains string1 (%s)' % test, expect={'count': '0'})},
        ]
    return {
        '_generator': GEN,
        'name': '010_permissions_removed',
        'source': ['t/010_permissions.t'],
        'description': 'Removing read permissions removes the column from views and their filters',
        'steps': steps,
    }


def permissions_global():
    """Setting global permissions can only be done by superadmin."""
    steps = [{'sheet': {'as': 'main', 'site_id': 1}}]
    for usertype in ('superadmin', 'useradmin', 'normal1'):
        step = {'id': 'create superadmin user as %s' % usertype, 'op': 'create_user', 'current_user': usertype,
                'params': {'email': '%s@example.com' % usertype, 'firstname': 'Joe', 'surname': 'Bloggs',
                           'permissions': ['superadmin']}}
        if usertype != 'superadmin':
            step['expect_error'] = 'do not have permission'
        steps.append({'user': step})
    steps.append({'db': {'id': 'user permissions', 'op': 'select', 'table': 'user_permission',
                         'columns': ['user_id', 'permission_id']}})
    return {
        '_generator': GEN,
        'name': '010_permissions_global',
        'source': ['t/010_permissions.t'],
        'description': 'Only a superadmin can create a user with global permissions',
        'steps': steps,
    }


def main():
    for doc in (createdby(), createddate(), delete(), permissions_users(), permissions_removed(),
                permissions_global()):
        write_scenario(os.path.join(OUT, doc['name'] + '.yaml'), doc)


if __name__ == '__main__':
    main()
