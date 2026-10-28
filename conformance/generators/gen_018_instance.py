#!/usr/bin/env python3
"""Generate the 018_instance scenario from upstream t/018_instance.t (tables
available to users, table purge, and table-level permissions checked against
the right table, with layouts built anew or from GADS::Instances)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_018_instance.py'

DENIED = {
    'delete': ['You do not have permission to delete records'],
    'purge': ['You do not have access to this deleted record'] * 2
    + ['You do not have permission to purge records'] * 2,
    'download': ['You do not have permission to download data'] * 2,
    'layout': ['You do not have permission to manage fields'],
    'view_create': ['User 4 does not have permission to create new views'],
    'link': ['You do not have permission to link records'],
}
ALLOWED = {
    'delete': [None],
    # Fails first as the record is not deleted
    'purge': [None, None, 'Cannot purge record that is not already deleted', None],
    'download': [None, None],
    'layout': [None],
    'view_create': [None],
    'link': [None],
}


def main():
    steps = [
        {'sheet': {'as': 's1'}},
        {'db': {'id': 'one instance', 'op': 'count', 'table': 'instance', 'expect': {'count': 1}}},
        # Second table, with no groups initially
        {'user': {'op': 'group', 'name': 'group2'}},
        {'layout': {'op': 'create_table', 'sheet': 's1', 'name': 'Table2', 'as': 't2'}},
        {'db': {'id': 'two instances', 'op': 'count', 'table': 'instance', 'expect': {'count': 2}}},
        # Access dependent on field permissions
        {'instances': {'id': 'admin tables', 'user': 'superadmin', 'expect': {'names': ['Layout1', 'Table2']}}},
        {'instances': {'id': 'normal tables', 'user': 'normal1', 'is_valid': [1, 2],
                       'expect': {'names': ['Layout1'], 'is_valid': ['1', '0']}}},
        {'column': {'op': 'create', 'sheet': 's1', 'table': 't2', 'type': 'string', 'name': 'string1',
                    'permissions': {'default': ['read']}}},
        {'instances': {'id': 'normal tables after field', 'user': 'normal1',
                       'expect': {'names': ['Layout1', 'Table2']}}},
        {'column': {'op': 'delete', 'sheet': 's1', 'table': 't2', 'name': 'string1'}},
        # Delete table and replace with a test table with all data
        {'layout': {'op': 'purge_table', 'sheet': 's1', 'table': 't2'}},
        {'db': {'id': 'instance purged', 'op': 'count', 'table': 'instance', 'expect': {'count': 1}}},
        {'sheet': {'as': 's2', 'instance_id': 2, 'curval_offset': 6, 'share_users': True}},
    ]
    for layout_from in ('new', 'instances'):
        for test in ('delete', 'purge', 'download', 'layout', 'view_create', 'link'):
            for p in (1, 2, 3, 4):
                sheet = 's1' if p in (1, 3) else 's2'
                if p == 3:
                    perms = [['default', 'delete'], ['default', 'purge']] if test == 'purge' \
                        else [['default', test]]
                    steps.append({'layout': {'op': 'set_groups', 'sheet': 's1', 'permissions': perms}})
                step = {
                    'id': '%s %s pass %d' % (layout_from, test, p),
                    'sheet': sheet,
                    'user': 'normal1',
                    'layout_from': layout_from,
                    'test': test,
                    'expect': {'errors': ALLOWED[test] if p == 3 else DENIED[test]},
                }
                if p == 3:
                    step['allowed'] = True
                    step['reset_groups'] = True
                steps.append({'table_permission': step})
    write_scenario(os.path.join(OUT, '018_instance.yaml'), {
        '_generator': GEN,
        'name': '018_instance',
        'source': ['t/018_instance.t'],
        'description': 'Tables available to users, table purge, and table-level permissions of the right table',
        'steps': steps,
    })


if __name__ == '__main__':
    main()
