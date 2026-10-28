#!/usr/bin/env python3
"""Generate scenarios/014_import.yaml from t/014_import.t"""
import yaml, os, copy

steps = []
S = steps.append

def imp(name, content, user=None, **options):
    d = {'id': name, 'sheet': 'main', 'content': content}
    if user:
        d['user'] = user
    if options:
        d['options'] = options
    S({'import': d})

def search(name, observe, **kw):
    d = {'id': name, 'sheet': 'main', 'observe': observe}
    d.update(kw)
    S({'search': d})

def user_row(name, email, pw):
    S({'user': {'op': 'insert_row', 'as': name,
                'set': {'email': email, 'username': email, 'password': pw}}})

# Version tests
S({'sheet': {'as': 'main', 'data': []}})
user_row('u1', 'test@example.com', 'test')
user_row('u2', 'test2@example.com', 'test2')
imp('version', 'string1,Last edited time,Last edited by\nFoobar,2014-10-10 12:00,{user:u2}', user='u1')
search('version written', ['string1', '_version_user', '_version_datetime'])
S({'db': {'id': 'version createdby', 'op': 'select', 'table': 'record', 'where': {'current_id': 1},
          'columns': ['createdby', 'created']}})

# Deleted version of live enumval and tree
for typ in ('enum', 'tree'):
    S({'reset': {}})
    S({'sheet': {'as': 'main', 'data': []}})
    val = 'foo1' if typ == 'enum' else 'tree1'
    S({'db': {'id': 'deleted %s count' % typ, 'op': 'count', 'table': 'enumval', 'where': {'value': val}}})
    S({'db': {'op': 'update', 'table': 'enumval', 'where': {'value': val}, 'set': {'deleted': 1}}})
    S({'db': {'op': 'insert', 'table': 'enumval',
              'set': {'layout_id': '@column:main:%s1' % typ, 'value': val, 'deleted': 0}}})
    imp('deleted ' + typ, 'enum1\nfoo1' if typ == 'enum' else 'tree1\ntree1')
    search('deleted %s written' % typ, [typ + '1'])

tests = [
    dict(data="string1\nString content", option=None),
    dict(data="string1\nFoo\nBar,FooBar", option=None),
    dict(data="string1\nString content", option='dry_run'),
    dict(data="string1,integer1\nString content,123\n,234", option='force_mandatory'),
    dict(data="enum1\nfoo1\nfoobar", option='blank_invalid_enum'),
    dict(data="enum1\nfoo1\nduplicate", option='take_first_enum'),
    dict(data='enum1\nfoo1\n"foo1,foo2"', option='split_multiple', written=['enum']),
    dict(data="string1\nfoo\n0", option='ignore_string_zeros', written=['string']),
    dict(data="integer1\n100\n12.7", option='round_integers', written=['intgr']),
    # GADS-dev: empty heading (appended to keep the ids of the others)
    dict(data="string1, ,integer1\nFoo,Bar,123", option=None, error='Empty field name'),
]
for n, t in enumerate(tests):
    for status in ('on', 'off'):
        S({'reset': {}})
        S({'sheet': {'as': 'main', 'data': [], 'multivalue': 1}})
        user_row('u', 'test@example.com', 'test')
        name = 'options %d %s %s' % (n, t['option'], status)
        if t['option'] == 'force_mandatory':
            S({'column': {'op': 'update', 'sheet': 'main', 'column': 'string1', 'set': {'optional': 0}}})
        if t['option'] == 'take_first_enum':
            S({'column': {'op': 'update', 'sheet': 'main', 'column': 'enum1', 'set': {
                'enumvals': [{'value': 'foo1'}, {'value': 'duplicate'}, {'value': 'duplicate'}]}}})
        opts = {t['option']: 1 if status == 'on' else 0} if t['option'] else {}
        imp(name, t['data'], user='u', **opts)
        if t.get('error'):
            steps[-1]['import']['expect_error'] = t['error']
            continue
        search(name + ' records', ['string1', 'integer1', 'enum1'])
        for table in t.get('written', []):
            S({'db': {'id': name + ' ' + table, 'op': 'select', 'table': table, 'columns': ['value']}})

ex_full = dict(string1='Foo', integer1=50, date1='2010-10-10', enum1='foo1', tree1='tree1',
               daterange1=['2010-10-10', '2010-11-10'], curval1=1)
update_tests = [
    dict(name='Update unique field with string', option='update_unique',
         data="string1,integer1\nFoo,100\nFoo2,150", unique='string1', results=['string1', 'integer1']),
    dict(name='Update unique field with enum', option='update_unique',
         data="string1,enum1\nFooBar1,foo1\nFooBar2,foo3", unique='enum1', results=['string1', 'enum1'],
         existing=[dict(string1='Foo', enum1='foo1'), dict(string1='Bar', enum1='foo2')]),
    dict(name='Update unique field with tree', option='update_unique',
         data="string1,tree1\nFooBar1,tree1\nFooBar2,tree3", unique='tree1', results=['string1', 'tree1'],
         existing=[dict(string1='Foo', tree1='tree1'), dict(string1='Bar', tree1='tree2')]),
    dict(name='Update unique field with person', option='update_unique',
         data='string1,person1\nBar,"User1, User1"', unique='person1', results=['string1', 'person1'],
         existing=[dict(string1='Foo', person1=1)]),
    dict(name='Update unique field with serial', option='update_unique',
         data="string1,Serial\nBar2,1", unique='_serial', results=['string1'],
         existing=[dict(string1='Foo')]),
    dict(name='Skip when existing unique value exists', option='skip_existing_unique',
         data="string1,integer1\nFoo,100\nFoo2,150", unique='string1', results=['string1', 'integer1']),
    dict(name='No change of value unless blank', option='no_change_unless_blank',
         data="string1,integer1,date1\nFoo,200,2010-10-10\nBar,300,2011-10-10", unique='string1',
         results=['string1', 'integer1', 'date1'],
         existing=[dict(string1='Foo', integer1=50, date1=''), dict(string1='Bar', integer1='', date1='')]),
    dict(name='Invalid values', option='update_unique',
         data="ID,integer1,date1,enum1,tree1,daterange1,curval1\n3,XX,,,,,\n3,,201-9,,,,\n3,,,foo4,,,\n3,,,,tree4,,\n3,,,,,2012-10-10 FF 2013-10-10,\n3,,,,,,9",
         unique='ID', results=['string1', 'integer1', 'date1', 'enum1', 'tree1', 'daterange1', 'curval1'],
         existing=[ex_full]),
    dict(name='Attempt to update blank ID', option='update_unique', data="ID,string1\n,FooBar",
         unique='ID', results=['string1'], existing=[]),
    dict(name='Attempt to update ID from different table', option='update_unique', data="ID,string1\n1,Bar",
         unique='ID', results=['string1'], existing=[dict(string1='Foo')]),
    dict(name='Update existing records only', option='update_only',
         data="ID,string1,integer1,date1,enum1,tree1,daterange1,curval1\n3,Bar,200,2011-10-10,foo2,tree2,2011-10-10 to 2011-11-10,2\n4,,,,,,,",
         unique='ID', results=['string1', 'integer1', 'date1', 'enum1', 'tree1', 'daterange1', 'curval1', 'calc1'],
         calc_code='''
            function evaluate (_version_user, _version_datetime)
                return _version_user.firstname .. _version_user.surname
                    .. _version_datetime.year .. _version_datetime.month .. _version_datetime.day
            end
        ''',
         existing=[ex_full, dict(ex_full, string1='FooBar', integer1=10)]),
]
for t in update_tests:
    S({'reset': {}})
    S({'set_time': {'time': '2014-10-10 01:00:00'}})
    S({'sheet': {'as': 'cv', 'instance_id': 2}})
    sheet = {'as': 'main', 'curval': 'cv', 'share_users': True}
    if 'existing' in t:
        sheet['data'] = copy.deepcopy(t['existing'])
    if 'calc_code' in t:
        sheet['calc_code'] = t['calc_code']
        sheet['calc_return_type'] = 'string'
    S({'sheet': sheet})
    S({'set_time': {'time': '2015-05-05 01:00:00'}})
    if t['unique'] not in ('ID', '_serial'):
        S({'column': {'op': 'update', 'sheet': 'main', 'column': t['unique'], 'set': {'isunique': 1}}})
    opt = t['option']
    if opt == 'skip_existing_unique':
        opts = {'skip_existing_unique': t['unique']}
    elif opt == 'no_change_unless_blank':
        opts = {'update_unique': t['unique'], 'no_change_unless_blank': 'skip_new'}
    elif opt == 'update_only':
        opts = {'update_only': 1, 'update_unique': t['unique']}
    else:
        opts = {'update_unique': t['unique']}
    imp(t['name'], t['data'], **opts)
    search(t['name'] + ' records', t['results'])
    S({'db': {'id': t['name'] + ' versions', 'op': 'count', 'table': 'record'}})

out = {'name': '014_import', 'source': ['t/014_import.t'],
       'description': 'CSV import of records (GADS::Import)', 'steps': steps}
p = os.path.join(os.path.dirname(__file__), '..', 'scenarios', '014_import.yaml')
with open(p, 'w') as f:
    yaml.safe_dump(out, f, sort_keys=False, allow_unicode=True)
