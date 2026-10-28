#!/usr/bin/env python3
"""Generate the 011_alert_limit scenarios from upstream t/011_alert_limit.t
(alerts on a system with views limited for users), with the limited view on
the whole table and on the user itself."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_011_alert_limit.py'
DATE_RULES = {'rules': [{'name': 'date1', 'type': 'date', 'value': '2014-10-10', 'operator': 'equal'}]}


def counts(id, cache, send):
    return [
        {'db': {'id': id + ' alert cache', 'op': 'count', 'table': 'alert_cache', 'expect': {'count': str(cache)}}},
        {'db': {'id': id + ' alert send', 'op': 'count', 'table': 'alert_send', 'expect': {'count': str(send)}}},
    ]


def write(string1):
    return {'record': {'op': 'create', 'sheet': 'main', 'user': 'normal1',
                       'values': {'string1': string1, 'date1': '2014-10-10'}, 'options': {'no_alerts': 0}}}


def scenario(type):
    steps = [
        {'sheet': {'as': 'main', 'data': [
            {'string1': 'Foo', 'date1': '2014-10-10'},
            {'string1': 'Bar', 'date1': '2014-10-10'},
            {'string1': 'Apple', 'date1': '2015-10-10'},
            {'string1': 'Pear', 'date1': '2015-10-10'},
        ]}},
        {'view': {'op': 'create', 'sheet': 'main', 'as': 'limit', 'name': 'Limit to view', 'is_admin': 1,
                  'filter': {'rules': [{'name': 'string1', 'type': 'string', 'value': 'Foo', 'operator': 'equal'}]}}},
    ]
    if type == 'table':
        # The limit on the table itself, with an override for the second
        # user so that they can see everything
        steps += [
            {'layout': {'op': 'update', 'sheet': 'main', 'view_limit': 'limit'}},
            {'view': {'op': 'create', 'sheet': 'main', 'as': 'all', 'name': 'Limit view for all records',
                      'is_admin': 1,
                      'filter': {'rules': [{'name': '_id', 'type': 'string', 'value': '0', 'operator': 'greater'}]}}},
            {'user': {'op': 'view_limits', 'user': 'normal2', 'views': ['all']}},
        ]
    else:
        steps.append({'user': {'op': 'view_limits', 'user': 'normal1', 'views': ['limit']}})
    steps += [
        {'search': {'id': 'limited to a view', 'sheet': 'main', 'user': 'normal1', 'no_results': True,
                    'expect': {'count': '1'}}},
        {'view': {'op': 'create', 'sheet': 'main', 'as': 'alerts', 'name': 'View with alerts',
                  'columns': ['string1'], 'filter': DATE_RULES}},
        {'alert': {'sheet': 'main', 'view': 'alerts', 'user': 'normal2', 'frequency': 24}},
    ]
    steps += counts('initial', 2, 0)
    steps.append(write('Foo2'))
    steps += counts('after write', 3, 1)
    # A user with a limited view does not receive alerts for records not in
    # the limited view
    steps.append({'alert': {'sheet': 'main', 'view': 'alerts', 'user': 'normal2', 'frequency': None}})
    steps += counts('alert removed', 0, 0)
    steps += [
        {'view': {'op': 'create', 'sheet': 'main', 'as': 'limited_alerts', 'name': 'Limited user view with alerts',
                  'columns': ['string1'], 'filter': DATE_RULES}},
        {'alert': {'sheet': 'main', 'view': 'limited_alerts', 'user': 'normal1', 'frequency': 24}},
    ]
    steps += counts('view limit user', 1, 0)
    steps.append(write('Foo3'))
    steps += counts('record outside view limit', 1, 0)
    steps.append(write('Foo'))
    steps += counts('record inside view limit', 2, 1)
    write_scenario(os.path.join(OUT, '011_alert_limit_%s.yaml' % type), {
        '_generator': GEN,
        'name': '011_alert_limit_%s' % type,
        'source': ['t/011_alert_limit.t'],
        'description': 'Alerts for records with views limited for users (limit on the %s)' % type,
        'steps': steps,
    })


def main():
    for type in ('table', 'user'):
        scenario(type)


if __name__ == '__main__':
    main()
