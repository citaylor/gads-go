#!/usr/bin/env python3
"""Generate scenarios from upstream t/005_setvalues.t.

The test creates many independent DataSheets (each with its own database);
these become sections of the scenarios separated by `reset` steps.
"""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_005_setvalues.py'

CURVAL_FOO = 'Foo, 50, foo1, , 2014-10-10, 2012-02-10 to 2013-06-15, , , c_amber, 2012'
CURVAL_BAR = 'Bar, 99, foo2, , 2009-01-02, 2008-05-04 to 2008-07-14, , , b_red, 2008'

VALUES = {
    'string1': dict(old_as_string='foo', new='bar', new_as_string='bar'),
    'integer1': dict(old_as_string='100', new=200, new_as_string='200', addable='(+ 25)', addable_result='225'),
    'enum1': dict(old_as_string='foo1', new=8, new_as_string='foo2', filter_value='foo2'),
    'tree1': dict(old_as_string='tree1', new=11, new_as_string='tree2', filter_value='tree2'),
    'date1': dict(old_as_string='2010-10-10', new='2011-10-10', new_as_string='2011-10-10',
                  addable='+ 1 year', addable_result='2012-10-10'),
    'daterange1': dict(old_as_string='2000-10-10 to 2001-10-10', new=['2000-11-11', '2001-11-11'],
                       new_as_string='2000-11-11 to 2001-11-11', filter_value='2000-11-11 to 2001-11-11',
                       addable=['+ 1 week', '+ 5 years'], addable_result='2000-11-18 to 2006-11-11'),
    'curval1': dict(old_as_string=CURVAL_FOO, new=2, new_as_string=CURVAL_BAR),
    'curval2': dict(old_as_string=CURVAL_FOO, new=2, new_as_string=CURVAL_BAR),
    'person1': dict(old_as_string='User1, User1',
                    new={'id': 2, 'username': 'user2@example.com', 'email': 'user2@example.com',
                         'value': 'User2, User2'},
                    new_as_string='User2, User2', new_html_form=2, new_html='User2, User2',
                    filter_value='User2, User2'),
    'file1': dict(old_as_string='file1.txt',
                  new={'name': 'file2.txt', 'mimetype': 'text/plain', 'content': 'Text file2'},
                  new_html_form=2, new_as_string='file2.txt', new_html='file2.txt', filter_value='file2.txt'),
    'calc1': dict(new='2000', filter_value='2000'),
    'rag1': dict(new='b_red', filter_value='b_red'),
}
USERINPUT = [t for t in VALUES if t not in ('calc1', 'rag1')]

DATA = {
    'blank': [{
        'string1': '', 'integer1': '', 'enum1': '', 'tree1': '', 'date1': '', 'daterange1': ['', ''],
        'curval1': '', 'curval2': '', 'file1': '', 'person1': '',
    }],
    'changed': [{
        'string1': 'foo', 'integer1': '100', 'enum1': 7, 'tree1': 10, 'date1': '2010-10-10',
        'daterange1': ['2000-10-10', '2001-10-10'], 'curval1': 1, 'curval2': 1, 'person1': 1,
        'file1': {'name': 'file1.txt', 'mimetype': 'text/plain', 'content': 'Text file1'},
    }],
    'nochange': [{
        'string1': 'bar', 'integer1': '200', 'enum1': 8, 'tree1': 11, 'date1': '2011-10-10',
        'daterange1': ['2000-11-11', '2001-11-11'], 'curval1': 2, 'curval2': 2, 'person1': 2,
        'file1': {'name': 'file2.txt', 'mimetype': 'text/plain', 'content': 'Text file2'},
    }],
}


def held(op, **kw):
    kw['op'] = op
    kw.setdefault('as', 'r')
    return {'held': kw}


def sheets(multivalue=0, data=None, create_records=True, site_id=None, **main):
    curval = {'as': 'curval', 'instance_id': 2}
    if site_id:
        curval['site_id'] = site_id
    m = {'as': 'main', 'own_group': True, 'curval': 'curval', 'multivalue': multivalue,
         'column_count': {'curval': 2}, 'create_records': create_records}
    if data is not None:
        m['data'] = data
    m.update(main)
    return [
        {'reset': {}},
        {'sheet': curval},
        {'sheet': m},
        # Add curval column with show_add
        {'column': {'op': 'update', 'sheet': 'main', 'column': 'curval2', 'set': {'show_add': 1}}},
    ]


def new_record_steps():
    steps = []
    for mv in (0, 1):
        steps += sheets(mv, create_records=False)
        steps.append(held('new', sheet='main'))
        steps.append(held('get', id=f'new record blank values (multivalue {mv})', datum=USERINPUT,
                          datum_props=['as_string', 'empty_value', 'id_hash'],
                          expect={'datum': {t: {'as_string': '', 'empty_value': 1} for t in USERINPUT}}))
    return steps


def main_loop_steps(multivalue, test):
    steps = []
    is_multi = ' for multivalue' if multivalue else ''
    for arrayref in (0, 1):
        for deleted in (0, 1):
            p = f'{test} mv{multivalue} arrayref{arrayref} deleted{deleted}'
            steps += sheets(multivalue, data=DATA[test], site_id=1)
            if deleted:
                steps.append({'db': {'table': 'enumval', 'set': {'deleted': 1}, 'clear_layouts': True}})
            steps.append({'search': {'id': f'{p}: one record', 'sheet': 'main', 'expect': {'count': 1}}})
            steps.append(held('single', sheet='main'))
            for t in sorted(VALUES):
                v = VALUES[t]
                if deleted and t not in ('enum1', 'tree1'):
                    continue
                if arrayref and t == 'daterange1':
                    continue
                q = f'{p} {t}'
                userinput = t in USERINPUT
                if userinput:
                    steps.append(held('get', id=f'{q}: blank status{is_multi}', datum=[t], datum_props=['blank'],
                                      expect={'datum': {t: {'blank': 1 if test == 'blank' else 0}}}))
                    s = held('set', id=f'{q}: set new value', values={t: [v['new']] if arrayref else v['new']})
                    if deleted and test != 'nochange':
                        s['held']['expect_error'] = 'has been deleted' if 'tree' in t else 'is not a valid'
                    steps.append(s)
                if test == 'changed' and not deleted:
                    fv = v.get('filter_value', v['new'])
                    steps.append(held('get', id=f'{q}: filter value', datum=[t], datum_props=['filter_value'],
                                      expect={'datum': {t: {'filter_value': str(fv)}}}))
                    init = v.get('new_html_form', v['new'])
                    steps.append({'datum': {'id': f'{q}: filter value (grouped datum)', 'sheet': 'main',
                                            'column': t, 'init_value': [init], 'datum_props': ['filter_value'],
                                            'expect': {'filter_value': str(fv)}}})
                if not userinput or deleted:
                    continue
                props = ['changed', 'oldvalue', 'as_string', 'html']
                exp = {'changed': 1 if test in ('blank', 'changed') else 0,
                       'as_string': v['new_as_string'], 'html': v.get('new_html', v['new_as_string'])}
                if test in ('changed', 'nochange'):
                    if t != 'curval2':
                        props.append('html_form')
                        old = v['old_as_string'] if test == 'changed' else v['new_as_string']
                        exp['oldvalue'] = {'as_string': old}
                        hf = v.get('new_html_form', v['new'])
                        exp['html_form'] = [str(x) for x in (hf if isinstance(hf, list) else [hf])]
                else:
                    exp['oldvalue'] = {'blank': 1}
                steps.append(held('get', id=f'{q}: after set{is_multi}', datum=[t], datum_props=props,
                                  expect={'datum': {t: exp}}))
                if test == 'blank':
                    blank = DATA['blank'][0][t]
                    steps.append(held('set', values={t: [blank] if arrayref else blank}))
                    steps.append(held('get', id=f'{q}: set to blank{is_multi}', datum=[t], datum_props=['blank'],
                                      expect={'datum': {t: {'blank': 1}}}))
                    if 'addable' in v:
                        steps.append(held('set', values={t: v['addable']}, set_options={'bulk': 1}))
                        steps.append(held('get', id=f'{q}: blank after addable{is_multi}', datum=[t],
                                          datum_props=['blank'], expect={'datum': {t: {'blank': 1}}}))
                elif test == 'changed' and 'addable' in v:
                    steps.append(held('set', values={t: v['addable']}, set_options={'bulk': 1}))
                    steps.append(held('get', id=f'{q}: after addable change{is_multi}', datum=[t],
                                      datum_props=['as_string'],
                                      expect={'datum': {t: {'as_string': v['addable_result']}}}))
    return steps


def undefined_steps():
    steps = sheets(0)
    for t in USERINPUT:
        steps.append({'datum': {'id': f'{t} has changed from undefined', 'sheet': 'main', 'column': t,
                                'init_no_value': True, 'set_value': VALUES[t]['new'],
                                'datum_props': ['changed'], 'expect': {'changed': 1}}})
        steps.append({'datum': {'id': f'{t} has not changed from undefined to blank', 'sheet': 'main',
                                'column': t, 'init_no_value': True, 'set_value': DATA['blank'][0][t],
                                'datum_props': ['changed'], 'expect': {'changed': 0}}})
    return steps


def mandatory_steps():
    # In order of $layout->all(userinput => 1)
    cols = ['string1', 'integer1', 'enum1', 'tree1', 'date1', 'daterange1', 'file1', 'person1', 'curval1']
    steps = [
        {'reset': {}},
        {'sheet': {'as': 'curval', 'instance_id': 2}},
        {'sheet': {'as': 'main', 'own_group': True, 'optional': 0, 'data': [], 'curval': 'curval'}},
        held('new', sheet='main'),
    ]
    for c in cols:
        steps.append(held('write', id=f'mandatory {c}', expect_error=c))
        steps.append(held('set', values={c: VALUES[c]['new']}))

    def upd(c, **set_):
        return {'column': {'op': 'update', 'sheet': 'main', 'column': c, 'set': set_}}
    # User without write access to a mandatory field can still save record
    steps += [upd(c, optional=1) for c in cols if c != 'string1']
    steps += [
        held('set', values={'string1': ''}),
        held('write', id='mandatory string with permission', expect_error='is not optional'),
        upd('string1', permissions={'default': []}, permissions_sheet='main'),
        held('write', id='no error writing without permission to mandatory value'),
        upd('string1', permissions='default', permissions_sheet='main'),
    ]
    steps += [upd(c, optional=0) for c in cols]
    # Filtered value for next page
    for c in cols:
        if c == 'curval1':
            steps.append(upd(c, filter={'rules': [{'name': 'string1', 'type': 'string', 'value': '$L1string1',
                                                   'operator': 'equal'}]}))
        else:
            steps.append(upd(c, optional=1))
    steps += [
        held('new', sheet='main'),
        held('set', values={'string1': 'foobar'}),
        held('write', id='missing curval filtered field value', expect_error='curval1'),
        upd('curval1', permissions={'default': []}, permissions_sheet='main'),
        {'db': {'id': 'record count before', 'op': 'count', 'table': 'record', 'expect': {'count': 3}}},
        held('write', id='mandatory field without write access on second page'),
        {'db': {'id': 'one record written', 'op': 'count', 'table': 'record', 'expect': {'count': 4}}},
    ]
    return steps


def person_text_steps():
    pid = 'person1'
    return [
        {'reset': {}},
        {'sheet': {'as': 'main'}},
        held('single', sheet='main', id='person field initially blank', datum=[pid], datum_props=['blank'],
             expect={'datum': {pid: {'blank': 1}}}),
        held('set', values={pid: 'User1, User1'}),
        held('write'),
        held('find', id='person updated using textual name', clear=True, current_id='@held:r', datum=[pid],
             datum_props=['id'], expect={'datum': {pid: {'id': 1}}}),
        held('set', values={pid: 'User2 User2'}),
        held('write'),
        held('find', id='person updated using textual name (2)', clear=True, current_id='@held:r', datum=[pid],
             datum_props=['id'], expect={'datum': {pid: {'id': 2}}}),
    ]


def file_steps():
    internal = ['_id', '_version_datetime', '_version_user', '_created', '_serial']
    return [
        {'reset': {}},
        # file1 undefined creates default dummy file
        {'sheet': {'as': 'main', 'data': [{'file1': None}]}},
        held('single', sheet='main'),
        held('set', id='update with same file ID has not changed', values={'file1': 1}, datum=['file1'],
             datum_props=['changed'], expect={'datum': {'file1': {'changed': 0}}}),
        held('get', id='internal values not blank for existing records', datum=internal, datum_props=['blank'],
             expect={'datum': {c: {'blank': 0} for c in internal}}),
    ]


def main():
    docs = [{'_generator': GEN, 'name': '005_setvalues_new', 'source': ['t/005_setvalues.t'],
             'description': 'Blank values of new records', 'steps': new_record_steps()}]
    for mv in (0, 1):
        for test in ('blank', 'nochange', 'changed'):
            docs.append({'_generator': GEN, 'name': f'005_setvalues_{test}_mv{mv}', 'source': ['t/005_setvalues.t'],
                         'description': f'Setting values on records ({test}, multivalue {mv})',
                         'steps': main_loop_steps(mv, test)})
    docs.append({'_generator': GEN, 'name': '005_setvalues_other', 'source': ['t/005_setvalues.t'],
                 'description': 'Undefined initial values, mandatory fields, person by name, unchanged file',
                 'steps': undefined_steps() + mandatory_steps() + person_text_steps() + file_steps()})
    for d in docs:
        write_scenario(os.path.join(OUT, d['name'] + '.yaml'), d)
        print('wrote', d['name'], len(d['steps']))


if __name__ == '__main__':
    main()
