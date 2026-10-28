#!/usr/bin/env python3
"""Generate the 007_code scenarios from upstream t/007_code.t.

Each independent block of the test (each with its own DataSheet and therefore
its own database) becomes a separate scenario.
"""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_007_code.py'

ANY_SERIAL = '~^\\d+$'


def regex_exact(s):
    """Expectation token for an exact string value (as qr/^$x$/ in the test)."""
    return s


# ----------------------------------------------------------------------
# Main set of code tests
# ----------------------------------------------------------------------

DATA = [
    {
        'daterange1': ['2000-10-10', '2001-10-10'],
        'curval1': 1,
        'tree1': 'tree1',
        'integer1': 10,
        'date1': '2016-12-20',
    },
    {
        'daterange1': ['2012-11-11', '2013-11-11'],
        'curval1': 2,
        'tree1': 'tree1',
        'integer1': 10,
    },
]

AUTOCUR_CODE = '''function evaluate (L2autocur1)
            return_value = ''
            for _, v in pairs(L2autocur1) do
                return_value = return_value .. v.field_values.L1daterange1.from.year
            end
            return return_value
        end'''

TESTS = [
    dict(name='calc field using curval (full value)', type='calc',
         code='function evaluate (L1curval1) \n return L1curval1.field_values.L2string1 .. ", " .. L1curval1.field_values.L2date1.ymd  \nend',
         before='Foo, 2014-10-10', after='Bar, 2009-01-02', multivalue=1),
    dict(name='calc field using curval (single curval field, standard)', type='calc',
         code='function evaluate (L1curval1) \n return L1curval1.field_values.L2string1 \nend',
         before='Foo', after='Bar', multivalue=1),
    dict(name='calc field using curval (single curval field, calc)', type='calc',
         code='function evaluate (L1curval1) \n return L1curval1.field_values.L2calc1 \nend',
         before='2012', after='2008', multivalue=1),
    dict(name='return array of multivalues', type='calc',
         code='function evaluate (_id) \n return {100, 200} \nend',
         before='100, 200', after='100, 200', multivalue=1),
    dict(name='serial value of record', type='calc',
         code='function evaluate (_serial) \n return _serial \nend',
         before='__SERIAL', after='__SERIAL'),
    dict(name='rag from daterange', type='rag',
         code='''
            function evaluate (L1daterange1)
                if L1daterange1 == nil then return end
                if L1daterange1.from.year < 2012 then return 'red' end
                if L1daterange1.from.year == 2012 then return 'amber' end
                if L1daterange1.from.year > 2012 then return 'green' end
            end
        ''', before='b_red', after='d_green'),
    dict(name='working days diff', type='calc',
         code='''
            function evaluate (L1date1)
                if L1date1 == nil then return nil end
                return working_days_diff(L1date1.epoch, 1483488000, 'GB', 'EAW')
            end
        ''', before='8', after='8'),
    dict(name='working days add', type='calc',
         code='''
            function evaluate (L1date1)
                if L1date1 == nil then return nil end
                return working_days_add(L1date1.epoch, 4, 'GB', 'EAW')
            end
        ''', before='1482883200', after='1482883200'),
    dict(name='decimal calc', type='calc',
         code='function evaluate (L1daterange1) \n return L1daterange1.from.year / 10 \nend',
         return_type='numeric', decimal_places=1, before='200.0', after='201.4'),
    dict(name='error return failed', type='calc',
         code="function evaluate (L1daterange1) \n return 'Unable to submit' \nend",
         return_type='error', is_error='Unable to submit'),
    dict(name='error return success', type='calc',
         code="function evaluate (L1daterange1) \n return '' \nend",
         return_type='error', is_error='', before='', after=''),
    dict(name='use date from another calc field', type='calc',
         code='function evaluate (L1calc1) \n return L1calc1.year \nend',
         before='2000', after='2014'),
    dict(name='use value from another calc field (integer)', type='calc',
         code='function evaluate (calc_integer) \n if calc_integer > 200 then return "greater" else return "less" end \nend',
         before='greater', after='greater'),
    dict(name='use value from another calc field (numeric)', type='calc',
         code='function evaluate (calc_numeric) \n if calc_numeric > 100 then return "greater" else return "less" end \nend',
         before='less', after='less'),
    dict(name='calc fields that returns 0 (int)', type='calc',
         code='function evaluate (L1curval1) \n return 0 \nend',
         return_type='integer', before='0', after='0'),
    dict(name='calc fields that returns 0 (string)', type='calc',
         code='function evaluate (L1curval1) \n return 0 \nend',
         return_type='string', before='0', after='0'),
    dict(name='calc fields that returns 0 (date)', type='calc',
         code='function evaluate (L1curval1) \n return 0 \nend',
         return_type='date', before='1970-01-01', after='1970-01-01'),
    dict(name='field with version editor', type='calc',
         code='function evaluate (_version_user) \n return _version_user.surname \nend',
         before='User1', after='User2'),
    dict(name='field with version editor organisation', type='calc',
         code='function evaluate (_version_user) \n return _version_user.organisation.name \nend',
         before='My Organisation', after='My Organisation'),
    dict(name='field with version editor department', type='calc',
         code='function evaluate (_version_user) \n return _version_user.department.name \nend',
         before='My Department', after='My Department'),
    dict(name='field with version date', type='calc',
         code='function evaluate (_version_datetime) \n return _version_datetime.day \nend',
         before='22', after='15'),
    dict(name='field with created date', type='calc',
         code='function evaluate (_created) \n return _created.day \nend',
         before='22', after='22'),
    # GADS-dev datetime return type (Europe/London, in BST)
    dict(name='return type of date and time', type='calc',
         code='function evaluate (_id) \n return 1781448310 \nend',
         return_type='datetime', before='2026-06-14 15:45:10', after='2026-06-14 15:45:10'),
    dict(name='tree node', type='calc',
         code='function evaluate (L1tree1) \n return L1tree1.value \nend',
         before='tree1', after='tree3'),
    dict(name='blank tree node', type='calc',
         code='function evaluate (L1tree1) \n return L1tree1.value \nend',
         tree_value=None, before='tree1', after=''),
    dict(name='flatten of hash', type='calc',
         code='function evaluate (L1tree1) \n return L1tree1 \nend',
         before='~HASH', after='~HASH'),
    dict(name='flatten of array', type='calc',
         code='function evaluate (L1tree1) \n a = {} \n a[1] = L1tree1 \n return a \nend',
         before='~HASH', after='~HASH', multivalue=1),
    dict(name='autocur', type='calc', layout='curval', record_check=1,
         code=AUTOCUR_CODE, before='20002000', after='2000'),
    dict(name='autocur code update only', type='calc', layout='curval', record_check=1,
         curval_update=0, code=AUTOCUR_CODE, before='20002000', after='20002014'),
    dict(name='integer passed to Lua as int type not string', type='calc',
         code='function evaluate (L1integer1) \n if L1integer1 == 10 then return "Yes" else return L1integer1 end \nend',
         before='Yes', after='Yes'),
    dict(name='curval ID passed to Lua as int type not string', type='calc',
         code='function evaluate (L1curval1) \n if L1curval1.id == 1 or L1curval1.id == 2 then return "Yes" else return "No" end \nend',
         before='Yes', after='Yes'),
]


def expect_value(v):
    if v.startswith('~'):
        return v
    if v == '__SERIAL':
        return ANY_SERIAL
    return v


def main_scenario():
    steps = [
        {'sheet': {'as': 'curval', 'instance_id': 2}},
        {'sheet': {
            'as': 'main', 'own_group': True, 'data': DATA,
            'curval': 'curval', 'curval_fields': ['string1', 'date1'],
            'calc_code': 'function evaluate (L1daterange1) \n return L1daterange1.from.epoch \n end',
            'calc_return_type': 'date',
        }},
        {'add_autocur': {'sheet': 'curval', 'refers_to': 'main', 'related_field': 'curval1',
                         'curval_fields': ['daterange1']}},
        {'column': {'id': 'create calc_integer', 'sheet': 'main', 'type': 'calc', 'name': 'calc_integer',
                    'name_short': 'calc_integer', 'return_type': 'integer', 'permissions_sheet': 'main',
                    'code': 'function evaluate (L1string1) \n return 450 \nend'}},
        {'column': {'id': 'create calc_numeric', 'sheet': 'main', 'type': 'calc', 'name': 'calc_numeric',
                    'name_short': 'calc_numeric', 'return_type': 'numeric', 'permissions_sheet': 'main',
                    'code': 'function evaluate (L1string1) \n return 10.56 \nend'}},
    ]
    year = 2014
    for t in TESTS:
        n = t['name']
        code_sheet = t.get('layout', 'main')
        col = {'id': f'{n}: create code col', 'sheet': code_sheet, 'type': t['type'],
               'name': 'code col', 'as': 'code', 'return_type': t.get('return_type', 'string'),
               'code': t['code'], 'permissions_sheet': 'main'}
        if 'decimal_places' in t:
            col['decimal_places'] = t['decimal_places']
        if 'multivalue' in t:
            col['multivalue'] = t['multivalue']
        steps.append({'column': col})
        # Code values should have been written to the database by now
        steps.append({'held': {'id': f'{n}: single', 'op': 'single', 'as': 'r1', 'sheet': 'main',
                               'panic_on_code': True}})
        steps.append({'held': {'id': f'{n}: find current', 'op': 'find', 'as': 'r2', 'sheet': 'main',
                               'clear': True, 'current_id': '@held:r1'}})
        steps.append({'held': {'op': 'new', 'as': 'r3', 'sheet': 'main'}})
        steps.append({'held': {'op': 'set', 'as': 'r3', 'values': [
            {'daterange1': ['2000-10-10', '2001-10-10']},
            {'date1': '2016-12-20'},
            {'curval1': 1},
            {'tree1': 10},
            {'integer1': 10},
        ]}})
        write = {'id': f'{n}: write new record', 'op': 'write', 'as': 'r3', 'clear_layout': True}
        if t.get('is_error'):
            write['expect_error'] = t['is_error']
            steps.append({'held': write})
            steps.append({'column': {'op': 'delete', 'sheet': code_sheet, 'column': '@col:code'}})
            continue
        steps.append({'held': write})
        steps.append({'held': {'id': f'{n}: reload new record', 'op': 'find', 'as': 'r3', 'sheet': 'main',
                               'clear': True, 'current_id': '@held:r3'}})
        for r in ['r1', 'r2', 'r3']:
            p = f'{n} [{r}]'
            steps.append({'held': {'id': f'{p}: serial', 'op': 'get', 'as': r, 'serial': True,
                                   'expect': {'serial': ANY_SERIAL}}})
            if t.get('record_check'):
                check = {'op': 'find', 'as': 'check', 'sheet': 'curval', 'clear': True,
                         'current_id': t['record_check']}
            else:
                check = {'op': 'get', 'as': r}
            steps.append({'held': dict(check, id=f'{p}: before', observe=['@col:code'],
                                       value_refs=['@col:code'],
                                       expect={'values': {'@col:code': expect_value(t['before'])}})})
            # Check we can update the record
            steps.append({'set_time': {'time': f'{year}-11-15 01:00:00'}})
            values = [{'daterange1': ['2014-10-10', '2015-10-10']}]
            if t.get('curval_update', 1):
                values.append({'curval1': 2})
            values.append({'tree1': t['tree_value'] if 'tree_value' in t else 12})
            steps.append({'held': {'op': 'set', 'as': r, 'values': values}})
            steps.append({'held': {'op': 'user', 'as': r, 'user': 2}})
            steps.append({'held': {'id': f'{p}: update', 'op': 'write', 'as': r, 'clear_layout': True}})
            steps.append({'held': dict(check, id=f'{p}: after', observe=['@col:code'],
                                       value_refs=['@col:code'],
                                       expect={'values': {'@col:code': expect_value(t['after'])}})})
            if not t.get('record_check'):
                steps.append({'db': {'table': 'enumval', 'where': {'id': 12}, 'set': {'deleted': 1}}})
                steps.append({'held': {'op': 'find', 'as': r, 'clear': True, 'clear_layout_first': True,
                                       'current_id': '@held:' + r}})
                steps.append({'held': {'op': 'set', 'as': r, 'values': {'string1': 'Foobar'}}})
                steps.append({'held': {'op': 'write', 'as': r, 'clear_layout': True}})
                steps.append({'held': {'id': f'{p}: after enum deletion', 'op': 'find', 'as': r,
                                       'clear': True, 'current_id': '@held:' + r,
                                       'observe': ['@col:code'],
                                       'expect': {'values': {'@col:code': expect_value(t['after'])}}}})
            # Reset values for next test
            steps.append({'db': {'table': 'enumval', 'where': {'id': 12}, 'set': {'deleted': 0}}})
            year += 1
            steps.append({'set_time': {'time': f'{year}-10-22 01:00:00'}})
            steps.append({'held': {'op': 'set', 'as': r, 'values': [
                {'daterange1': DATA[0]['daterange1']},
                {'curval1': DATA[0]['curval1']},
                {'string1': ''},
                {'tree1': 10},
            ]}})
            steps.append({'held': {'op': 'user', 'as': r, 'user': 1}})
            steps.append({'held': {'id': f'{p}: reset', 'op': 'write', 'as': r, 'clear_layout': True}})
        steps.append({'held': {'op': 'find', 'as': 'del', 'sheet': 'main', 'clear': True,
                               'current_id': '@held:r3'}})
        steps.append({'held': {'op': 'delete', 'as': 'del'}})
        steps.append({'held': {'op': 'purge', 'as': 'del'}})
        steps.append({'column': {'op': 'delete', 'sheet': code_sheet, 'column': '@col:code'}})

    # Attempt to create additional calc fields separately
    def calc(id_, name, code, **kw):
        c = {'id': id_, 'sheet': 'main', 'type': kw.pop('type', 'calc'), 'name': name, 'code': code,
             'permissions': 'none'}
        c.update(kw)
        return {'column': c}
    steps += [
        calc('invalid function', 'calc2', 'foobar evaluate (L1curval)',
             expect_error='Invalid code definition'),
        calc('invalid short name', 'calc2', 'function evaluate (L1curval) \n return L1curval1.value\nend',
             expect_error='Unknown short column name'),
        calc('short name from other table', 'calc2', 'function evaluate (L2string1) \n return L2string1\nend',
             expect_error='It is only possible to use fields from the same table'),
        calc('syntax error in nested code', 'calc3',
             'function evaluate (L1curval1) \n adsfadsf return L1curval1.field_values.L2daterange1.from.year \nend',
             capture_warnings=True, expect_messages_match='syntax error'),
        calc('syntax error in nested code (integer)', 'calc3',
             'function evaluate (L1curval1) \n adsfadsf return L1curval1.field_values.L2daterange1.from.year \nend',
             return_type='integer', capture_warnings=True, expect_messages_match='syntax error'),
        calc('missing bank holidays diff', 'calc3',
             "function evaluate (_id) \n return working_days_diff(2051222400, 2051222400, 'GB', 'EAW') \nend",
             capture_warnings=True,
             expect_messages_match='No bank holiday information available for year 2035'),
        calc('missing bank holidays add', 'calc3',
             "function evaluate (_id) \n return working_days_add(2082758400, 1, 'GB', 'EAW') \nend",
             capture_warnings=True, **{'as': 'bh'},
             expect_messages_match='No bank holiday information available for year 2036'),
        {'column': {'op': 'delete', 'sheet': 'main', 'column': '@col:bh'}},
        calc('syntax error in rag', 'rag2', '''
            function evaluate (L1daterange1)
                foobar
            end
        ''', type='rag', capture_warnings=True, expect_messages_match='syntax error', **{'as': 'rag2'}),
        {'column': {'op': 'delete', 'sheet': 'main', 'column': '@col:rag2'}},
    ]
    return {
        '_generator': GEN,
        'name': '007_code',
        'source': ['t/007_code.t'],
        'description': 'Calc and RAG code fields: values before and after updates, stored values, invalid code',
        'fixed_time': '2014-10-22 01:00:00',
        'steps': steps,
    }


# ----------------------------------------------------------------------
# Multi-value calc returns
# ----------------------------------------------------------------------

def mv_calc_scenario():
    def count(id_, n, where=None):
        s = {'id': id_, 'op': 'select', 'table': 'calcval', 'columns': ['id'],
             'expect': [{'id': '@any'}] * n}
        if where:
            s['where'] = where
        return {'db': s}

    def held(op, **kw):
        kw['op'] = op
        kw.setdefault('as', 'r')
        return {'held': kw}
    steps = [
        {'sheet': {'as': 'main', 'data': [], 'calc_return_type': 'string', 'calc_code': '''
            function evaluate (L1integer1)
                a = {}
                if L1integer1 < 10 then
                    for i=1, L1integer1 do
                        a[i] = 10
                    end
                elseif L1integer1 == 10 then
                    return {100, 200}
                else
                    return {200, 100}
                end
                return a
            end
        '''}},
        {'column': {'op': 'update', 'column': 'calc1', 'set': {'multivalue': 1}}},
        held('new'),
        held('set', values={'integer1': 1}),
        held('write'),
        count('one value written', 1),
        held('get', id='one element', observe=['calc1'], expect={'values': {'calc1': '10'}}),
        held('set', values={'integer1': 2}),
        held('re_evaluate', column='calc1'),
        count('second value not yet written', 1),
        held('get', id='two elements', observe=['calc1'], expect={'values': {'calc1': '10, 10'}}),
        held('write_value', column='calc1'),
        count('second value written', 2),
        held('find', clear=True, current_id=1),
        held('set', id='not changed to begin with', values={'integer1': 3}, changed=['calc1'],
             expect={'changed': {'calc1': 0}}),
        held('re_evaluate', id='changed after re-evaluation', column='calc1', changed=['calc1'],
             expect={'changed': {'calc1': 1}}),
        count('values before write', 2),
        held('write_value', column='calc1'),
        count('values after write', 3),
        held('set', values={'integer1': 1}),
        held('re_evaluate', column='calc1'),
        held('write_value', column='calc1'),
        count('old values deleted', 1),
        held('set', values={'integer1': 10}),
        held('write'),
        count('values for int 10', 2, where={'record_id': 2}),
        held('find', clear=True, current_id=1),
        held('set', values={'integer1': 10}),
        held('re_evaluate', id='not changed after same value', column='calc1', changed=['calc1'],
             observe=['calc1'], expect={'changed': {'calc1': 0}, 'values': {'calc1': '100, 200'}}),
        held('set', values={'integer1': 11}),
        held('re_evaluate', id='not changed after switching elements', column='calc1', changed=['calc1'],
             observe=['calc1'], expect={'changed': {'calc1': 0}, 'values': {'calc1': '200, 100'}}),
        held('write_value', column='calc1'),
        count('values after switch', 2, where={'record_id': 2}),
    ]
    return {'_generator': GEN, 'name': '007_code_mv_calc', 'source': ['t/007_code.t'],
            'description': 'Multi-value calc returns: values written and changed status',
            'fixed_time': '2014-10-22 01:00:00', 'steps': steps}


# ----------------------------------------------------------------------
# More "changed" tests
# ----------------------------------------------------------------------

def changed_scenario():
    def held(op, **kw):
        kw['op'] = op
        kw.setdefault('as', 'r')
        return {'held': kw}

    def find(**kw):
        return held('find', clear=True, current_id='@held:r', **kw)
    both = ['calc1', 'rag1']
    steps = [
        {'sheet': {'as': 'main'}},
        held('new'),
        held('set', values={'daterange1': ['2011-10-10', '2015-10-10']}),
        held('write', id='initial write', observe=both, expect={'values': {'calc1': '2011', 'rag1': 'b_red'}}),
        find(id='not changed on load', changed=both, expect={'changed': {'calc1': 0, 'rag1': 0}}),
        held('set', values={'daterange1': ['2011-09-10', '2015-10-10']}),
        held('write', id='not changed on suitable write', changed=both,
             expect={'changed': {'calc1': 0, 'rag1': 0}}),
        find(),
        held('set', values={'daterange1': ['2013-09-10', '2015-10-10']}),
        held('write', id='changed on suitable write', changed=both,
             expect={'changed': {'calc1': 1, 'rag1': 1}}),
        find(),
        held('set', values={'daterange1': None}),
        held('write', id='changed when set to blank', changed=['calc1'], expect={'changed': {'calc1': 1}}),
        find(),
        held('re_evaluate', id='not changed on blank re-evaluation', column='calc1', changed=['calc1'],
             expect={'changed': {'calc1': 0}}),
        {'column': {'op': 'update', 'column': 'calc1', 'set': {'return_type': 'string'}}},
        find(),
        held('re_evaluate', id='not changed after re-evaluation, string return', column='calc1',
             changed=['calc1'], expect={'changed': {'calc1': 0}}),
        {'column': {'op': 'update', 'column': 'calc1', 'set': {
            'return_type': 'date', 'code': 'function evaluate (_created) return _created.epoch end'}}},
        held('find', current_id='@held:r'),
        held('re_evaluate', id='date created code not changed', column='calc1', changed=['calc1'],
             expect={'changed': {'calc1': 0}}),
    ]
    return {'_generator': GEN, 'name': '007_code_changed', 'source': ['t/007_code.t'],
            'description': 'Changed status of calc and rag values',
            'fixed_time': '2014-10-22 01:00:00', 'steps': steps}


# ----------------------------------------------------------------------
# Multivalue fields changed into single value fields
# ----------------------------------------------------------------------

SINGLE_MULTI_TESTS = [
    ('daterange1', '''
                function evaluate (L1daterange1)
                    ret = ""
                    for _,val in ipairs(L1daterange1) do
                        ret = ret .. val.value
                    end
                    return ret
                end''', '2000-10-10 to 2001-10-102010-01-01 to 2011-01-01'),
    ('curval1', '''
                function evaluate (L1curval1)
                    ret = ""
                    for _,val in ipairs(L1curval1) do
                        v = val.field_values.L2string1 .. ", " .. val.field_values.L2date1.ymd
                        ret = ret .. v
                    end
                    return ret
                end''', 'Bar, 2009-01-02Foo, 2014-10-10'),
    ('tree1', '''
                function evaluate (L1tree1)
                    ret = ""
                    for _,val in ipairs(L1tree1) do
                        ret = ret .. val.value
                    end
                    return ret
                end''', 'tree1tree2'),
    ('enum1', '''
                function evaluate (L1enum1)
                    ret = ""
                    for _,val in ipairs(L1enum1.values) do
                        ret = ret .. val.value
                    end
                    return ret
                end''', 'foo3foo2'),
    ('date1', '''
                function evaluate (L1date1)
                    ret = ""
                    for _,val in ipairs(L1date1) do
                        ret = ret .. val.year
                    end
                    return ret
                end''', '20162017'),
    ('string1', '''
                function evaluate (L1string1)
                    ret = ""
                    for _,val in ipairs(L1string1) do
                        ret = ret .. val
                    end
                    return ret
                end''', 'BarFoo'),
]


def single_multi_scenario(multi):
    data = [{
        'daterange1': [['2000-10-10', '2001-10-10'], ['2010-01-01', '2011-01-01']],
        'curval1': [1, 2],
        'tree1': [10, 11],
        'enum1': [8, 9],
        'date1': ['2016-12-20', '2017-01-01'],
        'string1': ['Foo', 'Bar'],
    }]
    steps = [
        {'sheet': {'as': 'curval', 'instance_id': 2}},
        {'sheet': {
            'as': 'main', 'own_group': True, 'data': data, 'multivalue': 1,
            'curval': 'curval', 'curval_fields': ['string1', 'date1'],
            'calc_return_type': 'string',
            'calc_code': 'function evaluate (L1daterange1) \n return 1234 \n end',
            'rag_code': 'function evaluate (L1daterange1) \n return "green" \n end',
        }},
    ]
    for col, code, value in SINGLE_MULTI_TESTS:
        if not multi:
            steps.append({'column': {'op': 'update', 'sheet': 'main', 'column': col,
                                     'set': {'multivalue': 0}}})
        steps.append({'column': {'op': 'update', 'sheet': 'main', 'column': 'calc1', 'set': {'code': code}}})
        steps.append({'held': {'op': 'find', 'as': 'r', 'sheet': 'main', 'clear': True, 'current_id': 3}})
        steps.append({'held': {'id': f'{col} (multi {multi})', 'op': 're_evaluate', 'as': 'r',
                               'column': 'calc1', 'observe': ['calc1'],
                               'expect': {'values': {'calc1': value}}}})
    steps.append({'db': {'id': 'enumvals after column updates', 'op': 'select', 'table': 'enumval',
                         'columns': ['id', 'value', 'deleted', 'layout_id']}})
    return {'_generator': GEN, 'name': f'007_code_single_multi_mv{multi}', 'source': ['t/007_code.t'],
            'description': 'Code values of multivalue fields that have been changed to single value',
            'fixed_time': '2014-10-22 01:00:00', 'steps': steps}


# ----------------------------------------------------------------------
# Multivalue field within multivalue curval
# ----------------------------------------------------------------------

def curval_mv_scenario(multivalue):
    curval_var = 'L1curval1[1]' if multivalue else 'L1curval1'
    expected = '~^(foo1, foo2|foo2, foo1)$'
    steps = [
        {'sheet': {'as': 'curval', 'instance_id': 2, 'multivalue': 1, 'data': [{
            'string1': ['Foo', 'Bar'], 'enum1': [2, 3], 'date1': ['2016-12-20', '2017-01-01']}]}},
        {'sheet': {
            'as': 'main', 'own_group': True, 'data': [], 'multivalue': 1,
            'multivalue_columns': {'calc': 1, 'curval': multivalue},
            'curval': 'curval', 'curval_fields': ['string1', 'date1'],
            'calc_return_type': 'string',
            'calc_code': f'''function evaluate (L1curval1)
            curval = {curval_var}
            all_enums = {{}}
            for _,enum in ipairs(curval.field_values.L2enum1.values) do
               all_enums[#all_enums+1] = enum.value
            end
            return all_enums
        end''',
        }},
        # Edit the enum value, so that the correct version is checked
        {'held': {'op': 'find', 'as': 'c', 'sheet': 'main', 'current_id': 1}},
        {'held': {'op': 'set', 'as': 'c', 'values': {'curval:enum1': [1, 2]}}},
        {'held': {'op': 'write', 'as': 'c'}},
        {'held': {'op': 'new', 'as': 'r', 'sheet': 'main'}},
        {'held': {'op': 'set', 'as': 'r', 'values': {'curval1': [1]}}},
        {'held': {'id': 'after initial write', 'op': 'write', 'as': 'r', 'observe': ['calc1'],
                  'expect': {'values': {'calc1': expected}}}},
        {'held': {'id': 'after load', 'op': 'find', 'as': 'r', 'clear': True, 'current_id': '@held:r',
                  'observe': ['calc1'], 'expect': {'values': {'calc1': expected}}}},
        {'column': {'op': 'update_cached', 'sheet': 'main', 'column': 'calc1'}},
        {'held': {'id': 'after bulk update', 'op': 'find', 'as': 'r', 'clear': True, 'current_id': '@held:r',
                  'observe': ['calc1'], 'expect': {'values': {'calc1': expected}}}},
    ]
    return {'_generator': GEN, 'name': f'007_code_curval_mv{multivalue}', 'source': ['t/007_code.t'],
            'description': 'Multivalue field within multivalue curval in calc code',
            'fixed_time': '2014-10-22 01:00:00', 'steps': steps}


# ----------------------------------------------------------------------
# Blank and null strings treated the same
# ----------------------------------------------------------------------

def blank_scenario(test):
    field = 'L1string1' if 'string' in test else 'L1calc1'
    null = 'null' in test
    code = f'''
        function evaluate ({field})
            if L1string1 == nil then
                return "nil"
            end
            if L1string1 == "" then
                return "empty"
            end
            return "unexpected: " .. L1string1
        end'''
    steps = [
        {'sheet': {'as': 'main', 'data': [{'string1': ''}], 'calc_return_type': 'string',
                   'calc_code': 'function evaluate (_id) return "" end'}},
        {'column': {'sheet': 'main', 'type': 'calc', 'name': 'L1calc2', 'as': 'calc2', 'code': code}},
        {'db': {'table': 'calcval', 'set': {'value_text': None if null else ''}}},
        {'db': {'table': 'string', 'set': {'value': None if null else ''}}},
        {'column': {'op': 'update_cached', 'sheet': 'main', 'column': '@col:calc2'}},
        {'record': {'id': f'calc from string ({test})', 'op': 'get', 'sheet': 'main', 'current_id': 1,
                    'observe': ['L1calc2'], 'expect': {'values': {'L1calc2': 'nil'}}}},
    ]
    return {'_generator': GEN, 'name': f'007_code_blank_{test}', 'source': ['t/007_code.t'],
            'description': 'Blank and null string values in the database are treated the same in code',
            'fixed_time': '2014-10-22 01:00:00', 'steps': steps}


def main():
    docs = [main_scenario(), mv_calc_scenario(), changed_scenario()]
    docs += [single_multi_scenario(m) for m in (0, 1)]
    docs += [curval_mv_scenario(m) for m in (0, 1)]
    docs += [blank_scenario(t) for t in ('string_empty', 'string_null', 'calc_empty', 'calc_null')]
    for d in docs:
        write_scenario(os.path.join(OUT, d['name'] + '.yaml'), d)
        print('wrote', d['name'])


if __name__ == '__main__':
    main()
