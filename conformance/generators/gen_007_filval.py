#!/usr/bin/env python3
"""Generate scenarios/007_filval.yaml from t/007_filval.t"""
import yaml, os

calc = ("function evaluate (_created)\n    if _created == nil then\n        return\n"
        "    end\n    return 1000\nend")
FV = 'curval filter storage'

data = [
    dict(string1='Foo', integer1=40, date1='2014-10-10', daterange1=['2012-02-10', '2013-06-15'], enum1=1),
    dict(string1='Foo', integer1=50, date1='2014-10-10', daterange1=['2012-02-10', '2013-06-15'], enum1=1),
    dict(string1='Foo', integer1=55, date1='2014-10-10', daterange1=['2012-02-10', '2013-06-15'], enum1=1),
    dict(string1='Bar', integer1=99, date1='2009-01-02', daterange1=['2008-05-04', '2008-07-14'], enum1=2),
    dict(string1='Bar', integer1=200, date1='2009-01-02', daterange1=['2008-05-04', '2008-07-14'], enum1=''),
    dict(string1='FooBar', integer1=150, date1='2000-01-02', daterange1=['2001-05-12', '2002-03-22'], enum1=3),
]

steps = [
    {'sheet': {'as': 'cv', 'instance_id': 2, 'data': data, 'calc_code': calc}},
    {'sheet': {'as': 'main', 'data': [], 'multivalue': 1, 'curval': 'cv',
               'curval_fields': ['string1'], 'calc_code': calc}},
    {'column': {'op': 'update', 'sheet': 'main', 'column': 'curval1', 'set': {'filter': {
        'rules': [
            {'name': 'string1', 'type': 'string', 'value': '$L1string1', 'operator': 'equal'},
            {'name': 'calc1', 'type': 'string', 'value': '$L1calc1', 'operator': 'equal'},
        ], 'condition': 'AND'}}}},
    {'column': {'op': 'create', 'sheet': 'main', 'type': 'filval', 'name': FV,
                'refers_to': 'cv', 'curval_fields': ['integer1'],
                'related_field': 'curval1'}},
]

tests = [
    dict(name='new', new=1, string_value='Bar', count_values=2, curval_value=4),
    dict(name='draft', new=1, draft=1, string_value='Bar', curval_value=4),
    dict(name='draft and no set value', new=1, draft=1, no_set_value=1, string_value='Bar', curval_value=4),
    dict(name='edit', string_value='Foo', curval_value=2),
    dict(name='unchanged, including curval', delete_value=2, string_value='Foo',
         date_value='2017-01-01', curval_value=2),
    dict(name='unchanged sub-values, changed curval', delete_value=2, string_value='Foo',
         date_value='2018-05-01', curval_value=1),
]

prev = None
for i, t in enumerate(tests):
    a = 'r%d' % i
    if t.get('delete_value'):
        steps.append({'record': {'op': 'delete', 'sheet': 'cv', 'current_id': t['delete_value']}})
    if t.get('new'):
        steps.append({'held': {'op': 'new', 'as': a, 'sheet': 'main'}})
    else:
        steps.append({'held': {'op': 'find', 'as': a, 'sheet': 'main', 'current_id': '@held:' + prev}})
    vals = {'string1': t['string_value']}
    if t.get('date_value'):
        vals['date1'] = t['date_value']
    steps.append({'held': {'op': 'set', 'as': a, 'values': vals}})
    if t.get('draft'):
        steps.append({'held': {'op': 'write', 'as': a, 'options': {'draft': 1}}})
        steps.append({'held': {'op': 'clear', 'as': a}})
        steps.append({'held': {'op': 'load_remembered_values', 'as': a}})
    else:
        steps.append({'held': {'id': t['name'] + ': filtered values', 'op': 'get', 'as': a,
                               'filtered_values': 'curval1', 'submission_token': True}})
    if not t.get('no_set_value'):
        steps.append({'held': {'op': 'set', 'as': a, 'values': {'curval1': [t['curval_value']]}}})
    steps.append({'held': {'op': 'write', 'as': a, 'options': {'submission_token': 1}}})
    steps.append({'held': {'id': t['name'], 'op': 'find', 'as': a, 'sheet': 'main', 'clear': True,
                           'current_id': '@held:' + a, 'observe': [FV, 'curval1', 'string1']}})
    if t.get('delete_value'):
        steps.append({'record': {'op': 'restore', 'sheet': 'cv', 'current_id': t['delete_value'],
                                 'find_options': {'deleted': 1}}})
    steps.append({'clear_layouts': {}})
    prev = a

steps.append({'search': {'id': 'search filval enum', 'sheet': 'main',
    'view': {'name': 'Test view', 'columns': ['string1']},
    'filter': {'rules': [{'name': FV + '_enum1', 'type': 'string', 'value': 'foo1', 'operator': 'equal'}]},
    'observe': ['string1']}})
steps.append({'search': {'id': 'search filval id', 'sheet': 'main',
    'view': {'name': 'Test view', 'columns': ['string1']},
    'filter': {'rules': [{'name': FV, 'type': 'string', 'value': '5', 'operator': 'equal'}]},
    'observe': ['string1']}})

# Display conditions
steps.append({'column': {'op': 'update', 'sheet': 'main', 'column': 'date1', 'set': {'display_fields': {
    'condition': 'AND', 'rules': [
        {'name': 'curval1', 'operator': 'not_contains', 'value': '40'},
        {'name': FV, 'operator': 'contains', 'value': '40'},
    ]}}}})
steps.append({'column': {'op': 'update', 'sheet': 'main', 'column': 'curval1',
                         'set': {'curval_fields': ['string1', 'integer1']}}})
steps.append({'clear_layouts': {}})
for i, (s, cv) in enumerate([('Foo', [1]), ('Foo', [2]), ('Foo', [1, 2]), ('Bar', [5])]):
    a = 'd%d' % i
    steps.append({'held': {'op': 'new', 'as': a, 'sheet': 'main'}})
    steps.append({'held': {'op': 'set', 'as': a, 'values': {'string1': s, 'date1': '2016-01-01', 'curval1': cv}}})
    steps.append({'held': {'op': 'write', 'as': a, 'options': {'submission_token': 1}}})
    steps.append({'held': {'id': 'display %d' % i, 'op': 'find', 'as': a, 'sheet': 'main', 'clear': True,
                           'current_id': '@held:' + a, 'observe': ['date1', FV]}})
    steps.append({'clear_layouts': {}})

out = {'name': '007_filval', 'source': ['t/007_filval.t'],
       'description': 'Filval fields store the filtered values of a curval field at submission',
       'steps': steps}
p = os.path.join(os.path.dirname(__file__), '..', 'scenarios', '007_filval.yaml')
with open(p, 'w') as f:
    yaml.safe_dump(out, f, sort_keys=False, allow_unicode=True)
