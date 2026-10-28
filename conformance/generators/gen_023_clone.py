#!/usr/bin/env python3
"""Generate the 023_clone scenario from upstream t/023_clone.t (cloning
records, including curval values that are shown for adding)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_023_clone.py'

DATA = {'integer1': 150, 'string1': 'Foobar', 'curval1': 1, 'date1': '2010-01-01',
        'daterange1': ['2011-02-02', '2012-02-02'], 'enum1': 'foo1', 'tree1': 'tree1'}
EXPECTED = {'integer1': '150', 'string1': 'Foobar', 'curval1': 'Foo', 'date1': '2010-01-01',
            'daterange1': '2011-02-02 to 2012-02-02', 'enum1': 'foo1', 'tree1': 'tree1'}
COLS = list(DATA)
U = 'normal1'


def held(op, as_, **kw):
    d = {'op': op, 'as': as_}
    d.update(kw)
    return {'held': d}


def find(as_, id=None, current_id=3, **kw):
    return held('find', as_, sheet='main', user=U, current_id=current_id, id=id, **kw) if id else \
        held('find', as_, sheet='main', user=U, current_id=current_id, **kw)


def written(id, as_, cols, props):
    return find(as_, id=id, current_id='@held:%s' % as_, clear=True, datum=cols, datum_props=props)


def main():
    steps = [
        {'sheet': {'as': 'curval', 'instance_id': 2}},
        {'sheet': {'as': 'main', 'curval': 'curval', 'curval_fields': ['string1'], 'data': [DATA]}},
        {'add_autocur': {'sheet': 'curval', 'refers_to': 'main', 'related_field': 'curval1', 'curval_fields': ['string1']}},
        find('r', id='values to begin with', datum=['string1', 'curval1'], datum_props=['as_string'],
             expect={'datum': {'string1': {'as_string': 'Foobar'}, 'curval1': {'as_string': 'Foo'}}}),
        held('get', 'r', id='curval ids to begin with', datum=['curval1'], datum_props=['ids']),
    ]
    # Standard clone first
    for reload in (0, 1):
        s, c = 'src%d' % reload, 'clone%d' % reload
        steps += [
            find(s),
            held('clone', s, to=c),
            held('get', c, id='clone pre-write (reload %d)' % reload, datum=COLS, datum_props=['as_string', 'text_all'],
                 expect={'datum': {k: {'as_string': v, 'text_all': [v]} for k, v in EXPECTED.items()}}),
        ]
        target = c
        if reload:
            target = 'new%d' % reload
            steps += [
                held('new', target, sheet='main', user=U),
                held('set_from_form', target, **{'from': c, 'columns': COLS}),
            ]
        steps += [
            held('write', target, id='write clone (reload %d)' % reload),
            written('clone after write (reload %d)' % reload, target, COLS, ['as_string', 'text_all']),
            held('get', target, id='clone curval ids (reload %d)' % reload, datum=['curval1'], datum_props=['ids']),
        ]
    # Curval allowing adding and removal
    steps += [
        {'column': {'op': 'update', 'sheet': 'main', 'column': 'curval1', 'set': {'show_add': 1, 'value_selector': 'noshow'}}},
        find('r', clear=True),
        held('clone', 'r', to='k'),
        held('write', 'k', id='write clone with no updates'),
        written('clone with no updates', 'k', ['string1', 'curval1'], ['as_string']),
        held('get', 'k', id='clone with no updates curval ids', datum=['curval1'], datum_props=['ids']),
    ]
    # Clone and update with no changes, then with changes (as for HTML form
    # submissions)
    for replace in (None, ['Foo', 'Foo2']):
        tag = 'changes' if replace else 'no changes'
        for reload in (0, 1):
            s, c = 'fsrc%s%d' % (tag[0], reload), 'fclone%s%d' % (tag[0], reload)
            steps += [
                find(s),
                held('clone', s, to=c, id='form clone %s (reload %d)' % (tag, reload), datum=['curval1'],
                     datum_props=['html_form']),
            ]
            target = c
            if reload:
                target = 'fnew%s%d' % (tag[0], reload)
                steps += [
                    held('new', target, sheet='main', user=U),
                    held('set', target, values={'string1': 'Foobar'}),
                ]
            form = {'from': c, 'columns': ['curval1'], 'as_query': True}
            if replace:
                form['replace'] = replace
            steps += [
                held('set_from_form', target, **form),
                held('write', target, id='write form clone %s (reload %d)' % (tag, reload)),
                written('form clone %s after write (reload %d)' % (tag, reload), target, ['string1', 'curval1'],
                        ['as_string']),
            ]
            steps[-1]['held']['expect'] = {'datum': {'string1': {'as_string': 'Foobar'},
                                                     'curval1': {'as_string': 'Foo2' if replace else 'Foo'}}}
            steps.append(held('get', target, id='form clone %s curval ids (reload %d)' % (tag, reload),
                              datum=['curval1'], datum_props=['ids']))
    write_scenario(os.path.join(OUT, '023_clone.yaml'), {
        '_generator': GEN,
        'name': '023_clone',
        'source': ['t/023_clone.t'],
        'description': 'Cloning records, before and after writing, including curval values shown for adding',
        'steps': steps,
    })


if __name__ == '__main__':
    main()
