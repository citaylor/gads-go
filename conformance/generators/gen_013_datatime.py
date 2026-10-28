#!/usr/bin/env python3
"""Generate scenarios/013_datatime.yaml from t/013_datatime.t"""
import yaml, os, random
from datetime import date, timedelta

rnd = random.Random(1013)  # the original uses rand(), fixed here for parity
NOW = date(2008, 1, 1)     # fixed time 2008-01-01 01:00:00
ALL = ['string1', 'integer1', 'enum1', 'tree1', 'date1', 'daterange1', 'file1', 'person1',
       'curval1', 'rag1', 'calc1']

steps = []
S = steps.append

def search(label, sheet='main', **kw):
    d = {'id': label, 'sheet': sheet}
    d.update(kw)
    S({'search': d})

def tl(label, opts=True, **kw):
    search(label, timeline=opts, **kw)

def ymd(d):
    return d.isoformat()

def foo_filter():
    return {'rules': [{'name': 'string1', 'type': 'string', 'value': 'Foo', 'operator': 'equal'}]}

cdata = [
    dict(string1='Foo', integer1=50, date1='2014-10-10', daterange1=['2012-02-10', '2013-06-15']),
    dict(string1='Bar', integer1=99, date1='2009-01-02', daterange1=['2008-05-04', '2008-07-14']),
    dict(string1='FooBar', integer1=120, date1='2012-01-02', daterange1=['2012-05-04', '2013-07-14']),
]
data = [
    dict(string1='Foo', date1='2013-10-10', daterange1=['2014-03-21', '2015-03-01'], integer1=10,
         enum1='foo1', curval1=1),
    dict(string1='Bar', date1='2014-10-10', daterange1=['2010-01-04', '2011-06-03'], integer1=15,
         enum1='foo1', curval1=[2, 3]),
]
S({'sheet': {'as': 'cv', 'instance_id': 2, 'data': cdata}})
S({'sheet': {'as': 'main', 'data': data, 'curval': 'cv', 'multivalue': 1}})
search('all calendar', from_='x', user='none', retrieve_all=True, calendar=True)
tl('all timeline', user='none', retrieve_all=True, **{'from': '2008-01-01 01:00:00'})
tl('later date timeline', user='none', retrieve_all=True, **{'from': '2011-10-01'})
# The original creates a view for _created first (not otherwise used)
S({'view': {'op': 'create', 'sheet': 'main', 'name': 'Test view', 'columns': ['_created'],
            'filter': foo_filter()}})
view = {'name': 'Test view', 'columns': ['date1']}
search('filter calendar', user='none', view=view, filter=foo_filter(), calendar=True,
       **{'from': '2008-01-01 01:00:00'})
tl('filter timeline', user='none', view=view, filter=foo_filter(), **{'from': '2008-01-01 01:00:00'})
tl('filter timeline label', {'label': 'string1', 'group': 999, 'color': 999}, user='none', view=view,
   filter=foo_filter(), **{'from': '2008-01-01 01:00:00'})
search('filter range calendar', user='none', view=view, filter=foo_filter(), calendar=True,
       **{'from': '2010-01-01', 'to': '2020-01-01'})
tl('filter range timeline', user='none', view=view, filter=foo_filter(),
   **{'from': '2010-01-01', 'to': '2020-01-01'})

# Limited display of many timeline records
S({'reset': {}})
big = []
d = NOW
for count in range(1, 301):
    big.append({'string1': 'Foo%d' % rnd.randrange(1000), 'date1': ymd(d), 'enum1': 'foo%d' % ((count % 3) + 1)})
    d += timedelta(days=1)
S({'sheet': {'as': 'main', 'data': big}})
for sort in ('string1', 'enum1'):
    opts = {'group': 'enum1'} if sort == 'enum1' else True
    tl('large sorted by ' + sort, opts, user='none',
       view={'name': 'Foobar', 'columns': ['string1', 'date1'], 'sorts': [{'name': sort, 'type': 'asc'}]},
       **{'from': '2008-04-10 01:00:00'})
tl('large from to', user='none', retrieve_all=True, **{'from': '2008-01-01 01:00:00', 'to': '2008-01-11 01:00:00'})
tl('large from to midnight', user='none', retrieve_all=True, **{'from': '2008-01-01', 'to': '2008-01-10'})

# Exclusive functionality
S({'reset': {}})
S({'sheet': {'as': 'main', 'data': [
    dict(string1='foo1', daterange1=['2009-01-01', '2009-06-01']),
    dict(string1='foo2', daterange1=['2010-01-01', '2010-06-01']),
    dict(string1='foo3', daterange1=['2011-01-01', '2011-06-01']),
]}})
rng = {'from': '2009-03-01', 'to': '2011-03-01'}
tl('inclusive', user='none', retrieve_all=True, **rng)
tl('exclusive to', user='none', retrieve_all=True, exclusive='to', **rng)
tl('exclusive from', user='none', retrieve_all=True, exclusive='from', **rng)

# Various time tests
S({'reset': {}})
S({'sheet': {'as': 'main', 'data': []}})
for count in range(1, 6):
    S({'set_time': {'time': '2008-06-0%d 12:30:00' % count}})
    S({'record': {'op': 'create', 'sheet': 'main', 'values': {
        'string1': 'Count %d' % count, 'date1': '2008-07-05', 'daterange1': ['2008-01-01', '2008-02-01']}}})
times = ['_version_datetime', 'date1', 'daterange1']
tl('times all', retrieve=times, **{'from': '2008-06-03'})
tl('times exclusive from', retrieve=times, exclusive='from', **{'from': '2008-06-03 12:30:00', 'to': '2008-12-01'})
tl('times exclusive to', retrieve=times, exclusive='to', **{'from': '2007-01-01', 'to': '2008-06-03 12:30:00'})
S({'set_time': {'time': '2008-01-01 01:00:00'}})

# Records all after today, with only from set
S({'reset': {}})
S({'sheet': {'as': 'main', 'data': [
    dict(string1='foo1', date1='2009-01-01'), dict(string1='foo2', date1='2008-03-04'),
    dict(string1='foo3', date1='2008-10-04')]}})
tl('all after today', user='none', retrieve_all=True, **{'from': '2008-01-01 01:00:00'})

# Permissions
S({'reset': {}})
S({'sheet': {'as': 'main', 'data': [
    dict(string1='Foo', enum1=1, date1='2010-01-10', daterange1=['2009-01-01', '2009-06-01']),
    dict(string1='Bar', enum1=2, date1='2010-02-10', daterange1=['2009-02-01', '2009-05-01'])]}})
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'string1', 'set': {'permissions': {'default': []}}}})
S({'clear_layouts': {}})
tl('permissions string removed', user='normal1')
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'date1', 'set': {'permissions': {'default': []}}}})
S({'clear_layouts': {}})
tl('permissions date removed', user='normal1')

# Date from a calc field
S({'reset': {}})
year = 86400 * 366
S({'sheet': {'as': 'main', 'data': [
    dict(string1='foo1', daterange1=['2009-01-01', '2009-06-01']),
    dict(string1='foo2', daterange1=['2010-01-01', '2010-06-01'])],
    'calc_code': 'function evaluate (L1daterange1) \n return L1daterange1.from.epoch - %d \nend' % year,
    'calc_return_type': 'date'}})
tl('calc date', user='none', retrieve_all=True, **{'from': '2007-01-01', 'to': '2008-12-01'})

# No records to display
S({'reset': {}})
S({'sheet': {'as': 'main', 'data': []}})
tl('no records', user='none', retrieve_all=True, **{'from': '2008-01-01 01:00:00'})

# No records with date fields
S({'reset': {}})
S({'sheet': {'as': 'main', 'data': [{'string1': 'Foobar'}]}})
tl('no date fields', user='none', retrieve_all=True)

# Large set of records with early dateranges
S({'reset': {}})
big = []
d = NOW
early = date(1998, 1, 1)
for count in range(1, 301):
    big.append({'date1': ymd(d), 'daterange1': [ymd(early), '1998-02-01']})
    d += timedelta(days=1)
S({'sheet': {'as': 'main', 'data': big}})
tl('large with early ranges', retrieve_all=True, **{'from': '2008-04-10 01:00:00'})

# Default data, from and to
S({'reset': {}})
S({'sheet': {'as': 'main'}})
tl('default data from to', user='none', retrieve_all=True, **{'from': '2007-01-01 01:00:00', 'to': '2010-01-01 01:00:00'})

# Calc field as group
S({'reset': {}})
S({'sheet': {'as': 'main', 'data': [
    dict(string1='foo1', daterange1=['2009-01-01', '2009-06-01']),
    dict(string1='foo2', daterange1=['2010-01-01', '2010-06-01'])]}})
tl('calc as group', {'group': 'calc1'}, user='none', retrieve_all=True)

# Multiple items in group field, ordered correctly
S({'reset': {}})
big = []
d = date(2010, 1, 1)
for i in range(100):
    d += timedelta(days=1)
    big.append({'string1': 'foo2', 'enum1': [3], 'date1': ymd(d)})
big.append({'string1': 'foo1', 'enum1': [1, 4], 'date1': '2010-07-01'})
d = date(2010, 8, 1)
for i in range(120):
    d += timedelta(days=1)
    big.append({'string1': 'foo2', 'enum1': [2], 'date1': ymd(d)})
S({'sheet': {'as': 'main', 'data': big, 'multivalue': 1, 'enumvals_count': 4}})
tl('group ordering', {'group': 'enum1'}, user='none',
   view={'name': 'Test', 'columns': ['string1', 'date1'], 'sorts': [{'name': 'enum1', 'type': 'asc'}]},
   **{'from': '2010-06-01'})

# DST
S({'reset': {}})
S({'sheet': {'as': 'main', 'data': [{'string1': 'foo1', 'date1': '2018-03-26'}]}})
tl('dst', retrieve_all=True, **{'from': '2008-01-01 01:00:00'})

# Curval as group field
S({'reset': {}})
S({'sheet': {'as': 'cv', 'instance_id': 2, 'data': [
    {'string1': 'foobar1'}, {'string1': 'foobar4'}, {'string1': 'foobar3'}, {'string1': ''}]}})
S({'sheet': {'as': 'main', 'curval': 'cv', 'curval_fields': ['string1'], 'data': [
    dict(string1='Foo', date1='2013-10-10', curval1=1),
    dict(string1='Bar', date1='2014-10-10', curval1=2),
    dict(string1='FooBar', date1='2015-10-10', curval1=[2, 3]),
    dict(string1='Blank curval', date1='2015-03-10'),
    dict(string1='Blank curval string', date1='2015-04-10', curval1=4)]}})
tl('curval as group', {'group': 'curval1'}, user='none',
   view={'name': 'Test', 'columns': ALL, 'sorts': [{'name': 'curval1_string1', 'type': 'asc'}]})

# Only dates from a curval, only from set
S({'reset': {}})
S({'sheet': {'as': 'cv', 'instance_id': 2}})
S({'sheet': {'as': 'main', 'curval': 'cv', 'data': [
    {'string1': 'foo1', 'curval1': 1}, {'string1': 'foo1', 'curval1': 2}]}})
tl('only curval dates', retrieve=['string1', 'curval1'], **{'from': '2011-06-01'})

# Only curval dates, many records
S({'reset': {}})
cdata, mdata = [], []
d = NOW
for count in range(1, 301):
    cdata.append({'string1': 'Foo %d' % count, 'date1': ymd(d)})
    d += timedelta(days=1)
    mdata.append({'string1': 'Bar %d' % count, 'curval1': count})
d = date(1990, 1, 1)
for count in range(1, 101):
    cdata.append({'string1': 'Foo %d' % count, 'date1': ymd(d)})
    d += timedelta(days=1)
    mdata.append({'string1': 'Bar %d' % count, 'curval1': count + 300})
S({'sheet': {'as': 'cv', 'instance_id': 2, 'data': cdata}})
S({'sheet': {'as': 'main', 'curval': 'cv', 'data': mdata}})
tl('many curval dates', view={'name': 'Test view', 'columns': ['string1', 'curval1'],
                              'sorts': [{'name': 'string1', 'type': 'asc'}]},
   **{'from': '2008-04-10 01:00:00'})

# View with no date column
S({'reset': {}})
S({'sheet': {'as': 'main'}})
tl('view without date column', user='none', view={'name': 'Test view', 'columns': ['string1']},
   **{'from': '2008-01-01 01:00:00'})

# Curval date fields the user cannot access
S({'reset': {}})
mdata = []
d = date(2010, 1, 1)
for count in range(1, 201):
    mdata.append({'string1': 'Bar %d' % count, 'date1': ymd(d), 'curval1': count})
    d += timedelta(days=1)
cdata = []
for count in range(1, 201):
    cdata.append({'string1': 'Foo %d' % count, 'date1': ymd(d)})
    d -= timedelta(days=1)
S({'sheet': {'as': 'cv', 'instance_id': 2, 'data': cdata, 'multivalue': 1}})
S({'column': {'op': 'update', 'sheet': 'cv', 'column': 'date1', 'set': {'permissions': 'none'}}})
S({'clear_layouts': {}})
S({'sheet': {'as': 'main', 'curval': 'cv', 'data': mdata, 'multivalue': 1}})
S({'layout': {'op': 'set_user', 'sheet': 'main', 'user': 'normal1'}})
S({'layout': {'op': 'set_user', 'sheet': 'cv', 'user': 'normal1'}})
tl('curval table without date access', sheet='cv', user='normal1', **{'from': '2010-01-01'})
tl('no access to curval dates', user='normal1', **{'from': '2010-01-01'})

# 'from_' placeholder in the first step
for s in steps:
    sr = s.get('search')
    if sr and 'from_' in sr:
        del sr['from_']
        sr['from'] = '2008-01-01 01:00:00'

out = {'name': '013_datatime', 'source': ['t/013_datatime.t'],
       'description': 'Records plotted on a timeline and calendar',
       'fixed_time': '2008-01-01 01:00:00', 'steps': steps}
p = os.path.join(os.path.dirname(__file__), '..', 'scenarios', '013_datatime.yaml')
with open(p, 'w') as f:
    yaml.safe_dump(out, f, sort_keys=False, allow_unicode=True)
