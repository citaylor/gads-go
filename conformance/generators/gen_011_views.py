#!/usr/bin/env python3
"""Generate the 011_views scenario from upstream t/011_views.t (view
permissions, other users' views, deleting views with alerts and invalid
views)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_011_views.py'

def view_new(as_, **kw):
    d = {'op': 'new', 'as': as_}
    d.update(kw)
    return {'view_held': d}


def view_op(as_, op, **kw):
    d = {'op': op, 'as': as_}
    d.update(kw)
    return {'view_held': d}


def new_view1(as_):
    return view_new(as_, name='view1', columns=['string1'], **{'global': 0})


def set_user(u):
    return {'layout': {'op': 'set_user', 'user': u}}


def views(id, other_user=None):
    d = {'id': id}
    if other_user:
        d['other_user'] = other_user
    return {'views': d}


def main():
    steps = [
        {'sheet': {'as': 'main'}},
        {'user': {'as': 'create1', 'permissions': ['view_create']}},
        {'user': {'as': 'create2', 'permissions': ['view_create']}},
        {'user': {'as': 'layoutu', 'permissions': ['layout']}},
        {'user': {'as': 'viewgroup', 'permissions': ['view_create', 'view_group']}},
        {'user': {'as': 'nothing'}},
        new_view1('v'),
        set_user('nothing'),
        view_op('v', 'write', id='create view without permission',
                expect_error='does not have permission to create new views'),
    ]
    steps += [
        set_user('create1'),
        view_op('v', 'write', id='create normal view as normal user'),
        view_new('v2', id='@held:v'),
        set_user('create2'),
        view_op('v2', 'filter', id='read other user view', expect_error='.'),
    ]
    # Do test, change, then back again
    for i, test in enumerate(['is_admin', 'global', 'is_admin']):
        success = test == 'global'
        t = '%s %d' % (test, i)
        steps += [
            view_op('v', 'set', **{test: 1}),
            view_op('v', 'write', id='write %s as normal user' % t, expect_error='.'),
            set_user('viewgroup'),
            view_op('v', 'write', id='write %s as view_group no group' % t, expect_error='.'),
            view_op('v', 'set', group='default'),
            view_op('v', 'write', id='write %s with group as view_group' % t,
                    **({} if success else {'expect_error': '.'})),
            # Read group view as normal user in that group
            view_new('v2', id='@held:v'),
            {'user': {'op': 'set_groups', 'user': 'create2', 'groups': ['default'], 'save': 'create2'}},
            set_user('create2'),
            view_op('v2', 'filter', id='read %s in group' % t,
                    **({} if success else {'expect_error': '.'})),
            views('views %s in group' % t),
            # Read group view as normal user not in that group
            view_new('v2', id='@held:v'),
            {'user': {'op': 'set_groups', 'user': 'create2', 'groups': []}},
            set_user('create2'),
            view_op('v2', 'filter', id='read %s not in group' % t, expect_error='.'),
            views('views %s not in group' % t),
            # Return to previous setting
            {'user': {'op': 'set_groups', 'user': 'create2', 'restore': 'create2'}},
            # Now as admin user
            set_user('superadmin'),
            view_op('v', 'set', group=None),
            view_op('v', 'write', id='write %s as admin' % t),
            # Read global view as normal user
            view_new('v2', id='@held:v'),
            set_user('create2'),
            view_op('v2', 'filter', id='read %s as normal user' % t),
            # Put back to normal
            new_view1('v'),
            set_user('create1'),
            view_op('v', 'write'),
        ]
    # User ID is written correctly for global/personal
    steps += [
        set_user('superadmin'),
        new_view1('p'),
        view_op('p', 'write', id='personal view user id'),
        view_op('p', 'set', **{'global': 1}),
        view_op('p', 'write', id='global view no user id', expect={'user_id': None}),
        view_op('p', 'set', **{'global': 0}),
        view_op('p', 'write', id='personal view user id again'),
    ]
    # Edit other user views
    steps += [
        view_new('o', name='FooBar'),
        set_user('create1'),
        view_op('o', 'write'),
        set_user('create2'),
        views('normal user other user views', other_user='create1'),
        set_user('superadmin'),
        views('admin other user views', other_user='create1'),
        # Creating views for other users
        view_new('o', name='FooBar2', other_user='create2'),
        set_user('create1'),
        view_op('o', 'write'),
        set_user('create2'),
        views('normal user cannot create as other user'),
        set_user('create1'),
        views('normal user created own view'),
        view_new('o', name='FooBar3', other_user='create2'),
        set_user('superadmin'),
        view_op('o', 'write', view_as='foobar3'),
        set_user('create2'),
        views('admin created as other user'),
        # Edit other user's view
        set_user('superadmin'),
        view_new('o', id='foobar3'),
        view_op('o', 'set', name='FooBar4'),
        view_op('o', 'write'),
        set_user('create2'),
        views('admin updated other user view'),
    ]
    # View can be deleted, with alerts
    steps += [
        new_view1('a'),
        view_op('a', 'write', view_as='alerted'),
        {'alert': {'view': 'alerted', 'frequency': 24}},
        {'db': {'id': 'view count', 'op': 'count', 'table': 'view'}},
        view_op('a', 'set', groups=['string1']),
        view_op('a', 'write', id='group on view with alerts', expect_error='not possible to have alerts on a grouped view'),
        view_op('a', 'delete'),
        {'db': {'id': 'view count after delete', 'op': 'count', 'table': 'view'}},
    ]
    # Invalid view
    steps += [
        view_new('i', id=-10),
        view_op('i', 'get', id='invalid view', observe=True, expect={'name': None, 'columns': []}),
    ]
    # View with an invalid column in the filter (e.g. deleted)
    bad = {'rules': [{'id': 100, 'type': 'string', 'value': 'foo2', 'operator': 'equal'}], 'condition': 'equal'}
    steps += [
        view_new('f', name='Test', filter=bad),
        view_op('f', 'write', id='invalid field in filter', expect_error='does not exist'),
        view_op('f', 'set', filter=None),
        view_op('f', 'write', view_as='badfilter'),
        {'db': {'op': 'update', 'table': 'view', 'where': {'name': 'Test'},
                'set': {'filter': '{"rules":[{"id":100,"type":"string","value":"foo2","operator":"equal"}],"condition":"equal"}'}}},
        view_new('f', id='badfilter'),
        view_op('f', 'get', id='invalid rule removed from base64', filter_base64=True,
                expect={'filter_base64': {'rules': [{}], 'condition': 'equal'}}),
    ]
    long = ('Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. '
            'Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. '
            'Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. '
            'Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum')
    steps += [
        view_new('l', name=long),
        view_op('l', 'write', id='view name too long', expect_error='View name must be less than'),
    ]
    write_scenario(os.path.join(OUT, '011_views.yaml'), {
        '_generator': GEN,
        'name': '011_views',
        'source': ['t/011_views.t'],
        'description': 'View permissions, views of other users, deleting views with alerts, invalid views and filters',
        'steps': steps,
    })


if __name__ == '__main__':
    main()
