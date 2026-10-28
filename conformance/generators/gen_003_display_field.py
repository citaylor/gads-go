#!/usr/bin/env python3
"""Generate scenarios/003_display_field.yaml from t/003_display_field.t"""
import yaml, os

steps = []
S = steps.append

def held(op, as_, **kw):
    d = {'op': op, 'as': as_}
    d.update(kw)
    S({'held': d})

def display(column, rules, condition=None, sheet='main'):
    S({'column': {'op': 'update', 'sheet': sheet, 'column': column,
                  'set': {'display_fields': {'condition': condition, 'rules': rules}}}})
    S({'clear_layouts': {}})

def r1(name, value, op='equal'):
    return {'name': name, 'operator': op, 'value': value}

def reload(as_, label, observe, cid=3):
    held('find', as_, id=label, sheet='main', clear=True, current_id=cid, observe=observe)

S({'sheet': {'as': 'cv', 'instance_id': 2}})
S({'sheet': {'as': 'main', 'curval': 'cv', 'curval_fields': ['string1'], 'multivalue': 1,
             'multivalue_columns': ['string', 'tree'], 'column_count': {'integer': 2}}})
display('integer1', [r1('string1', 'foobar')])
held('find', 'r', id='initial', sheet='main', current_id=3, observe=['string1', 'integer1'])

types = [
    dict(type='equal', normal='foobar', blank='xxfoobarxx'),
    dict(type='contains', normal='xxfoobarxx', blank='foo'),
    dict(type='not_equal', normal='foo', blank='foobar'),
    dict(type='not_contains', normal='foo', blank='xxfoobarxx'),
    dict(type='equal', normal=['foo', 'bar', 'foobar'], blank=['xxfoobarxx', 'abc']),
    dict(type='contains', normal=['foo', 'bar', 'xxfoobarxx'], blank='fo'),
    dict(type='not_equal', normal=['foo', 'foobarx'], blank=['foobar', 'foobar2']),
    dict(type='not_contains', normal=['fo'], blank=['foo', 'bar', 'xxfoobarxx']),
]
for i, t in enumerate(types):
    T = 'single %d %s: ' % (i, t['type'])
    display('integer1', [r1('string1', 'foobar', t['type'])])
    reload('r', None, None)
    held('set', 'r', values=[{'string1': t['normal']}, {'integer1': '150'}])
    held('write', 'r')
    reload('r', T + 'normal', ['string1', 'integer1'])
    held('set', 'r', values=[{'string1': t['blank']}, {'integer1': '200'}])
    held('write', 'r')
    reload('r', T + 'blank', ['string1', 'integer1'])

multi = [
    ('AND', [dict(blank={'string1': 'xxfoobarxx', 'enum1': 8}, normal={'string1': 'foobar', 'enum1': 7}),
             dict(blank={'string1': 'foobar', 'enum1': 8}),
             dict(blank={'string1': 'xxfoobarxx', 'enum1': 7})]),
    ('OR', [dict(normal={'string1': 'foobar', 'enum1': 7}, blank={'string1': 'xxfoobarxx', 'enum1': 8}),
            dict(normal={'string1': 'foobar', 'enum1': 8}),
            dict(normal={'string1': 'xxfoobarxx', 'enum1': 7})]),
]
for cond, values in multi:
    display('integer1', [r1('string1', 'foobar'), r1('enum1', 'foo1')], cond)
    reload('r', None, None)
    for j, v in enumerate(values):
        for kind, intval in (('normal', '150'), ('blank', '200')):
            if kind not in v:
                continue
            held('set', 'r', values=[{'string1': v[kind]['string1']}, {'enum1': v[kind]['enum1']},
                                     {'integer1': intval}])
            held('write', 'r')
            reload('r', 'multi %s %d %s' % (cond, j, kind), ['string1', 'integer1'])

display('integer1', [r1('string1', 'foobar')])

# Mandatory field not required if not shown
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'integer1', 'set': {'optional': 0}}})
S({'clear_layouts': {}})
held('new', 'm', sheet='main')
held('set', 'm', values=[{'string1': 'foobar'}, {'integer1': ''}])
held('write', 'm', id='shown mandatory blank', expect_error='is not optional')
held('set', 'm', values=[{'string1': 'foo'}, {'integer1': ''}])
held('write', 'm', id='hidden mandatory blank')
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'integer1', 'set': {'optional': 1}}})
S({'clear_layouts': {}})

fields = [
    ('string1', 'apples', 'foobar', 'apples'),
    ('enum1', 'foo3', 8, 9),
    ('tree1', 'tree1', 11, 10),
    ('integer2', '250', '240', '250'),
    ('curval1', 'Bar', 1, 2),
    ('date1', '2010-10-10', '2011-10-10', '2010-10-10'),
    ('daterange1', '2010-12-01 to 2011-12-02', ['2011-01-01', '2012-01-01'], ['2010-12-01', '2011-12-02']),
    ('person1', 'User1, User1', 2, 1),
]
for field, regex, blank, match in fields:
    display('integer1', [r1(field, regex)])
    a = 'f_' + field
    held('new', a, sheet='main')
    held('set', a, values=[{field: blank}, {'integer1': 838}])
    held('write', a, allow_error=True)
    held('find', a, id='field %s blank' % field, sheet='main', clear=True, current_id='@held:' + a,
         observe=['integer1'])
    held('clear', a)
    held('initialise', a)
    held('set', a, values=[{field: match}, {'integer1': 839}])
    held('write', a, allow_error=True)
    held('find', a, id='field %s match' % field, sheet='main', clear=True, current_id='@held:' + a,
         observe=['integer1'])

# Blank value match
display('integer1', [r1('string1', '')])
held('new', 'b1', sheet='main')
held('set', 'b1', values=[{'string1': ''}, {'integer1': 789}])
held('write', 'b1')
held('find', 'b1', id='blank regex match', sheet='main', clear=True, current_id='@held:b1', observe=['integer1'])
held('new', 'b2', sheet='main')
held('set', 'b2', values=[{'string1': 'foo'}, {'integer1': 234}])
held('write', 'b2')
held('find', 'b2', id='blank regex no match', sheet='main', clear=True, current_id='@held:b2', observe=['integer1'])

# Tree values
display('integer1', [r1('tree1', '(.*#)?tree3')])
reload('r', None, None)
for label, tree, intval in (('tree blank', 10, '250'), ('tree match', 12, '350'), ('tree multi', [10, 12], '360')):
    held('set', 'r', values=[{'tree1': tree}, {'integer1': intval}])
    held('write', 'r')
    reload('r', label, ['tree1', 'integer1'])
display('integer1', [r1('tree1', 'tree2#tree3')])
for label, tree, intval in (('tree full path', 12, '400'), ('tree full path reversed', 11, '500')):
    held('set', 'r', values=[{'tree1': tree}, {'integer1': intval}])
    held('write', 'r')
    reload('r', label, ['tree1', 'integer1'])
display('integer1', [r1('tree1', 'tree2#', 'contains')])
reload('r', None, None)
held('set', 'r', values=[{'tree1': 12}, {'integer1': '600'}])
held('write', 'r')
reload('r', 'tree higher level', ['tree1', 'integer1'])

# Fields not visible to the user
reload('r', None, None)
held('set', 'r', values={'string1': 'Foobar'})
held('write', 'r')
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'integer1',
              'set': {'display_fields': {'condition': None, 'rules': [r1('string1', 'Foobar')]}}}})
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'string1', 'set': {'permissions': {'default': []}}}})
S({'clear_layouts': {}})
reload('r', None, None)
held('set', 'r', values={'integer1': '250'})
held('write', 'r')
reload('r', 'depends on non-visible', ['integer1'])
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'integer1',
              'set': {'display_fields': {'condition': None, 'rules': [r1('string1', 'Foobar', 'not_equal')]}}}})
reload('r', None, None)
held('set', 'r', values={'integer1': '250'})
held('write', 'r')
reload('r', 'depends on non-visible reverse', ['integer1'])
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'integer1',
              'set': {'display_fields': {'condition': None, 'rules': [r1('string1', 'ABC')]},
                      'permissions': {'default': []}}}})
S({'clear_layouts': {}})
reload('r', None, None)
# Dependent fields without write access do not clear on update of their
# parent field: GADS-dev fails the write
held('set', 'r', values={'date1': '2018-06-01'})
held('write', 'r', id='no write access', expect_error='You do not have permission to edit field integer1')
reload('r', 'no write access unchanged', ['string1', 'integer1', 'date1'])
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'string1', 'set': {'permissions': 'default'}}})
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'integer1', 'set': {'permissions': 'default'}}})
S({'clear_layouts': {}})

# dependent_not_shown
display('integer1', [r1('string1', 'Foobar')])
held('new', 'd', sheet='main')
held('set', 'd', values=[{'string1': 'Foobar'}, {'integer1': '100'}])
held('write', 'd')
dns = dict(datum=['string1', 'integer1'], datum_props=['dependent_not_shown'])
held('find', 'd', id='dns shown', sheet='main', clear=True, current_id='@held:d', **dns)
held('set', 'd', values=[{'string1': 'Foo'}, {'integer1': '200'}])
held('write', 'd', id='dns not shown', **dns)
held('set', 'd', values=[{'string1': 'Foobarbar'}, {'integer1': '200'}])
held('write', 'd', id='dns not shown 2', **dns)
S({'search': {'id': 'dns in table view', 'sheet': 'main', 'columns': ['integer1'], 'single': True,
              'single_dependent_not_shown': 'integer1'}})
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'integer1', 'set': {'display_fields': None}}})
S({'clear_layouts': {}})

# dependent_not_shown within curval record, with mixed permissions
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'curval1', 'set': {'show_add': 1},
              'write_options': {'no_alerts': 1}}})
S({'clear_layouts': {}})
S({'column': {'op': 'create', 'sheet': 'cv', 'type': 'curval', 'refers_to': 'main',
              'curval_fields': ['string1'], 'name': 'Curval back to main table', 'name_short': 'L2curval1'}})
S({'clear_layouts': {}})
display('L2curval1', [r1('string1', 'Apple', 'contains')], sheet='cv')
held('new', 'c', sheet='main')
held('set', 'c', values=[{'string1': 'Master'},
                         {'curval1': ['@field:string1=Apple&@field:integer1=100&@field:L2curval1=4']}])
held('write', 'c')
held('find', 'c', id='curval master written', sheet='main', clear=True, current_id='@held:c',
     datum=['curval1'], datum_props=['as_string', 'ids'])
held('find', 'cr', id='curval record', sheet='cv', current_id=26,
     observe=['string1', 'integer1', 'L2curval1'])
S({'column': {'op': 'update', 'sheet': 'cv', 'column': 'L2curval1',
              'set': {'permissions': {'default': ['read', 'write_existing', 'write_existing_no_approval']}}}})
S({'clear_layouts': {}})
held('find', 'c', sheet='main', clear=True, current_id='@held:c')
held('set', 'c', values=[{'string1': 'Master2'},
                         {'curval1': [{'query': '@field:string1=Apples&@field:integer1=200&@field:L2curval1=3',
                                       'own_id': 'Apple'}]}])
held('write', 'c')
held('find', 'cr', id='curval record updated', sheet='cv', clear=True, current_id=26,
     observe=['string1', 'L2curval1'])
S({'column': {'op': 'update', 'sheet': 'cv', 'column': 'integer1', 'set': {'display_fields': None}}})
S({'clear_layouts': {}})

# Previously blank value need not be completed only if displayed
held('new', 'p', sheet='main')
held('set', 'p', values={'string1': 'Foo1'})
held('write', 'p')
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'integer1', 'set': {'optional': 0}}})
S({'clear_layouts': {}})
held('find', 'p', sheet='main', clear=True, current_id='@held:p')
held('set', 'p', values=[{'string1': 'Foo2'}, {'integer1': ''}])
held('write', 'p', id='no longer optional warning', expect_messages_match='no longer optional')
display('integer1', [r1('string1', 'Foobar')])
held('find', 'p', sheet='main', clear=True, current_id='@held:p')
held('set', 'p', values={'string1': 'Foo3'})
held('write', 'p', id='mandatory not shown')
held('find', 'p', sheet='main', clear=True, current_id='@held:p')
held('set', 'p', values=[{'string1': 'Foobar'}, {'integer1': ''}])
held('write', 'p', id='mandatory now shown', expect_error='is not optional')

# Recursive display fields
display('string1', [r1('string1', 'Foobar'), r1('date1', '2013-02-02')], 'OR')
held('new', 'q', sheet='main')
held('set', 'q', values=[{'string1': 'Foobar'}, {'date1': '2013-02-02'}, {'integer1': '1234'}])
held('write', 'q')
held('find', 'q', id='recursive initial', sheet='main', clear=True, current_id='@held:q', observe=['string1'])
held('set', 'q', values={'date1': '2013-02-01'})
held('write', 'q')
held('find', 'q', id='recursive first removed', sheet='main', clear=True, current_id='@held:q', observe=['string1'])
held('set', 'q', values={'string1': 'Foobar2'})
held('write', 'q')
held('find', 'q', id='recursive second removed', sheet='main', clear=True, current_id='@held:q', observe=['string1'])

S({'column': {'id': 'delete depended field', 'op': 'delete', 'sheet': 'main', 'column': 'string1',
              'expect_error': 'remove these conditions before deletion'}})
S({'column': {'id': 'delete independent field', 'op': 'delete', 'sheet': 'main', 'column': 'integer1'}})

# Remove find steps without an id label (reload only)
for s in steps:
    h = s.get('held')
    if h and 'id' in h and h['id'] is None:
        del h['id']
    if h and 'observe' in h and h['observe'] is None:
        del h['observe']

out = {'name': '003_display_field', 'source': ['t/003_display_field.t'],
       'description': 'Fields blanked when their display conditions are not met',
       'steps': steps}
p = os.path.join(os.path.dirname(__file__), '..', 'scenarios', '003_display_field.yaml')
with open(p, 'w') as f:
    yaml.safe_dump(out, f, sort_keys=False, allow_unicode=True)
