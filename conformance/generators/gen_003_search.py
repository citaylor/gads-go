#!/usr/bin/env python3
"""Generate scenarios ported from t/003_search.t (filters, aggregates)."""
import os, sys
sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario, rule

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')

LONG = ('Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. '
        'Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. '
        'Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. '
        'Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum')

DATA = [
    {'string1': '', 'integer1': -4, 'date1': '', 'daterange1': ['', ''], 'enum1': 'foo1', 'tree1': 'tree1', 'curval1': 1},
    {'string1': '', 'integer1': 5, 'date1': '', 'daterange1': ['', ''], 'enum1': 'foo1', 'tree1': 'tree3', 'curval1': 2},
    {'string1': '', 'integer1': 6, 'date1': '2014-10-10', 'daterange1': ['2014-03-21', '2015-03-01'], 'enum1': 'foo1', 'tree1': 'tree2', 'curval1': 2},
    {'string1': 'Foo', 'integer1': 7, 'date1': '2014-10-10', 'daterange1': ['2013-10-10', '2013-12-03'], 'enum1': 'foo1', 'tree1': 'tree1'},
    {'string1': 'FooBar', 'date1': '2015-10-10', 'daterange1': ['2009-01-04', '2017-06-03'], 'enum1': 'foo2', 'tree1': 'tree2', 'curval2': 1},
    {'string1': LONG + '1', 'integer1': 2},
    {'string1': LONG + '2', 'integer1': 3},
]

CALC_CODE = '''
        function evaluate (L1daterange1)
            if type(L1daterange1) == "table" and L1daterange1[1] then
                dr1 = L1daterange1[1]
            elseif type(L1daterange1) == "table" and next(L1daterange1) == nil then
                dr1 = nil
            else
                dr1 = L1daterange1
            end
            if dr1 == nil then return end
            return dr1.from.epoch
        end'''


def setup_steps():
    return [
        {'sheet': {'as': 'curval', 'instance_id': 2}},
        {'sheet': {'as': 'main', 'instance_id': 1, 'data': DATA, 'curval': 'curval', 'multivalue': True,
                   'calc_code': CALC_CODE, 'calc_return_type': 'date', 'column_count': {'enum': 1, 'curval': 2},
                   'create_records': False}},
        {'layout': {'op': 'position', 'sheet': 'main', 'columns': ['curval1', 'string1', 'integer1', 'date1', 'daterange1', 'enum1', 'tree1']}},
        {'column': {'op': 'create', 'sheet': 'main', 'type': 'calc', 'name': 'calc_int', 'return_type': 'integer',
                    'code': 'function evaluate (L1integer1) return L1integer1[1] end'}},
        {'create_records': {'sheet': 'main'}},
        {'add_autocur': {'sheet': 'curval', 'refers_to': 'main', 'related_field': 'curval1'}},
        {'add_autocur': {'sheet': 'curval', 'refers_to': 'main', 'related_field': 'curval2'}},
        {'record': {'id': 'change enum of record 6', 'op': 'update', 'sheet': 'main', 'user': 'normal1', 'current_id': 6, 'values': {'enum1': 8}}},
        {'sheet': {'as': 'curval2', 'instance_id': 3, 'curval_offset': 12}},
        {'column': {'op': 'create', 'sheet': 'curval', 'type': 'curval', 'name': 'curval3', 'refers_to': 'curval2', 'curval_fields': ['string1']}},
        {'record': {'id': 'set curval3', 'op': 'update', 'sheet': 'curval', 'user': 'normal1', 'current_id': 1, 'values': {'curval3': 10}}},
        {'db': {'id': 'force string 3 undef', 'table': 'string', 'where': {'id': 3}, 'set': {'value': None}}},
        {'db': {'id': 'force string 4 empty', 'table': 'string', 'where': {'id': 4}, 'set': {'value': ''}}},
    ]


def F(name, rules, count, aggregate, condition=None, columns=None, no_errors=False, sheet='main', values=None, count_graph=None):
    return dict(name=name, rules=rules, count=count, aggregate=aggregate, condition=condition,
                columns=columns, no_errors=no_errors, sheet=sheet, values=values)


FILTERS = [
    F('string is Foo', [rule('string1', 'Foo')], 1, 7),
    F('check case-insensitive search', [rule('string1', 'foo', 'begins_with')], 2, 7),
    F('string is long1', [rule('string1', LONG + '1')], 1, 2),
    F('string is long', [rule('string1', LONG, 'begins_with')], 2, 5),
    F('date is equal', [rule('date1', '2014-10-10', type='date')], 2, 13),
    F('date using CURDATE', [rule('date1', 'CURDATE', type='date')], 2, 13),
    F('date using CURDATE plus 1 year', [rule('date1', 'CURDATE + %d' % (86400 * 365), type='date')], 1, ''),
    F('date in calc', [rule('calc1', 'CURDATE - %d' % (86400 * 365), type='date')], 1, 7),
    F('negative filter for calc', [rule('calc_int', '-1', 'less')], 1, -4),
    F('date is empty', [rule('date1', operator='is_empty', type='date')], 4, 6),
    F('date is empty - value as array ref', [rule('date1', [], 'is_empty', type='date')], 4, 6),
    F('date is blank string', [rule('date1', '', type='date')], 4, 6, no_errors=True),
    F('string begins with Foo', [rule('string1', 'Foo', 'begins_with')], 2, 7),
    F('string contains ooba', [rule('string1', 'ooba', 'contains')], 1, ''),
    F('string does not contain ooba', [rule('string1', 'ooba', 'not_contains')], 6, 19),
    F('string does not begin with Foo', [rule('string1', 'Foo', 'not_begins_with')], 5, 12),
    F('string is empty', [rule('string1', operator='is_empty')], 3, 7),
    F('string is not equal to Foo', [rule('string1', 'Foo', 'not_equal')], 6, 12),
    F('string is not equal to nothing', [rule('string1', '', 'not_equal')], 4, 12),
    F('string is not equal to nothing (array ref)', [rule('string1', [], 'not_equal')], 4, 12),
    F('use of user team in substitution', [rule('string1', '[CURUSER.TEAM]')], 1, 7),
    F('greater than undefined value', [rule('integer1', operator='greater', type='integer')], 1, ''),
    F('negative integer filter', [rule('integer1', '-1', 'less', type='integer')], 1, -4),
    F('daterange less than', [rule('daterange1', '2013-12-31', 'less', type='daterange')], 1, 7),
    F('daterange less or equal', [rule('daterange1', '2013-12-31', 'less_or_equal', type='daterange')], 2, 7),
    F('daterange greater than', [rule('daterange1', '2013-12-31', 'greater', type='daterange')], 1, 6),
    F('daterange greater or equal', [rule('daterange1', '2014-10-10', 'greater_or_equal', type='daterange')], 2, 6),
    F('daterange equal', [rule('daterange1', '2014-03-21 to 2015-03-01', type='daterange')], 1, 6),
    F('daterange not equal', [rule('daterange1', '2014-03-21 to 2015-03-01', 'not_equal', type='daterange')], 6, 13),
    F('daterange empty', [rule('daterange1', operator='is_empty', type='daterange')], 4, 6),
    F('daterange not empty', [rule('daterange1', operator='is_not_empty', type='daterange')], 3, 13),
    F('daterange contains', [rule('daterange1', '2014-10-10', 'contains', type='daterange')], 2, 6),
    F('daterange does not contain', [rule('daterange1', '2014-10-10', 'not_contains', type='daterange')], 5, 13),
    F('nested search', [rule('string1', 'Foo', 'begins_with'),
                        {'rules': [rule('date1', '2015-10-10', type='date'), rule('date1', '2014-12-01', 'greater', type='date')]}],
      1, '', condition='AND'),
    F('Search using enum with different tree in view', [rule('enum1', 'foo1')], 3, 7),
    F('Search negative multivalue enum', [rule('enum1', 'foo1', 'not_equal')], 4, 12),
    F('Search using enum with curval in view', [rule('enum1', 'foo1')], 3, 7, columns=['curval1'],
      values={'curval1': 'Foo, 50, foo1, , 2014-10-10, 2012-02-10 to 2013-06-15, , , c_amber, 2012'}),
    F('Search 2 using enum with different tree in view', [rule('tree1', 'tree1')], 2, 3, columns=['tree1', 'enum1']),
    F('Search for ID', [rule('_id', '4', type='integer')], 1, 5, columns=['string1']),
    F('Search for multiple IDs', [rule('_id', ['4', '5'], type='integer')], 2, 11, columns=['string1']),
    F('Search for empty IDs', [rule('_id', [], type='integer')], 0, '', columns=['string1']),
    F('Search for version date 1', [rule('_version_datetime', '2014-10-10', 'greater', type='date'),
                                    rule('_version_datetime', '2014-10-11', 'less', type='date')], 7, 19,
      condition='AND', columns=['string1']),
    F('Search for version date 2', [rule('_version_datetime', '2014-10-15', 'greater', type='date')], 0, '', columns=['string1']),
    F('Search for created date', [rule('_created', '2014-10-15', 'less', type='date')], 7, 19, columns=['string1']),
    F('Search for version editor', [rule('_version_user', 'User4, User4')], 1, 7, columns=['string1']),
    F('Search for invalid date', [rule('date1', '20188-01', type='date')], 0, '', columns=['string1'], no_errors=True),
    F('Search for invalid daterange', [rule('daterange1', '20188-01 XX', type='date')], 0, '', columns=['string1'], no_errors=True),
    F('Search for blank calc date as empty string (array ref)', [rule('calc1', [''], type='date')], 4, 6),
    F('Search by curval ID', [rule('curval1', '2')], 2, 11, columns=['string1']),
    F('Search by curval ID not equal', [rule('curval1', '2', 'not_equal')], 5, 8, columns=['string1']),
    F('Search curval ID and enum, only curval in view', [rule('curval1', '1'), rule('enum1', 'foo1')], 1, -4,
      condition='AND', columns=['curval1']),
    F('Search by curval field', [rule('curval1_string1', 'Bar')], 2, 11, columns=['string1']),
    F('Search by curval field not equal', [rule('curval1_string1', 'Bar', 'not_equal')], 5, 8, columns=['string1']),
    F('Search by curval enum field', [rule('curval1_enum1', 'foo2')], 2, 11, columns=['enum1']),
    F('Search by curval within curval', [rule('curval1_curval3', '10')], 1, -4, columns=['curval1']),
    F('Search by curval enum field across 2 curvals', [rule('curval1_enum1', 'foo2'), rule('curval2_enum1', 'foo1')], 3, 11,
      condition='OR', columns=['enum1']),
    F('Search by autocur ID', [rule('autocur1', '3')], 1, 50, columns=['autocur1'], sheet='curval'),
    F('Search by autocur ID not equal', [rule('autocur1', '3', 'not_equal')], 1, 99, columns=['autocur1'], sheet='curval'),
    F('Search by autocur enum field', [rule('autocur1_enum1', 'foo1')], 2, 149, columns=['string1'], sheet='curval'),
    F('Search for invalid autocur', [rule('autocur1', 'Foobar')], 0, '', columns=['autocur1'], sheet='curval', no_errors=True),
    F('Search by record ID', [rule('ID', '3')], 1, -4, columns=['string1']),
    F('Search by invalid record ID', [rule('ID', '3DD')], 0, '', columns=['string1'], no_errors=True),
]


def filter_steps(mv):
    steps = [{'set_multivalue': {'id': 'multivalue %d' % mv, 'sheet': 'main', 'value': mv}}]
    if not mv:
        steps.append({'column': {'op': 'update', 'sheet': 'main', 'name': 'integer1', 'set': {'aggregate': 'sum'}}})
        steps.append({'column': {'op': 'update', 'sheet': 'curval', 'name': 'integer1', 'set': {'aggregate': 'sum'}}})
    for f in FILTERS:
        cols = f['columns'] or ['string1', 'tree1']
        flt = {'rules': f['rules']}
        if f['condition']:
            flt['condition'] = f['condition']
        prefix = 'mv%d %s' % (mv, f['name'])
        if f['no_errors']:
            steps.append({'view': {'id': prefix + ': write fails', 'op': 'create', 'sheet': f['sheet'], 'name': 'Test view',
                                   'filter': flt, 'columns': cols, 'expect_error': '.'}})
        expect = {'count': f['count']}
        search = {'id': prefix, 'sheet': f['sheet'], 'user': 'normal1', 'filter': flt, 'columns': cols,
                  'observe': cols, 'expect': expect}
        if f['values']:
            search['expect'] = dict(expect, rows=None)
            del search['expect']['rows']
        steps.append({'search': search})
        if f['values']:
            steps.append({'search': {'id': prefix + ': value of first result', 'sheet': f['sheet'], 'user': 'normal1',
                                     'filter': flt, 'columns': cols, 'observe': list(f['values'].keys()), 'no_count': True,
                                     'expect_first': f['values']}})
        steps.append({'search': {'id': prefix + ': sorted', 'sheet': f['sheet'], 'user': 'normal1',
                                 'view': {'filter': flt, 'columns': cols, 'sorts': [{'name': c, 'type': 'asc'} for c in cols]},
                                 'observe': cols, 'expect': {'count': f['count']}}})
        agg_cols = cols + (['integer1'] if 'integer1' not in cols else [])
        steps.append({'search': {'id': prefix + ': aggregate', 'sheet': f['sheet'], 'user': 'normal1',
                                 'view': {'filter': flt, 'columns': agg_cols}, 'no_count': True, 'no_results': True,
                                 'aggregate': ['integer1'], 'expect': {'aggregate': {'integer1': f['aggregate']}}}})
    return steps


def view_limit_steps(mv):
    p = 'vl mv%d ' % mv
    S = lambda **kw: {'search': dict({'sheet': 'main', 'user': 'normal1'}, **kw)}
    R = lambda **kw: {'record': dict({'op': 'get', 'sheet': 'main', 'user': 'normal1', 'observe': ['string1']}, **kw)}
    steps = [
        {'set_multivalue': {'id': p + 'set multivalue', 'sheet': 'main', 'value': mv}},
        {'view': {'id': p + 'table limit view', 'op': 'create', 'sheet': 'main', 'name': 'Limit to view table', 'is_admin': 1,
                  'filter': {'rules': [rule('date1', '2015-10-10', type='date')]}, 'as': 'vlt%d' % mv}},
        {'layout': {'op': 'update', 'sheet': 'main', 'view_limit': 'vlt%d' % mv}},
        S(id=p + 'limit to table view', expect={'count': 1}),
        {'view': {'id': p + 'user limit view', 'op': 'create', 'sheet': 'main', 'name': 'Limit to view', 'is_admin': 1,
                  'filter': {'rules': [rule('date1', '2014-10-10', type='date')]}, 'as': 'vl%d' % mv}},
        {'user': {'op': 'view_limits', 'user': 'normal1', 'views': ['vl%d' % mv]}},
        S(id=p + 'user limit replaces table limit', expect={'count': 2}),
        {'layout': {'op': 'update', 'sheet': 'main', 'view_limit': None}},
        {'view': {'id': p + 'normal view', 'op': 'create', 'sheet': 'main', 'name': 'Foo',
                  'filter': {'rules': [rule('string1', 'Foo', 'begins_with')]}, 'as': 'foo%d' % mv}},
        S(id=p + 'normal view plus limit', view_ref='foo%d' % mv, observe=['string1'], expect={'count': 1}),
        {'column': {'op': 'update', 'sheet': 'main', 'name': 'date1', 'set': {'permissions': {'default': []}}}},
        S(id=p + 'limit without permission to field', view_ref='foo%d' % mv, expect={'count': 1}),
        {'column': {'op': 'update', 'sheet': 'main', 'name': 'date1', 'set': {'permissions': 'default'}}},
    ]
    for cols in (None, []):
        q = p + ('no columns ' if cols is not None else '')
        extra = {'columns': cols} if cols is not None else {}
        steps += [
            R(id=q + 'find current 5', current_id=5, expect={'current_id': 5}, **extra),
            R(id=q + 'find record 5', record_id=5, expect={'current_id': 5}, **extra),
            R(id=q + 'find current 4 not viewable', current_id=4, expect_error='.', **extra),
            R(id=q + 'find record 4 not viewable', record_id=4, expect_error='.', **extra),
            {'db': {'table': 'current', 'where': {'id': 5}, 'set': {'deleted': '@now'}}},
            R(id=q + 'find deleted current 5', current_id=5, expect_error='Requested record not found', **extra),
            R(id=q + 'find deleted record 5', record_id=5, expect_error='Requested record not found', **extra),
            {'db': {'table': 'current', 'where': {'id': 5}, 'set': {'deleted': None}}},
        ]
    steps += [
        {'view': {'id': p + 'second limit view', 'op': 'create', 'sheet': 'main', 'name': 'Limit to view2',
                  'filter': {'rules': [rule('date1', '2015-10-10', type='date')]}, 'as': 'vl2_%d' % mv}},
        {'user': {'op': 'view_limits', 'user': 'normal1', 'views': ['vl%d' % mv, 'vl2_%d' % mv]}},
        S(id=p + 'two view limits', view_ref='foo%d' % mv, expect={'count': 2}),
        {'view': {'id': p + 'negative multivalue limit', 'op': 'create', 'sheet': 'main', 'name': 'limit to view',
                  'filter': {'rules': [rule('enum1', 'foo1', 'not_equal')]}, 'as': 'vl3_%d' % mv}},
        {'user': {'op': 'view_limits', 'user': 'normal1', 'views': ['vl3_%d' % mv]}},
        {'view': {'id': p + 'date1 view', 'op': 'create', 'sheet': 'main', 'name': 'date1',
                  'filter': {'rules': [rule('date1', '2014-10-10')]}, 'as': 'vd%d' % mv}},
        S(id=p + 'negative multivalue limit with view', view_ref='vd%d' % mv, observe=['string1'], expect={'count': 1, 'ids': ['@any']}),
        S(id=p + 'quick search Foobar', view_ref='vd%d' % mv, search='Foobar', no_count=True, expect={'ids': ['@any']}),
        S(id=p + 'quick search -4', view_ref='vd%d' % mv, search='-4', no_count=True, expect={'ids': []}),
        S(id=p + 'quick search 8', view_ref='vd%d' % mv, search='8', no_count=True, expect={'ids': ['@any']}),
        S(id=p + 'quick search 5', view_ref='vd%d' % mv, search='5', no_count=True, expect={'ids': [7], 'first': 7}),
        {'user': {'op': 'view_limits', 'user': 'normal1', 'views': ['vl%d' % mv]}},
        S(id=p + 'quick search Foobar normal limit', view_ref='vd%d' % mv, search='Foobar', no_count=True, expect={'ids': []}),
        S(id=p + 'quick search 8 normal limit', view_ref='vd%d' % mv, search='8', no_count=True, expect={'ids': []}),
        S(id=p + 'quick search 5 normal limit', view_ref='vd%d' % mv, search='5', no_count=True, expect={'ids': ['@any']}),
        {'view': {'id': p + 'limit by enumval', 'op': 'update', 'sheet': 'main', 'ref': 'vl%d' % mv,
                  'filter': {'rules': [rule('enum1', 'foo2')]}}},
        S(id=p + 'limit with enumval', expect={'count': 2}),
        R(id=p + 'find in enumval limit', current_id=7, expect={'current_id': 7}),
        S(id=p + 'quick search date with enumval limit', search='2014-10-10', no_count=True, expect={'ids': ['@any']}),
        {'view': {'id': p + 'limit by curval', 'op': 'update', 'sheet': 'main', 'ref': 'vl%d' % mv,
                  'filter': {'rules': [rule('curval1', '1')]}}},
        S(id=p + 'limit with curval', observe=['string1', 'curval1'], expect={'count': 1, 'ids': ['@any']}),
        R(id=p + 'find in curval limit', current_id=3, expect={'current_id': 3}),
        S(id=p + 'quick search foo1 with curval limit', search='foo1', no_count=True, expect={'ids': ['@any']}),
        {'view': {'id': p + 'limit by curval subfield and enum', 'op': 'update', 'sheet': 'main', 'ref': 'vl%d' % mv,
                  'filter': {'rules': [rule('enum1', 'foo1'), rule('curval1_enum1', 'foo1')], 'operator': 'AND'}}},
        S(id=p + 'limit with curval subfield', expect={'count': 1, 'ids': ['@any']}),
        S(id=p + 'quick search with curval subfield limit', search='foo1', rows=50, expect={'pages': 1, 'ids': ['@any']}),
        {'user': {'op': 'view_limits', 'user': 'normal1', 'views': []}},
        S(id=p + 'quick search 2014-10-10', search='2014-10-10', no_count=True, expect={'ids': ['@any'] * 4}),
        S(id=p + 'quick search Foo', search='Foo', no_count=True, expect={'ids': ['@any'] * 5}),
        S(id=p + 'quick search 99', search='99', no_count=True, expect={'ids': ['@any'] * 2}),
        S(id=p + 'quick search invalid date', search='1979-01-204', no_count=True, expect={'ids': []}),
        R(id=p + 'find history record 3', record_id=3, expect={'record_id': 3}),
        R(id=p + 'find current 3', current_id=3, expect={'current_id': 3}),
        R(id=p + 'find history record 1 other sheet', record_id=1, observe=None, expect={'record_id': 1}),
        R(id=p + 'find current 1 other sheet', current_id=1, observe=None, expect={'current_id': 1}),
        R(id=p + 'find missing version', record_id=100, expect_error='Record version ID 100 not found'),
        R(id=p + 'find missing record', current_id=100, expect_error='Record ID 100 not found'),
        R(id=p + 'find invalid record', current_id='XYXY', expect_error='Invalid record ID'),
        R(id=p + 'find invalid record 2', current_id='123XYXY', expect_error='Invalid record ID'),
    ]
    for st in steps:
        r = st.get('record')
        if r is not None and r.get('observe', 1) is None:
            del r['observe']
    return steps


def view_limit_extra_scenario():
    data = [{'string1': 'FooBar', 'integer1': 50}, {'string1': 'Bar', 'integer1': 100},
            {'string1': 'Foo', 'integer1': 150}, {'string1': 'FooBar', 'integer1': 200}]
    S = lambda **kw: {'search': dict({'sheet': 'main', 'observe': ['string1'], 'single': True}, **kw)}
    steps = [
        {'sheet': {'as': 'main', 'data': data}},
        {'view': {'op': 'create', 'sheet': 'main', 'name': 'Limit to view extra', 'is_admin': 1,
                  'filter': {'rules': [rule('string1', 'FooBar')]}, 'as': 'extra1'}},
        {'view': {'op': 'create', 'sheet': 'main', 'name': 'Limit to view extra', 'is_admin': 1,
                  'filter': {'rules': [rule('integer1', '75', 'greater')]}, 'as': 'extra2'}},
        {'layout': {'op': 'update', 'sheet': 'main', 'default_view_limit_extra': 'extra1'}},
        S(id='default view limit extra', expect={'count': 2, 'rows': [{'string1': 'FooBar'}, {'string1': 'FooBar'}]}),
        S(id='changed view limit extra', view_limit_extra='extra2', expect={'count': 3, 'rows': [{'string1': 'Bar'}, {'string1': 'Foo'}, {'string1': 'FooBar'}]}),
        {'user': {'op': 'view_limits', 'user': 'superadmin', 'views': ['extra1']}},
        S(id='view limits and extra limits', view_limit_extra='extra2', expect={'count': 1, 'rows': [{'string1': 'FooBar'}]}),
        {'user': {'op': 'view_limits', 'user': 'superadmin', 'views': []}},
        {'sheet': {'as': 'main2', 'instance_id': 2, 'data': [], 'curval': 'main', 'multivalue': True}},
    ]
    for test, user in (('admin', 'superadmin'), ('normal', 'normal1')):
        steps += [
            {'column': {'id': 'filtered values %s' % test, 'op': 'get', 'sheet': 'main2', 'name': 'curval1', 'layout_user': True,
                        'user': user, 'filtered_values': True, 'expect': {'filtered_values': ['@any'] * 4}}},
            {'record': {'id': 'write curval %s' % test, 'op': 'create', 'sheet': 'main2', 'user': user, 'layout_user': True,
                        'values': {'curval1': [1, 2, 3], 'string1': test}, 'as': 'r_' + test}},
            {'record': {'id': 'curval contains all %s' % test, 'op': 'get', 'sheet': 'main2', 'user': user, 'layout_user': True,
                        'current_id': '@record:r_' + test, 'datum_values': ['curval1'], 'observe': ['curval1'],
                        'expect': {'datum_values': {'curval1': ['@any'] * 3}}}},
        ]
    steps += [
        {'view': {'op': 'create', 'sheet': 'main2', 'name': 'Limit to view extra', 'is_admin': 1,
                  'filter': {'rules': [rule('string1', 'normal')]}, 'write_options': {'force': 1}, 'as': 'extra_autocur'}},
        {'layout': {'op': 'update', 'sheet': 'main2', 'default_view_limit_extra': 'extra_autocur'}},
        {'add_autocur': {'sheet': 'main', 'refers_to': 'main2', 'related_field': 'curval1', 'curval_fields': ['string1']}},
    ]
    for test, user in (('admin', 'superadmin'), ('normal', 'normal1')):
        steps.append({'record': {'id': 'autocur contains all %s' % test, 'op': 'get', 'sheet': 'main', 'user': user, 'layout_user': True,
                                 'current_id': 1, 'datum_values': ['autocur1'], 'observe': ['string1', 'autocur1'],
                                 'expect': {'datum_values': {'autocur1': ['@any'] * 2}}}})
    return steps


SORTS = [
    dict(name='Sort by ID descending', show=['string1', 'enum1'], sort_by=[None], types=['desc'], first='^9$', last='^3$'),
    dict(name='Sort by single column in view ascending', show=['string1', 'enum1'], sort_by=['enum1'], types=['asc'], first='^(8|9)$', last='^(6|7)$'),
    dict(name='Sort by single column not in view ascending', show=['string1', 'tree1'], sort_by=['enum1'], types=['asc'], first='^(8|9)$', last='^(6|7)$'),
    dict(name='Sort by single column not in view descending', show=['string1', 'tree1'], sort_by=['enum1'], types=['desc'], first='^(6|7)$', last='^(8|9)$'),
    dict(name='Sort by single column not in view ascending (opposite enum columns)', show=['string1', 'enum1'], sort_by=['tree1'], types=['asc'], first='^(8|9)$', last='^(4)$'),
    dict(name='Sort by two columns, one in view one not in view, asc then desc', show=['string1', 'tree1'], sort_by=['enum1', 'daterange1'], types=['asc', 'desc'], first='^(8|9)$', last='^(7)$'),
    dict(name='Sort with filter on enums', show=['enum1', 'curval1', 'tree1'], sort_by=['enum1'], types=['asc'], first='^(3)$',
         first_string={'curval1': ''}, last='^(6)$',
         last_string={'curval1': 'Foo, 50, foo1, , 2014-10-10, 2012-02-10 to 2013-06-15, , , c_amber, 2012'},
         max_id=6, min_id=3, count=2, filter={'rules': [rule('tree1', 'tree1')]}),
    dict(name='Sort with filter on enums - opposite filter/sort combo', show=['enum1', 'curval1', 'tree1'], sort_by=['tree1'], types=['asc'],
         first='^(3)$', last='^(4)$', max_id=5, min_id=3, count=3, filter={'rules': [rule('enum1', 'foo1')]}),
    dict(name='Sort with filter on tree', show=['string1', 'enum1'], sort_by=['enum1'], types=['asc'], first='^(3)$', last='^(6)$',
         max_id=6, min_id=3, count=2, filter={'rules': [rule('tree1', 'tree1')]}),
    dict(name='Sort by enum that is after another enum in the fetched column', show=['enum1', 'curval1', 'tree1'], sort_by=['tree1'], types=['asc'], first='^(8|9)$', last='^(4)$'),
    dict(name='Sort by enum with filter on curval', show=['enum1', 'curval1', 'tree1'], sort_by=['enum1'], types=['asc'], first='^(4|5)$', last='^(7)$',
         max_id=7, min_id=4, count=3, filter={'rules': [rule('curval1_enum1', 'foo2'), rule('curval2_enum1', 'foo1')], 'condition': 'OR'}),
    dict(name='Sort by curval with filter on curval', show=['enum1', 'curval1', 'curval2'], sort_by=['curval1'], types=['asc'], first='^(4|5)$', last='^(3)$',
         max_id=5, min_id=3, count=3, filter={'rules': [rule('curval1_enum1', 'foo1'), rule('curval1_enum1', 'foo2')], 'condition': 'OR'}),
    dict(name='Sort by field in curval', show=['enum1', 'curval1', 'curval2'], sort_by=['curval1_string1'], types=['asc'], first='^(6|7|8|9)$', last='^(3)$',
         max_id=9, min_id=3, count=7),
    dict(name='Sort by curval without curval in view', show=['string1'], sort_by=['curval1'], types=['asc'], first='^(6|7|8|9)$', last='^(3)$', max_id=9, min_id=3, count=7),
    dict(name='Sort by field in curval without curval in view', show=['integer1'], sort_by=['curval1_string1'], types=['asc'], first='^(6|7|8|9)$', last='^(3)$',
         max_id=9, min_id=3, count=7),
]


def sort_scenario(mv):
    data = [dict(d) for d in DATA]
    data[3]['enum1'] = 'foo2'
    steps = [
        {'sheet': {'as': 'curval', 'instance_id': 2}},
        {'sheet': {'as': 'main', 'instance_id': 1, 'data': data, 'curval': 'curval', 'multivalue': bool(mv),
                   'calc_code': CALC_CODE, 'calc_return_type': 'date', 'column_count': {'enum': 1, 'curval': 2}}},
        {'add_autocur': {'sheet': 'curval', 'refers_to': 'main', 'related_field': 'curval1'}},
        {'add_autocur': {'sheet': 'curval', 'refers_to': 'main', 'related_field': 'curval2'}},
        {'sheet': {'as': 'curval2', 'instance_id': 3, 'curval_offset': 12}},
        {'column': {'op': 'create', 'sheet': 'curval', 'type': 'curval', 'name': 'curval3', 'refers_to': 'curval2', 'curval_fields': ['string1']}},
        {'record': {'op': 'update', 'sheet': 'curval', 'user': 'normal1', 'current_id': 1, 'values': {'curval3': 10}}},
        {'search': {'id': 'default_sort asc', 'sheet': 'main', 'user': 'normal1', 'default_sort': {'type': 'asc', 'name': '_id'}, 'no_count': True,
                    'expect': {'first': 3, 'last': 9}}},
        {'search': {'id': 'default_sort desc', 'sheet': 'main', 'user': 'normal1', 'default_sort': {'type': 'desc', 'name': '_id'}, 'no_count': True,
                    'expect': {'first': 9, 'last': 3}}},
        {'search': {'id': 'default_sort integer1', 'sheet': 'main', 'user': 'normal1', 'default_sort': {'type': 'desc', 'name': 'integer1'}, 'no_count': True,
                    'expect': {'first': 6, 'last': 7}}},
        {'search': {'id': 'standard sort', 'sheet': 'main', 'user': 'normal1', 'sort': [{'name': 'integer1', 'type': 'desc'}], 'no_count': True,
                    'expect': {'first': 6, 'last': 7}}},
        {'search': {'id': 'invalid sort falls back to default', 'sheet': 'main', 'user': 'normal1', 'sort': [{'id': -1000, 'type': 'desc'}],
                    'default_sort': {'type': 'desc', 'name': 'integer1'}, 'no_count': True, 'expect': {'first': 6, 'last': 7}}},
    ]
    for srt in SORTS:
        flt = srt.get('filter')
        sort_by = [x if x else '_id' for x in srt['sort_by']]
        for pss in (1, 2, 3, 4):
            if pss == 4 and not srt.get('count'):
                continue
            if len(srt['types']) > 1:
                types = srt['types']
            elif pss == 3:
                types = ['desc' if srt['types'][0] == 'asc' else 'asc']
            else:
                types = srt['types']
            view = {'columns': srt['show'], 'sorts': [{'name': n, 'type': t} for n, t in zip(sort_by, types + [types[-1]] * len(sort_by))]}
            if flt:
                view['filter'] = flt
            base = {'sheet': 'main', 'user': 'normal1', 'view': view, 'observe': srt['show'], 'page': 1}
            name = '%s (pass %d)' % (srt['name'], pss)
            if pss == 1:
                ov = dict(base, sort=[{'name': '_id', 'type': 'desc'}], rows=1, no_count=True)
                e = {'first': srt.get('max_id', 9)}
                steps.append({'search': dict(ov, id=name + ' override first', expect=e)})
                if srt.get('first_string'):
                    steps[-1]['search']['expect_first'] = srt['first_string']
                ov2 = dict(ov, page=srt.get('count', 7))
                steps.append({'search': dict(ov2, id=name + ' override last', expect={'last': srt.get('min_id', 3)})})
                if srt.get('last_string'):
                    steps[-1]['search']['expect_first'] = srt['last_string']
            elif pss in (2, 3):
                if pss == 3 and len(srt['types']) > 1:
                    continue
                first, last = (srt['first'], srt['last']) if pss == 2 else (srt['last'], srt['first'])
                e = {'first': '~' + first, 'last': '~' + last}
                steps.append({'search': dict(base, id=name + ' full page', no_count=True, expect=e)})
                if srt.get('count'):
                    steps[-1]['search']['expect']['ids'] = ['@any'] * srt['count']
                steps.append({'search': dict(base, id=name + ' first of 1 per page', rows=1, no_count=True, expect={'first': '~' + first})})
                steps.append({'search': dict(base, id=name + ' last of 1 per page', rows=1, page=srt.get('count', 7), no_count=True, expect={'first': '~' + last})})
            else:
                steps.append({'search': dict(base, id=name + ' count', no_results=True, expect={'count': srt['count']})})
    return steps


def main():
    write_scenario(os.path.join(OUT, '003_search_view_limits.yaml'), {
        '_generator': 'generators/gen_003_search.py',
        'name': '003_search_view_limits',
        'source': ['t/003_search.t'],
        'description': 'View limits for tables and users, record retrieval within limits, quick search',
        'fixed_time': '2014-10-10 01:00:00',
        'steps': setup_steps() + view_limit_steps(0) + view_limit_steps(1),
    })
    write_scenario(os.path.join(OUT, '003_search_view_limit_extra.yaml'), {
        '_generator': 'generators/gen_003_search.py',
        'name': '003_search_view_limit_extra',
        'source': ['t/003_search.t'],
        'description': 'Extra view limits (default_view_limit_extra) and their interaction with curvals and autocurs',
        'fixed_time': '2014-10-10 01:00:00',
        'steps': view_limit_extra_scenario(),
    })
    for mv in (0, 1):
        write_scenario(os.path.join(OUT, '003_search_sorts_mv%d.yaml' % mv), {
            '_generator': 'generators/gen_003_search.py',
            'name': '003_search_sorts_mv%d' % mv,
            'source': ['t/003_search.t'],
            'description': 'Sorting by columns in and out of the view, curval fields, with filters and paging (multivalue=%d)' % mv,
            'fixed_time': '2014-10-10 01:00:00',
            'steps': sort_scenario(mv),
        })
    steps = setup_steps()
    for mv in (0, 1):
        steps += filter_steps(mv)
    write_scenario(os.path.join(OUT, '003_search_filters.yaml'), {
        '_generator': 'generators/gen_003_search.py',
        'name': '003_search_filters',
        'source': ['t/003_search.t'],
        'description': 'Filter rules of every operator and column type, with sorting and aggregates, for single and multi-value fields',
        'fixed_time': '2014-10-10 01:00:00',
        'steps': steps,
    })


if __name__ == '__main__':
    main()
