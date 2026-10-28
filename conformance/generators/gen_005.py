#!/usr/bin/env python3
"""Generate scenarios from upstream t/005_unique.t and t/005_dateformat.t."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_005.py'

UNIQUE_ERR = 'must be unique but value .* already exists'


def held(op, **kw):
    kw['op'] = op
    kw.setdefault('as', 'r')
    return {'held': kw}


def col_update(column, **set_):
    return {'column': {'op': 'update', 'sheet': 'main', 'column': column, 'set': set_}}


def unique_scenario():
    data = {
        'string1': 'Bar',
        'integer1': 99,
        'date1': '2009-01-02',
        'enum1': 1,
        'tree1': 4,
        'daterange1': ['2008-05-04', '2008-07-14'],
        'person1': 1,
        'file1': {'name': 'file1.txt', 'mimetype': 'text/plain', 'content': 'Text file1'},
    }
    steps = [
        {'sheet': {'as': 'main', 'data': [data], 'calc_return_type': 'string',
                   'calc_code': 'function evaluate (L1string1)\n        return L1string1\n    end'}},
        held('new', sheet='main'),
    ]
    # In order of $layout->all(userinput => 1)
    for name in ['string1', 'integer1', 'enum1', 'tree1', 'date1', 'daterange1', 'file1', 'person1']:
        steps += [
            col_update(name, isunique=1),
            held('set', values={name: data[name]}),
            held('write', id=f'unique existing value for {name}', expect_error=UNIQUE_ERR),
            col_update(name, isunique=0),
        ]
    # Calc unique values
    steps += [
        col_update('calc1', isunique=1),
        held('set', values={'calc1': data['string1']}),
        held('write', id='unique existing value for calc value', expect_error=UNIQUE_ERR),
        held('new', sheet='main'),
        col_update('calc1', isunique=0),
    ]
    # Calc with child unique
    steps += [
        col_update('calc1', isunique=1),
        col_update('string1', can_child=1),
        held('new', **{'as': 'child'}, sheet='main', parent_id=1),
        held('set', **{'as': 'child'}, values={'string1': data['string1']}),
        held('write', **{'as': 'child'}, id='unique existing value for calc-dependent value',
             expect_error=UNIQUE_ERR),
        col_update('date1', can_child=1),
        col_update('string1', can_child=0),
        held('new', **{'as': 'child'}, sheet='main', parent_id=1),
        held('set', **{'as': 'child'}, values={'date1': '2013-10-10'}),
        held('write', **{'as': 'child'}, id='wrote child with duplicate calc value', observe=['calc1'],
             expect={'values': {'calc1': data['string1']}}),
    ]
    # Calc unique value that the user does not have access to
    steps += [
        col_update('calc1', isunique=1, permissions={'default': []}),
        held('set', values={'string1': data['string1']}),
        held('write', id='unique existing value for calc value without access', expect_error=UNIQUE_ERR),
        held('new', sheet='main'),
        held('set', values={'string1': 'XXX'}),
        held('write'),
        held('find', id='non-existing value written', clear=True, current_id='@held:r', observe=['string1'],
             expect={'values': {'string1': 'XXX'}}),
        col_update('calc1', isunique=0),
    ]
    return {'_generator': GEN, 'name': '005_unique', 'source': ['t/005_unique.t'],
            'description': 'Unique field values, including calc and child records', 'steps': steps}


DATEFORMAT_TESTS = {
    'yyyy-MM-dd': {
        'data': [
            {'date1': '2010-10-10', 'daterange1': ['2000-10-10', '2001-10-10']},
            {'date1': '2011-10-10', 'daterange1': ['2009-08-10', '2012-10-10']},
            {'date1': '2001-10-10', 'daterange1': ['2008-08-10', '2011-10-10']},
        ],
        'search': {'valid': '2010-10-10', 'invalid': '10-10-2010', 'calc': '2008-08-10'},
        'retrieved': {'date': '2010-10-10', 'daterange': '2009-08-10 to 2012-10-10'},
    },
    'dd-MM-yyyy': {
        'data': [
            {'date1': '10-10-2010', 'daterange1': ['10-10-2000', '10-10-2001']},
            {'date1': '10-10-2011', 'daterange1': ['10-08-2009', '10-10-2012']},
            {'date1': '10-10-2001', 'daterange1': ['10-08-2008', '10-10-2011']},
        ],
        'search': {'valid': '10-10-2010', 'invalid': '2010-10-10', 'calc': '10-08-2008'},
        'retrieved': {'date': '10-10-2010', 'daterange': '10-08-2009 to 10-10-2012'},
    },
}


def dateformat_scenario(fmt):
    t = DATEFORMAT_TESTS[fmt]

    def rule(col, value, op='equal'):
        return {'rules': [{'name': col, 'type': 'date', 'value': value, 'operator': op}]}

    def search(id_, expect, **kw):
        s = {'id': id_, 'sheet': 'main', 'view_ref': 'v', 'expect': expect}
        s.update(kw)
        return {'search': s}

    def view_filter(f, **kw):
        v = {'op': 'update', 'sheet': 'main', 'ref': 'v', 'filter': f}
        v.update(kw)
        return {'view': v}
    s = t['search']
    steps = [
        {'sheet': {'as': 'main', 'data': t['data'], 'calc_return_type': 'date',
                   'calc_code': 'function evaluate (L1daterange1) \n return L1daterange1.from.epoch \n end'}},
        {'view': {'op': 'create', 'sheet': 'main', 'as': 'v', 'name': 'Test view',
                  'filter': rule('date1', s['valid']), 'columns': ['date1', 'daterange1']}},
        search('records for date search', {'count': 1}, observe=['date1'],
               expect_first={'date1': t['retrieved']['date']}),
        search('date filter and additional calendar search', {'count': 1},
               **{'from': '2010-10-01', 'to': '2010-11-01'}),
        view_filter(rule('daterange1', s['valid'], 'contains')),
        search('records for daterange search', {'count': 2}, observe=['daterange1'],
               expect_first={'daterange1': t['retrieved']['daterange']}),
        view_filter(rule('calc1', s['calc'])),
        search(f'calc date search, format {fmt}', {'count': 1}),
        search(f'quick search, format {fmt}', {'ids': ['@any']}, search=s['valid'], no_count=True),
        search(f'quick search for calc, format {fmt}', {'ids': ['@any']}, search=s['calc'], no_count=True),
        view_filter(rule('date1', s['invalid']), id='filter with invalid date', expect_error='.'),
        view_filter(rule('date1', ''), id='filter with empty string', expect_error='.'),
    ]
    return {'_generator': GEN, 'name': '005_dateformat_' + fmt.replace('-', ''),
            'source': ['t/005_dateformat.t'],
            'description': f'Date format {fmt}: input, display, searching and filters',
            'config': {'dateformat': fmt}, 'steps': steps}


def main():
    docs = [unique_scenario()] + [dateformat_scenario(f) for f in DATEFORMAT_TESTS]
    for d in docs:
        write_scenario(os.path.join(OUT, d['name'] + '.yaml'), d)
        print('wrote', d['name'])



# ----------------------------------------------------------------------
# t/005_multivalue.t
# ----------------------------------------------------------------------

CURVAL_FOO = 'Foo, 50, foo1, , 2014-10-10, 2012-02-10 to 2013-06-15, , , c_amber, 2012'
CURVAL_BAR = 'Bar, 99, foo2, , 2009-01-02, 2008-05-04 to 2008-07-14, , , b_red, 2008'

MV_TESTS = [
    dict(name='Write 2 values',
         write={'enum1': [7, 8], 'curval1': [1, 2], 'curval3': [1], 'tree1': [13, 14]},
         as_string={'enum1': 'foo1, foo2', 'enum2': 'foo1', 'curval1': CURVAL_FOO + '; ' + CURVAL_BAR,
                    'curval2': CURVAL_BAR, 'curval3': CURVAL_FOO, 'tree1': 'tree1, tree2', 'tree2': 'tree1',
                    'calc1': 'foo1foo2BarFootree1tree2'},
         search=[{'column': 'enum1', 'value': 'foo2'}], count=2),
    dict(name='Search 2 values',
         write={'enum1': [7, 8], 'enum2': [10, 11], 'tree1': 13, 'tree2': 16},
         as_string={'enum1': 'foo1, foo2', 'enum2': 'foo1, foo2', 'curval1': CURVAL_FOO + '; ' + CURVAL_BAR,
                    'curval2': CURVAL_BAR, 'tree1': 'tree1', 'tree2': 'tree1', 'calc1': 'foo1foo2BarFootree1'},
         search=[{'column': 'enum1', 'value': 'foo1'}, {'column': 'enum2', 'value': 'foo1'}], count=1),
    dict(name='Search 2 tree values',
         write={'tree1': [13, 14], 'tree2': [16, 17]},
         as_string={'enum1': 'foo1, foo2', 'enum2': 'foo1, foo2', 'curval1': CURVAL_FOO + '; ' + CURVAL_BAR,
                    'curval2': CURVAL_BAR, 'tree1': 'tree1, tree2', 'tree2': 'tree1, tree2',
                    'calc1': 'foo1foo2BarFootree1tree2'},
         search=[{'column': 'tree1', 'value': 'tree1'}, {'column': 'tree2', 'value': 'tree1'}], count=1),
    dict(name='Search negative 1', write={'enum1': [7, 8], 'enum2': [11, 12]},
         search=[{'column': 'enum1', 'value': 'foo1', 'operator': 'not_equal'}], count=2),
    dict(name='Search negative 2', write={'enum1': [7, 8], 'enum2': [10, 11]},
         search=[{'column': 'enum1', 'value': 'foo2', 'operator': 'not_equal'}], count=1),
    dict(name='Search negative 3', write={'enum1': [7, 8], 'enum2': [11, 12]},
         search=[{'column': 'enum1', 'value': ['foo1', 'foo2'], 'operator': 'not_equal'}], count=1),
    dict(name='Search negative 1 tree', write={'tree1': [13, 14], 'tree2': [17, 18]},
         search=[{'column': 'tree1', 'value': 'tree1', 'operator': 'not_equal'}], count=2),
    dict(name='Search negative 2 tree', write={'tree1': [13, 14], 'tree2': [16, 17]},
         search=[{'column': 'tree1', 'value': 'tree2', 'operator': 'not_equal'}], count=1),
    dict(name='Search negative 3 tree', write={'tree1': [13, 14], 'tree2': [17, 18]},
         search=[{'column': 'tree1', 'value': ['tree1', 'tree2'], 'operator': 'not_equal'}], count=1),
]

MV_CALC = '''
        function evaluate (L1enum1, L1curval1, L1tree1)
            values = {}
            for k, v in pairs(L1enum1.values) do table.insert(values, v.value) end
            table.sort(values)
            local text = ''
            for i,v in ipairs(values) do
                text = text .. v
            end
            for i,v in ipairs(L1curval1) do
                text = text .. v.field_values.L2string1[1]
            end
            for i,v in ipairs(L1tree1) do
                text = text .. v.value
            end
            return text
        end
    '''


def multivalue_scenario():
    data = [
        {'string1': 'Foo', 'enum1': 7, 'enum2': 10, 'tree1': 13, 'tree2': 16, 'curval1': 1, 'curval2': 2},
        {'string1': 'Bar', 'enum1': 8, 'enum2': 11, 'tree1': 14, 'tree2': 17, 'curval1': 1, 'curval2': 2},
        {'string1': 'FooBar', 'enum1': 9, 'enum2': 12, 'tree1': 15, 'tree2': 18, 'curval1': 1, 'curval2': 2},
    ]
    mv_cols = ['enum1', 'enum2', 'curval1', 'curval2', 'tree1', 'tree2']
    steps = [
        {'sheet': {'as': 'curval', 'instance_id': 2, 'multivalue': 1}},
        {'sheet': {'as': 'main', 'own_group': True, 'data': data, 'curval': 'curval', 'create_records': False,
                   'column_count': {'enum': 2, 'curval': 3, 'tree': 2},
                   'calc_code': MV_CALC, 'calc_return_type': 'string'}},
    ]
    steps += [col_update(c, multivalue=1) for c in mv_cols]
    steps.append({'create_records': {'sheet': 'main'}})
    for i, t in enumerate(MV_TESTS):
        n = t['name']
        steps.append(held('find', sheet='main', current_id=3))
        steps.append(held('set', values=[{k: t['write'][k]} for k in sorted(t['write'])]))
        steps.append(held('write'))
        if 'as_string' in t:
            steps.append(held('find', id=f'{n}: updated', clear=True, current_id=3,
                              observe=sorted(t['as_string']), expect={'values': t['as_string']}))
        else:
            steps.append(held('find', clear=True, current_id=3))
        rules = [{'name': s['column'], 'type': 'string', 'value': s['value'],
                  'operator': s.get('operator', 'equal')} for s in t['search']]
        steps.append({'view': {'op': 'create', 'sheet': 'main', 'as': f'v{i}', 'name': 'Test view',
                               'filter': {'rules': rules, 'condition': 'OR'}, 'columns': '@all'}})
        steps.append({'search': {'id': f'{n}: count', 'sheet': 'main', 'view_ref': f'v{i}',
                                 'expect': {'count': t['count']}}})
        single = held('single', id=f'{n}: first result', sheet='main', view_ref=f'v{i}')
        if 'as_string' in t:
            single['held'].update(observe=sorted(t['as_string']), expect={'values': t['as_string']})
        steps.append(single)
    # Fields set back to single value still display existing multi-values
    steps += [col_update(c, multivalue=0) for c in mv_cols]
    expected = {
        'enum1': 'foo1, foo2',
        'enum2': 'foo2, foo3',
        'curval1': CURVAL_FOO + '; ' + CURVAL_BAR,
        'curval2': CURVAL_BAR,
        'tree1': 'tree1, tree2',
        'tree2': 'tree2, tree3',
    }
    steps.append(held('find', id='single field with multiple values (single retrieval)', clear=True,
                      current_id=3, observe=sorted(expected), expect={'values': expected}))
    steps.append(held('single', id='single field with multiple values (multiple retrieval)', sheet='main',
                      observe=sorted(expected), expect={'values': expected}))
    return {'_generator': GEN, 'name': '005_multivalue', 'source': ['t/005_multivalue.t'],
            'description': 'Multivalue enum, tree and curval fields: values, code, searches',
            'steps': steps}


main_docs = main


def main():  # noqa: F811
    main_docs()
    d = multivalue_scenario()
    write_scenario(os.path.join(OUT, d['name'] + '.yaml'), d)
    print('wrote', d['name'])


if __name__ == '__main__':
    main()
