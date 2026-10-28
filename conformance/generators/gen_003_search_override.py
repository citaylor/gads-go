#!/usr/bin/env python3
"""Generate scenarios/003_search_override.yaml from t/003_search_override.t
(GADS-dev): a view limit override view shows records outside the view limit,
but not the columns missing from the override view, which also cannot be
searched or filtered on, and the restricted records cannot be opened."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario, rule  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios', '003_search_override.yaml')
USER = '@sheet:main'

steps = [
    {'sheet': {'as': 'main', 'data': [
        {'string1': 'bob', 'integer1': '40', 'enum1': 'foo1'},
        # Restricted record
        {'string1': 'boris', 'integer1': '50', 'enum1': 'foo2'},
    ]}},
    {'search': {'id': 'full table', 'sheet': 'main', 'expect': {'count': 2}}},
    {'search': {'id': 'initial search sensitive', 'sheet': 'main', 'search': 'boris',
                'expect': {'ids': [2]}}},
    {'view': {'op': 'create', 'sheet': 'main', 'name': 'Sensitive view', 'as': 'sensitive',
              'filter': {'rules': [rule('string1', 'boris')]}}},
    {'search': {'id': 'initial filter sensitive', 'sheet': 'main', 'view_ref': 'sensitive',
                'expect': {'ids': [2]}}},
]

# One test for a limited view attached to the user, one attached to the table
for t in ('user', 'table'):
    steps += [
        {'view': {'op': 'create', 'sheet': 'main', 'name': 'Limited view', 'as': 'limited_' + t,
                  'is_admin': 1, 'filter': {'rules': [rule('enum1', 'foo2', 'not_equal')]}}},
        {'view': {'op': 'create', 'sheet': 'main', 'name': 'Normal view', 'as': 'normal_' + t,
                  'columns': ['string1', 'integer1', 'enum1']}},
    ]
    if t == 'table':
        steps += [
            {'layout': {'op': 'update', 'sheet': 'main', 'view_limit': 'limited_' + t}},
            {'user': {'op': 'view_limits', 'user': USER, 'views': []}},
        ]
    else:
        steps += [
            {'layout': {'op': 'update', 'sheet': 'main', 'view_limit': None}},
            {'user': {'op': 'view_limits', 'user': USER, 'views': ['limited_' + t]}},
        ]
    steps += [
        {'search': {'id': '%s limited table' % t, 'sheet': 'main',
                    'observe': ['string1', 'integer1', 'enum1'],
                    'expect': {'count': 1, 'rows': [{'string1': 'bob', 'integer1': '40', 'enum1': 'foo1'}]}}},
        {'view': {'op': 'create', 'sheet': 'main', 'name': 'Override view', 'as': 'override_' + t,
                  'columns': ['integer1', 'enum1']}},
        # The override flag is not available in GADS::View: set it directly
        {'db': {'op': 'update', 'table': 'view', 'where': {'id': '@view:override_' + t},
                'set': {'is_limit_override': 1}}},
        # All records, only the columns of the override view, but the
        # internal value of the restricted field still there
        {'search': {'id': '%s override' % t, 'sheet': 'main', 'view_limit_override': 'override_' + t,
                    'record_columns': True, 'observe': ['string1'],
                    'expect': {'count': 2,
                               'record_columns': [['ID', 'integer1', 'enum1'], ['ID', 'integer1', 'enum1']],
                               'rows': [{'string1': 'bob'}, {'string1': 'boris'}]}}},
        {'held': {'op': 'find', 'id': '%s open restricted record' % t, 'as': 'r_' + t, 'sheet': 'main',
                  'current_id': 2, 'expect_error': 'record not found'}},
        {'search': {'id': '%s override search sensitive' % t, 'sheet': 'main',
                    'view_limit_override': 'override_' + t, 'search': 'boris', 'expect': {'ids': []}}},
        {'search': {'id': '%s override filter sensitive' % t, 'sheet': 'main',
                    'view_limit_override': 'override_' + t, 'view_ref': 'sensitive',
                    'expect': {'count': 2}}},
    ]

write_scenario(OUT, {
    '_generator': 'generators/gen_003_search_override.py',
    'name': '003_search_override',
    'source': ['t/003_search_override.t'],
    'description': 'View limit override: restricted records shown without restricted columns',
    'steps': steps,
})
