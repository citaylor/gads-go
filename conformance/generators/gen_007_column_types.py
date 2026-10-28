#!/usr/bin/env python3
"""Generate scenarios/007_column_types.yaml from t/007_column_types.t"""
import yaml, os

data = [
    dict(string1='Foo', integer1='100', enum1=[7, 8], tree1=10, date1='2010-10-10',
         daterange1=['2000-10-10', '2001-10-10'], curval1=1, file1=None),
    dict(string1='Bar', integer1='200', enum1=8, tree1=11, date1='2011-10-10',
         daterange1=['2000-11-11', '2001-11-11'], curval1=2, file1=None),
]
data2 = [
    dict(string1='Foo', integer1=50, date1='2014-10-10', daterange1=['2012-02-10', '2013-06-15'], enum1=1),
    dict(string1='Bar', integer1=99, date1='2009-01-02', daterange1=['2008-05-04', '2008-07-14'], enum1=2),
    dict(string1='Bar', integer1=99, date1='2009-01-02', daterange1=['2008-05-04', '2008-07-14'], enum1=''),
    dict(string1='FooBar', integer1=150, date1='2000-01-02', daterange1=['2001-05-12', '2002-03-22'], enum1=3),
]
calc = '''function evaluate (L1string1)
    if type(L1string1) == "table" then
        L1string1 = L1string1[1]
    end
    return L1string1
end'''

steps = []
S = steps.append
U = 'normal1'

def col(op, **kw):
    d = {'op': op}
    d.update(kw)
    S({'column': d})

def fv(label, sheet='main', all_values=False, **kw):
    d = dict(id=label, op='get', sheet=sheet, filtered_values=True, **kw)
    if all_values:
        d['all_values'] = True
    S({'column': d})

def rule(name, value=None, op='equal'):
    r = {'name': 'cv:' + name, 'type': 'string', 'operator': op}
    if value is not None:
        r['value'] = value
    return r

S({'sheet': {'as': 'cv', 'instance_id': 2, 'data': data2}})
S({'sheet': {'as': 'main', 'data': data, 'multivalue': 1, 'curval': 'cv', 'curval_fields': ['string1'],
             'calc_code': calc, 'calc_return_type': 'string'}})

col('update', id='calc invalid character', sheet='main', column='calc1',
    set={'code': 'function evaluate (_id) return "test“test" end'}, expect_error='.')
S({'clear_layouts': {}})

S({'held': {'op': 'find', 'as': 'p', 'sheet': 'cv', 'current_id': 1}})
S({'held': {'op': 'delete', 'as': 'p'}})
S({'held': {'id': 'purge referenced', 'op': 'purge', 'as': 'p',
            'expect_error': 'The following records refer to this record as a value'}})
S({'held': {'op': 'restore', 'as': 'p'}})

fv('curval values', column='curval1', all_values=True)

S({'sheet': {'as': 'cs2', 'curval': 'main', 'instance_id': 3, 'curval_offset': 12}})
fv('second curval sheet values', sheet='cs2', column='curval1')

env = {'GADS_ALLOW_BLANK_CURVAL': 1}
col('create', id='curval blank', sheet='main', type='curval', name='curval blank', refers_to='cv',
    curval_fields=[], env=env, **{'as': 'cb'})
S({'clear_layouts': {}})
fv('curval blank values', column='@col:cb')
# (named differently to the first, which the original also names "curval
# blank", so that columns can be found by name)
col('create', id='curval blank filter', sheet='main', type='curval', name='curval blank filter', refers_to='cv',
    curval_fields=[], env=env, **{'as': 'cbf'})
S({'clear_layouts': {}})
S({'db': {'op': 'update', 'table': 'layout', 'where': {}, 'set': {'filter': None}}})
fv('undefined filter values', column='@col:cbf')

S({'db': {'id': 'curval fields at start', 'op': 'count', 'table': 'curval_fields'}})
col('create', sheet='main', type='curval', name='curval fields', refers_to='cv',
    curval_fields=['string1'], **{'as': 'caf'})
S({'db': {'id': 'curval fields after new', 'op': 'count', 'table': 'curval_fields'}})
col('update', sheet='main', column='@col:caf', set={'curval_fields': ['string1', 'integer1']})
S({'db': {'id': 'curval fields after addition', 'op': 'count', 'table': 'curval_fields'}})
col('update', sheet='main', column='@col:caf', set={'curval_fields': ['integer1']})
S({'db': {'id': 'curval fields after removal', 'op': 'count', 'table': 'curval_fields'}})
col('delete', sheet='main', column='@col:caf')

col('create', sheet='main', type='curval', name='curval filter', refers_to='cv',
    curval_fields=['integer1'], filter={'rules': [rule('string1', 'Foo')]}, **{'as': 'cf'})
S({'clear_layouts': {}})
fv('curval filter values', column='@col:cf', all_values=True)

# Legacy value retained after filter change
S({'held': {'op': 'single', 'as': 's', 'sheet': 'main'}})
S({'held': {'op': 'set', 'as': 's', 'values': {'@col:cf': 1}}})
S({'held': {'op': 'write', 'as': 's'}})
col('update', sheet='main', column='@col:cf', set={'filter': {'rules': [rule('string1', 'Bar')]}})
S({'clear_layouts': {}})
fv('curval filter values after change', column='@col:cf')
S({'held': {'id': 'legacy value', 'op': 'find', 'as': 's', 'sheet': 'main', 'clear': True,
            'current_id': '@held:s', 'datum': ['@col:cf'], 'datum_props': ['as_string', 'id', 'ids']}})
col('update', sheet='main', column='@col:cf', set={'multivalue': 1})
S({'clear_layouts': {}})
S({'held': {'id': 'legacy value multiple', 'op': 'single', 'as': 's2', 'sheet': 'main',
            'datum': ['@col:cf'], 'datum_props': ['as_string', 'ids'],
            'for_code_curval': '@col:cf', 'for_code_fields': ['L2enum1']}})

# View limits
S({'add_autocur': {'id': 'autocur', 'sheet': 'cv', 'refers_to': 'main', 'related_field': 'curval1'}})
S({'layout': {'op': 'set_user', 'sheet': 'main', 'user': U}})
S({'layout': {'op': 'set_user', 'sheet': 'cv', 'user': U}})
S({'clear_layouts': {}})
fv('filtered before view limit', column='@col:cf')
S({'view_held': {'op': 'new', 'as': 'vl', 'sheet': 'cv', 'user': U, 'name': 'Limit to view',
                 'filter': {'rules': [{'name': 'enum1', 'type': 'string', 'value': 'foo2', 'operator': 'equal'}]}}})
S({'view_held': {'op': 'write', 'as': 'vl', 'view_as': 'vl'}})
S({'user': {'op': 'view_limits', 'user': U, 'views': ['vl']}})
S({'clear_layouts': {}})
fv('filtered after view limit', column='@col:cf', all_values=True)
col('update', sheet='main', column='@col:cf', set={'override_permissions': 1})
S({'clear_layouts': {}})
fv('filtered with override', column='@col:cf')
S({'view_held': {'op': 'new', 'as': 'vl2', 'sheet': 'main', 'user': U, 'name': 'Limit to view',
                 'filter': {'rules': [{'name': 'enum1', 'type': 'string', 'value': 'foo3', 'operator': 'equal'}]}}})
S({'view_held': {'op': 'write', 'as': 'vl2', 'view_as': 'vl2'}})
S({'user': {'op': 'view_limits', 'user': U, 'views': ['vl2']}})
S({'record': {'id': 'autocur with limited record', 'op': 'get', 'sheet': 'cv', 'user': U,
              'current_id': 1, 'observe': ['autocur1']}})
S({'search': {'id': 'sorted view with autocur', 'sheet': 'cv', 'user': U,
              'view': {'name': 'Sorted view with autocur', 'columns': ['autocur1'],
                       'sorts': [{'name': 'autocur1_enum1', 'type': 'asc'}]},
              'observe': ['autocur1']}})
S({'user': {'op': 'view_limits', 'user': U, 'views': []}})
S({'clear_layouts': {}})

S({'layout': {'op': 'position', 'sheet': 'main', 'columns': [
    'integer1', 'date1', 'daterange1', 'enum1', 'tree1', 'curval1', '@col:cf', 'string1']}})
col('create', sheet='main', type='calc', name='calc2', name_short='L1calc2', return_type='string',
    code='function evaluate (_id, L1date1, L1daterange1) \n return {"Foo", "Bar"} \nend', multivalue=1)
col('update', sheet='main', column='date1', set={'display_fields': {
    'condition': None, 'rules': [{'name': 'string1', 'operator': 'equal', 'value': 'Foo'}]}})
S({'clear_layouts': {}})
col('hold', sheet='main', column='@col:cf', **{'as': 'cf'})

for test in ['string1', 'enum1', 'calc1', 'integer1', 'multi', 'negative', 'nomatch', 'invalid',
             'calcmulti', 'displayfield']:
    if test in ('string1', 'enum1', 'calc1', 'integer1'):
        field = test
    elif test == 'calcmulti':
        field = 'string1'
    elif test in ('multi', 'negative'):
        field = 'enum1'
    elif test == 'displayfield':
        field = 'date1'
    else:
        field = 'string1'
    match = test in ('string1', 'enum1', 'calc1', 'integer1', 'multi', 'negative')
    value = {'calc1': '$L1calc1', 'integer1': '$L1integer1', 'calcmulti': '$L1calc2'}.get(test)
    if value is None:
        if match:
            value = '$L1' + field
        elif test == 'nomatch':
            value = '$L1tree1'
        elif test == 'displayfield':
            value = '$L1date1'
        else:
            value = '$L1string123'
    if test == 'multi':
        rules = {'rules': [rule(field, value), rule(field, op='is_empty')], 'condition': 'OR'}
    elif test == 'negative':
        rules = {'rules': [rule(field, value, 'not_equal')]}
    elif test in ('calc1', 'calcmulti'):
        rules = {'rules': [rule('string1', value)]}
    elif test == 'integer1':
        rules = {'rules': [rule('integer1', value, 'less')]}
    elif test == 'displayfield':
        rules = {'rules': [rule('date1', value, 'greater')]}
    else:
        rules = {'rules': [rule(field, value)]}
    T = 'filter ' + test + ': '
    col('update', sheet='main', held_col='cf', set={'filter': rules, 'curval_fields': ['string1']})
    col('clear', sheet='main', held_col='cf')
    S({'column': {'id': T + 'input required', 'op': 'get', 'sheet': 'main', 'held_col': 'cf',
                  'subvals_input_required': True}})
    S({'clear_layouts': {}})
    a = 't_' + test
    S({'held': {'op': 'find', 'as': a, 'sheet': 'main', 'current_id': 5}})
    written = 'string1' if field == 'calc1' else field
    S({'held': {'op': 'clone_oldvalue', 'as': a, 'columns': [written]}})
    S({'held': {'op': 'write', 'as': a, 'options': {'dry_run': 1, 'missing_not_fatal': 1,
                                                    'submitted_fields_of': '@col:cf'}}})
    S({'column': {'id': T + 'filtered for record', 'op': 'get', 'sheet': 'main', 'held_col': 'cf',
                  'filtered_values': True}})
    S({'clear_layouts': {}})
    S({'held': {'op': 'new', 'as': a + '_new', 'sheet': 'main'}})
    S({'held': {'id': T + 'filtered for new record', 'op': 'get', 'as': a + '_new',
                'filtered_values': '@col:cf'}})
    col('delete', sheet='main', held_col='cf')

S({'search': {'id': 'curval text without building values', 'sheet': 'main', 'observe': ['curval1'],
              'env': {'PANIC_ON_CURVAL_BUILD_VALUES': 1}}})
S({'clear_layouts': {}})

# Tree values
S({'db': {'id': 'tree values at start', 'op': 'count', 'table': 'enumval',
          'where': {'layout_id': '@column:main:tree1', 'deleted': 0}}})
col('update', sheet='main', column='tree1', set={'tree': [
    {'text': 'tree1'}, {'text': 'tree2', 'children': [{'text': 'tree3'}]}, {'text': 'tree4'}]})
S({'db': {'id': 'tree values after addition', 'op': 'count', 'table': 'enumval',
          'where': {'layout_id': '@column:main:tree1', 'deleted': 0}}})
col('update', sheet='main', column='tree1', set={'tree': [{'text': 'tree4'}]})
S({'db': {'id': 'tree values after removal', 'op': 'count', 'table': 'enumval',
          'where': {'layout_id': '@column:main:tree1', 'deleted': 0}}})

S({'layout': {'id': 'write internal columns', 'op': 'write_internal', 'sheet': 'main'}})

col('delete', sheet='cs2', column='curval1')
col('delete', sheet='cv', column='autocur1')
S({'layout': {'id': 'delete all columns', 'op': 'delete_all_columns', 'sheet': 'main'}})

# Same again with all fields of type string
# (the original never creates their records, nor the curval sheet's columns)
S({'sheet': {'as': 'cv2', 'instance_id': 2, 'build_columns': False, 'create_records': False}})
S({'sheet': {'as': 'main2', 'data': data, 'curval': 'cv2', 'create_records': False}})
S({'db': {'op': 'update', 'table': 'layout', 'where': {}, 'set': {'type': 'string'}}})
S({'clear_layouts': {}})
S({'layout': {'id': 'delete all string columns', 'op': 'delete_all_columns', 'sheet': 'main2'}})

out = {'name': '007_column_types', 'source': ['t/007_column_types.t'],
       'description': 'Curval values and filters, tree values, internal columns and column deletion',
       'steps': steps}
p = os.path.join(os.path.dirname(__file__), '..', 'scenarios', '007_column_types.yaml')
with open(p, 'w') as f:
    yaml.safe_dump(out, f, sort_keys=False, allow_unicode=True)
