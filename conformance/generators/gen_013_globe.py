#!/usr/bin/env python3
"""Generate scenarios/013_globe.yaml from t/013_globe.t"""
import yaml, os

CALC = {'calc_code': "function evaluate (L1string1) \n return L1string1 end", 'calc_return_type': 'globe'}
steps = []
S = steps.append

simple = [
    dict(string1='France', integer1=10, enum1='foo2'),
    dict(string1='Great Britain', integer1=15, enum1='foo3'),
]

def globe(name_, **kw):
    d = {'id': name_, 'sheet': 'main'}
    d.update(kw)
    S({'globe': d})

# Simple test first
S({'sheet': dict(as_='main', data=simple, **CALC)})
globe('simple')

# With and without a view that doesn't contain a globe field, and no view
countries = ['Albania', 'Algeria', 'Andorra', 'Angola', 'Australia',
             'Bahamas', 'Bahrain', 'Barbados', 'Bermuda', 'Bolivia']
data = [dict(string1=countries[i % 10], integer1=10, enum1='foo%d' % (i % 3 + 1)) for i in range(1, 501)]
for withview in ('none', 'with', 'without'):
    S({'reset': {}})
    S({'sheet': dict(as_='main', data=data, **CALC)})
    kw = {}
    if withview != 'none':
        S({'view': {'op': 'create', 'sheet': 'main', 'name': 'Test view', 'as': 'v',
                    'columns': ['calc1'] if withview == 'with' else ['string1'],
                    'groups': ['string1']}})
        kw['view_ref'] = 'v'
    for test, opt in (('group', {'group': 'enum1'}), ('label', {'label': 'string1'}),
                      ('color', {'color': 'integer1'}), ('color_count', {'color': -1}),
                      ('group_numeric', {'group': 'integer1'})):
        globe('%s %s' % (withview, test), **opt, **kw)
    if withview == 'none':
        # Extra cases (not in the original): colour by text, numeric label
        # with grouping, label by count
        globe('none color_text', color='enum1')
        globe('none label_numeric_group', label='integer1', group='enum1')
        globe('none label_count', label=-1)
        # GADS-dev fails this case: the column is selected only once but
        # aggregated twice ("No such column 'field9'")
        globe('none color_label_same', color='integer1', label='integer1', group='enum1',
              expect_error="No such column 'field\\d+' on GADS::Schema::Result::Current")

# Count of curval numeric field
S({'reset': {}})
cdata = [
    dict(string1='Foo', integer1=250, enum1=['foo1', 'foo2']),
    dict(string1='FooBar', integer1=25, enum1=['foo2', 'foo3']),
    dict(string1='Bar', integer1=25, enum1=['foo1']),
    dict(string1='Foo', integer1=25, enum1=['foo2']),
]
S({'sheet': {'as': 'cv', 'data': cdata, 'multivalue': 1, 'instance_id': 2}})
# Change one of the curval records to check only the latest version is used
S({'held': {'op': 'find', 'as': 'c1', 'sheet': 'cv', 'current_id': 1}})
S({'held': {'op': 'set', 'as': 'c1', 'values': {'integer1': 25}}})
S({'held': {'op': 'write', 'as': 'c1', 'options': {'no_alerts': 1}}})
S({'sheet': dict(as_='main', data=[
    dict(string1='France', curval1=[1, 2]),
    dict(string1='Germany', curval1=3),
    dict(string1='Spain', curval1=4),
], curval='cv', multivalue=1, **CALC)})
globe('curval color', color=['curval1', 'cv:integer1'])

# Invalid columns
S({'reset': {}})
S({'sheet': dict(as_='main', data=simple, **CALC)})
for opt in ('group', 'label', 'color'):
    globe('invalid ' + opt, **{opt: 999})

for s in steps:
    sh = s.get('sheet')
    if sh and 'as_' in sh:
        sh['as'] = sh.pop('as_')

out = {'name': '013_globe', 'source': ['t/013_globe.t'],
       'description': 'Records plotted on a globe', 'steps': steps}
p = os.path.join(os.path.dirname(__file__), '..', 'scenarios', '013_globe.yaml')
with open(p, 'w') as f:
    yaml.safe_dump(out, f, sort_keys=False, allow_unicode=True)
