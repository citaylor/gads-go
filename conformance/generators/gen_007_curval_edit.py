#!/usr/bin/env python3
"""Generate scenarios/007_curval_edit.yaml from t/007_curval_edit.t"""
import yaml, os

main_calc = """function evaluate (L1curval1)
    if L1curval1 == nil then
        return ""
    end
    ret = ""
    -- Allow values to be single or multi value
    if L1curval1.field_values ~= nil then
        ret = ret .. L1curval1.field_values.L2string1
    else
        for _, curval in ipairs(L1curval1) do
            ret = ret .. curval.field_values.L2string1
        end
    end
    return ret
end"""
cv_calc = """
function evaluate (L2autocur1)
    return_value = ''
    for _, v in pairs(L2autocur1) do
        return_value = return_value .. v.field_values.L1integer1
    end
    return return_value
end
"""
U = 'normal1'
steps = []
S = steps.append

def q(s):
    return '@field:string1=' + s

def count(label):
    S({'db': {'id': label, 'op': 'count', 'table': 'current', 'where': {'instance_id': '@instance:cv'}}})

def held(op, as_, **kw):
    d = {'op': op, 'as': as_}
    d.update(kw)
    S({'held': d})

def find_main(as_, label, cid=3, **kw):
    held('find', as_, id=label, sheet='main', clear=True, current_id=cid,
         observe=['calc1', 'curval1'], **kw)

for test in ['delete_not_used', 'typeahead', 'dropdown', 'noshow']:
    dnu = 1 if test == 'delete_not_used' else 0
    vs = 'noshow' if test == 'delete_not_used' else test
    T = test + ': '
    S({'reset': {}})
    S({'sheet': {'as': 'cv', 'instance_id': 2, 'site_id': 1}})
    S({'sheet': {'as': 'main', 'curval': 'cv', 'column_count': {'curval': 2}, 'curval_offset': 6,
                 'curval_fields': ['string1'], 'calc_return_type': 'string', 'calc_code': main_calc}})
    S({'sheet': {'as': 'main2', 'instance_id': 3, 'curval_offset': 12, 'curval': 'cv',
                 'curval_fields': ['string1'], 'data': []}})
    S({'layout': {'op': 'set_user', 'sheet': 'main', 'user': U}})
    S({'clear_layouts': {}})
    S({'column': {'op': 'create', 'sheet': 'cv', 'type': 'curval', 'refers_to': 'main',
                  'curval_fields': ['string1'], 'name': 'Curval back to main table',
                  'name_short': 'L2curval2'}})
    S({'column': {'op': 'update', 'sheet': 'cv', 'column': 'daterange1', 'set': {'permissions': 'none'}}})
    S({'add_autocur': {'sheet': 'cv', 'refers_to': 'main', 'related_field': 'curval1',
                       'curval_fields': ['string1']}})
    S({'column': {'op': 'update', 'sheet': 'cv', 'column': 'calc1', 'set': {'code': cv_calc}}})
    S({'add_autocur': {'sheet': 'cv', 'refers_to': 'main2', 'related_field': 'curval1',
                       'curval_fields': ['string1']}})
    S({'clear_layouts': {}})
    for c in ['curval1', 'curval2']:
        S({'column': {'op': 'update', 'sheet': 'main', 'column': c,
                      'set': {'delete_not_used': dnu, 'show_add': 1, 'value_selector': vs},
                      'write_options': {'no_alerts': 1}}})
    S({'column': {'op': 'update', 'sheet': 'main', 'column': 'daterange1', 'set': {'isunique': 1}}})
    S({'clear_layouts': {}})

    # Brand new record
    held('new', 'n', sheet='main', user=U)
    held('set', 'n', values={'integer1': 10, 'curval1': [q('foo55')]})
    held('write', 'n', id=T + 'new write', observe=['calc1'])
    held('find', 'n', id=T + 'new load', sheet='main', clear=True, current_id='@held:n',
         observe=['calc1', 'curval1'])

    held('find', 'r', id=T + 'blank to begin', sheet='main', user=U, current_id=3, observe=['curval1'])
    if vs == 'dropdown':
        held('get', 'r', id=T + 'dropdown initial', filtered_values='curval1', filtered_full=True)

    # Add a value on write
    count(T + 'count before add')
    held('set', 'r', values={'curval1': [q('foo1')]})
    held('write', 'r', id=T + 'add write', observe=['calc1'])
    count(T + 'count after add')
    find_main('r', T + 'add load', datum=['curval1'], datum_props=['as_string', 'ids'])
    held('find', 'cr', id=T + 'curval record', sheet='cv', user=U, current_id=7, observe=['calc1'])

    # Add new, keep existing
    held('set', 'r', values={'curval1': [q('foo2'), {'own_id': '^foo1$'}]})
    held('write', 'r', id=T + 'add second write', observe=['calc1'])
    count(T + 'count after second')
    find_main('r', T + 'add second load')
    held('find', 'cr', id=T + 'autocur calc', sheet='cv', clear=True, current_id=7, observe=['calc1'])

    # Edit existing
    find_main('r', T + 'edit reload')
    held('set', 'r', values={'curval1': [{'query': q('foo5'), 'own_id': '^foo2$'}, {'own_id': '^foo1$'}]})
    held('write', 'r')
    count(T + 'count after edit')
    find_main('r', T + 'edit load')

    # One edited via query but unchanged, other changed
    held('set', 'r', values={'curval1': [{'query': q('foo1'), 'own_id': '^foo1$'},
                                         {'query': q('foo6'), 'own_id': '^foo5$'}]})
    held('write', 'r')
    count(T + 'count after edit 2')
    find_main('r', T + 'edit 2 load')

    # No actual change
    held('find', 'r2', sheet='main', user=U, current_id=3)
    held('set', 'r2', values=[{'curval1': '@own_queries'}, {'date1': '2020-10-10'}])
    held('write', 'r2')
    count(T + 'count after no change')
    find_main('r2', T + 'no change load')

    # Delete existing
    held('set', 'r2', values={'curval1': [{'own_id': '^foo1$'}]})
    held('write', 'r2')
    count(T + 'count after delete')
    S({'db': {'id': T + 'live after delete', 'op': 'count', 'table': 'current',
              'where': {'instance_id': '@instance:cv', 'deleted': None}}})
    find_main('r2', T + 'delete load')
    held('find', 'r2', id=T + 'old version', sheet='main', clear=True, current_id=3, version=1,
         observe=['curval1'])

    if vs == 'dropdown':
        held('find', 'u1', sheet='main', user=U, current_id=3)
        held('set', 'u1', values={'daterange1': ['2020-10-10', '2021-10-10']})
        held('write', 'u1')
        held('new', 'u2', sheet='main', user=U)
        held('set', 'u2', values={'daterange1': ['2020-10-10', '2021-10-10']})
        held('write', 'u2', id=T + 'unique fail', expect_error='must be unique')
        held('get', 'u2', id=T + 'filtered after unique fail', filtered_values='curval1')

    # Save draft
    held('new', 'd', sheet='main', user=U)
    held('get', 'd', id=T + 'draft filtered before', filtered_values='curval1')
    held('set', 'd', values={'curval1': [q('foo10'), q('foo20')], 'curval2': [q('foo145')],
                             'integer1': 10})
    held('write', 'd', options={'draft': 1})
    held('clear', 'd')
    held('load_remembered_values', 'd')
    if vs == 'dropdown':
        held('get', 'd', id=T + 'draft filtered', filtered_values='curval1', filtered_full=True)
        S({'layout': {'op': 'set_user', 'sheet': 'cv', 'user': U}})
        S({'clear_layouts': {}})
        held('new', 'm', sheet='cv', user=U)
        S({'db': {'id': T + 'main non-draft count', 'op': 'count', 'table': 'current',
                  'where': {'instance_id': '@instance:main', 'draftuser_id': None, 'deleted': None}}})
        held('load_remembered_values', 'm')
        held('get', 'm', id=T + 'modal filtered', filtered_values='Curval back to main table')
        held('find', 'ex', sheet='main', user=U, current_id=3)
        held('get', 'ex', id=T + 'existing filtered', filtered_values='curval1')
    held('get', 'd', id=T + 'draft form', datum=['curval1', 'curval2'],
         datum_props=['blank', 'html_form', 'html_form_query'])
    held('set_from_form', 'd', **{'from': 'd', 'columns': ['curval1'], 'as_query': True,
                                   'replace': ['foo20', 'foo30']})
    held('set_from_form', 'd', **{'from': 'd', 'columns': ['curval2'], 'as_query': True})
    held('write', 'd')
    held('find', 'd', id=T + 'draft submitted', sheet='main', clear=True, current_id='@held:d',
         observe=['curval1', 'curval2'])

    # Curval record value before main record write
    held('find', 'r2', sheet='main', clear=True, current_id=3)
    held('set', 'r2', id=T + 'curval before write', values={'curval1': [q('foo10')]},
         datum=['curval1'], datum_props=['as_string'])

    # Failed writes
    for is_draft in range(3):
        L = T + 'fail %d: ' % is_draft
        S({'column': {'op': 'update', 'sheet': 'main', 'column': 'string1', 'set': {'optional': 0}}})
        S({'clear_layouts': {}})
        a = 'f%d' % is_draft
        held('new', a, sheet='main', user=U)
        held('get', a, id=L + 'filtered before', filtered_values='curval1')
        held('set', a, values={'curval1': [q('foo99')], 'integer1': 50})
        if is_draft == 1:
            held('write', a, options={'draft': 1})
        else:
            held('write', a, id=L + 'write', expect_error='is not optional')
            kw = {}
            if vs == 'dropdown':
                kw = {'filtered_values': 'curval1', 'filtered_full': True}
            held('get', a, id=L + 'after fail', datum=['curval1'], datum_props=['blank'], **kw)

# Curval value within curval subfield
S({'reset': {}})
S({'sheet': {'as': 'cs2', 'instance_id': 4}})
S({'sheet': {'as': 'cs1', 'data': [], 'curval': 'cs2', 'curval_fields': ['string1'], 'instance_id': 2}})
S({'sheet': {'as': 'main', 'data': [], 'curval': 'cs1', 'curval_fields': ['string1']}})
S({'layout': {'op': 'set_user', 'sheet': 'main', 'user': U}})
S({'column': {'op': 'update', 'sheet': 'main', 'column': 'curval1',
              'set': {'show_add': 1, 'value_selector': 'noshow'}, 'write_options': {'no_alerts': 1}}})
S({'clear_layouts': {}})
held('new', 'c', sheet='main', user=U)
held('set', 'c', values={'curval1': ['@field:curval1=1&@field:string1=foobars']})
held('write', 'c', options={'draft': 1})
held('clear', 'c')
held('load_remembered_values', 'c', id='inner curval draft form', datum=['curval1'],
     datum_props=['blank', 'html_form', 'html_form_query'])
held('set_from_form', 'c', **{'from': 'c', 'columns': ['curval1'], 'as_query': True})
held('write', 'c')
held('find', 'c', id='inner curval saved', sheet='main', clear=True, current_id='@held:c',
     observe=['curval1'])

out = {'name': '007_curval_edit', 'source': ['t/007_curval_edit.t'],
       'description': 'Adding, editing and removing curval records from the parent record edit',
       'steps': steps}
p = os.path.join(os.path.dirname(__file__), '..', 'scenarios', '007_curval_edit.yaml')
with open(p, 'w') as f:
    yaml.safe_dump(out, f, sort_keys=False, allow_unicode=True)
