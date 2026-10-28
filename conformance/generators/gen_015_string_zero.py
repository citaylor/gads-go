#!/usr/bin/env python3
"""Generate the 015_string_zero scenario from upstream t/015_string_zero.t
(zero versus empty string values for string, integer and calc)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario, rule  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'scenarios')
GEN = 'generators/gen_015_string_zero.py'
COLS = ['string1', 'integer1', 'calc1']


def main():
    steps = []
    for i, value in enumerate(['', '0']):
        blank = 1 if value == '' else 0
        if i:
            steps.append({'reset': {}})
        steps += [
            {'sheet': {'as': 'main', 'data': [], 'calc_code': 'function evaluate (L1integer1)\nreturn L1integer1\nend'}},
            {'held': {'op': 'new', 'as': 'r', 'sheet': 'main'}},
            {'held': {'id': "set '%s'" % value, 'op': 'set', 'as': 'r', 'values': {'string1': value, 'integer1': value},
                      'datum': COLS, 'datum_props': ['blank'],
                      'expect': {'datum': {c: {'blank': blank} for c in COLS}}}},
            {'held': {'op': 'write', 'as': 'r'}},
            {'held': {'id': "written '%s'" % value, 'op': 'single', 'as': 'w', 'sheet': 'main',
                      'datum': COLS, 'datum_props': ['as_string', 'blank', 'html_form'],
                      'expect': {'datum': {
                          'string1': {'as_string': value, 'blank': blank, 'html_form': [value]},
                          'integer1': {'as_string': value, 'blank': blank, 'html_form': [value]},
                          'calc1': {'as_string': value, 'blank': blank},
                      }}}},
        ]
        # Check filters
        for c in COLS:
            if c == 'integer1' and value == '':
                continue
            steps.append({'search': {'id': "filter '%s' %s" % (value, c), 'sheet': 'main',
                                     'filter': {'rules': [rule(c, value)]}, 'columns': [c],
                                     'expect': {'count': 1}}})
    write_scenario(os.path.join(OUT, '015_string_zero.yaml'), {
        '_generator': GEN,
        'name': '015_string_zero',
        'source': ['t/015_string_zero.t'],
        'description': 'Zero versus empty string values in string, integer and calc fields, and filtering on them',
        'steps': steps,
    })


if __name__ == '__main__':
    main()
