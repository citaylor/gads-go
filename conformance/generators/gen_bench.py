#!/usr/bin/env python3
"""Generate the benchmark scenarios (conformance/bench): realistic workloads
run by both runners with GADS_BENCH set, whose step timings bench_all.sh
compares. The data is pseudo-random but deterministic, so that both runners
(and successive runs) do the same work.

    generators/gen_bench.py [--records N]     (default 1000)

The steps repeat with "runs" (the step is run that many times; the timing
covers every run and the observation is that of the last). Each scenario
still has a few expectations, so that a benchmark of a broken engine
fails rather than reporting meaningless times."""
import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from common import write_scenario  # noqa: E402

OUT = os.path.join(os.path.dirname(__file__), '..', 'bench')
GEN = 'generators/gen_bench.py'

WORDS = ['alpha', 'bravo', 'charlie', 'delta', 'echo', 'foxtrot', 'golf',
         'hotel', 'india', 'juliet', 'kilo', 'lima', 'mike', 'november']
ENUMS = ['foo1', 'foo2', 'foo3']
TREES = ['tree1', 'tree2', 'tree3']
CURVALS = 50

CALC = """function evaluate (L1integer1, L1date1, L1string1)
  if L1integer1 == nil then return 0 end
  local total = 0
  for i = 1, 20 do
    total = total + (L1integer1 * i) % 7
  end
  if L1date1 then total = total + L1date1.year end
  if L1string1 and string.find(L1string1, "alpha") then total = total + 1000 end
  return total
end"""


class Rand:
    """A small LCG, so that the data does not depend on Python's random"""

    def __init__(self, seed):
        self.state = seed

    def next(self, n):
        self.state = (self.state * 1103515245 + 12345) % 2 ** 31
        return self.state % n


def date(r, start_year=2000):
    return '%04d-%02d-%02d' % (start_year + r.next(20), 1 + r.next(12), 1 + r.next(28))


def record(r, i):
    y = 2000 + r.next(20)
    return {
        # (unique, with the record number, so that sorting by it has no
        # ties: GADS adds no tie-break to sorts, and PostgreSQL may return
        # tied rows in a different order for the Perl and Go queries)
        'string1': '%s %s %d-%d' % (WORDS[r.next(len(WORDS))], WORDS[r.next(len(WORDS))], r.next(1000), i),
        # (unique too, in a scrambled order over 0-10006, for the sorts)
        'integer1': (i * 7919) % 10007,
        'enum1': ENUMS[r.next(3)],
        'tree1': TREES[r.next(3)],
        'date1': date(r),
        'daterange1': ['%04d-01-%02d' % (y, 1 + r.next(28)), '%04d-06-%02d' % (y + 1, 1 + r.next(28))],
        'person1': 1,
        'curval1': 1 + r.next(CURVALS),
    }


def sheets(n, seed=1):
    r = Rand(seed)
    return [
        {'sheet': {'as': 'cv', 'instance_id': 2, 'site_id': 1,
                   'data': [{'string1': 'curval %d' % i, 'integer1': i} for i in range(1, CURVALS + 1)]}},
        {'sheet': {'id': 'create %d records' % n, 'as': 'main', 'curval': 'cv',
                   'curval_fields': ['string1'], 'calc_code': CALC, 'calc_return_type': 'integer',
                   'data': [record(r, i) for i in range(n)],
                   'expect': {'instance_id': '1'}}},
    ]


def first_id():
    return CURVALS + 1


def records_scenario(n):
    view = ['string1', 'integer1', 'enum1', 'tree1', 'date1', 'daterange1', 'curval1', 'calc1']
    steps = sheets(n)
    steps += [
        {'record': {'id': 'record get', 'op': 'get', 'sheet': 'main', 'current_id': first_id(), 'runs': 50,
                    'expect': {'current_id': str(first_id())}}},
        {'record': {'id': 'record update', 'op': 'update', 'sheet': 'main', 'current_id': first_id() + 1,
                    'runs': 20, 'values': {'integer1': 4242, 'string1': 'updated alpha'}}},
        {'search': {'id': 'count all', 'sheet': 'main', 'no_results': True, 'runs': 20,
                    'expect': {'count': str(n)}}},
        {'search': {'id': 'page of 50 sorted', 'sheet': 'main', 'rows': 50, 'page': 2, 'runs': 20,
                    'view': {'columns': view, 'sorts': [{'name': 'integer1', 'type': 'desc'}]},
                    'observe': view}},
        # (500 rows: retrieving the curval values of more than about 1000
        # records at once fails on SQLite in both implementations, the
        # query being "me.id = ? OR me.id = ? ..." beyond SQLite's maximum
        # expression depth of 1000)
        {'search': {'id': 'page of 500 all columns', 'sheet': 'main', 'retrieve_all': True, 'runs': 3,
                    'rows': 500, 'view': {'columns': view, 'sorts': [{'name': 'string1', 'type': 'asc'}]},
                    'expect': {'count': str(n)}}},
        {'search': {'id': 'filter and', 'sheet': 'main', 'runs': 20,
                    'view': {'columns': view, 'filter': {'condition': 'AND', 'rules': [
                        {'name': 'integer1', 'type': 'integer', 'operator': 'greater', 'value': '5000'},
                        {'name': 'enum1', 'type': 'string', 'operator': 'equal', 'value': 'foo2'},
                    ]}},
                    'observe': ['string1', 'integer1']}},
        {'search': {'id': 'filter or dates', 'sheet': 'main', 'runs': 20,
                    'view': {'columns': view, 'filter': {'condition': 'OR', 'rules': [
                        {'name': 'date1', 'type': 'date', 'operator': 'less', 'value': '2003-01-01'},
                        {'name': 'string1', 'type': 'string', 'operator': 'begins_with', 'value': 'alpha'},
                        {'name': 'curval1', 'type': 'string', 'operator': 'equal', 'value': '7'},
                    ]}}}},
        {'search': {'id': 'quick search', 'sheet': 'main', 'search': 'bravo', 'runs': 10,
                    'observe': ['string1']}},
        # (the values of the groups are not observed: GADS does not order
        # grouped results, so their order on PostgreSQL is undefined)
        {'search': {'id': 'group by enum', 'sheet': 'main', 'runs': 10,
                    'view': {'columns': ['integer1', 'calc1', 'string1'], 'groups': ['enum1']}}},
        {'search': {'id': 'group by curval', 'sheet': 'main', 'runs': 10,
                    'view': {'columns': ['integer1'], 'groups': ['curval1']}}},
        {'graph': {'id': 'bar graph sum', 'sheet': 'main', 'title': 'Sum by enum', 'type': 'bar', 'runs': 10,
                   'x_axis': 'enum1', 'y_axis': 'integer1', 'y_axis_stack': 'sum'}},
        {'graph': {'id': 'line graph by date', 'sheet': 'main', 'title': 'Count by year', 'type': 'line',
                   'runs': 10, 'x_axis': 'date1', 'x_axis_grouping': 'year', 'y_axis': 'string1',
                   'y_axis_stack': 'count', 'group_by': 'enum1'}},
        {'download': {'id': 'csv download', 'sheet': 'main', 'runs': 3,
                      'view': {'name': 'All', 'columns': view, 'sorts': [{'name': 'integer1', 'type': 'asc'}]}}},
        {'cli': {'id': 'update-cached', 'script': 'update-cached.pl', 'runs': 2}},
    ]
    return {'_generator': GEN, 'name': 'b01_records',
            'description': 'Record creation, retrieval, update, searches, sorts, filters, grouping, '
                           'graphs, CSV download and calc recalculation on %d records' % n,
            'steps': steps}


def import_scenario(n):
    r = Rand(2)
    lines = ['string1,integer1,enum1,tree1,date1']
    for i in range(n):
        rec = record(r, i)
        lines.append('%s,%d,%s,%s,%s' % (rec['string1'], rec['integer1'], rec['enum1'], rec['tree1'], rec['date1']))
    steps = [
        {'sheet': {'as': 'main', 'data': [], 'calc_code': CALC, 'calc_return_type': 'integer'}},
        {'import': {'id': 'import %d rows' % n, 'sheet': 'main', 'content': '\n'.join(lines) + '\n'}},
        {'search': {'id': 'count imported', 'sheet': 'main', 'no_results': True,
                    'expect': {'count': str(n)}}},
    ]
    return {'_generator': GEN, 'name': 'b02_import',
            'description': 'CSV import of %d rows' % n, 'steps': steps}


def lua_scenario():
    steps = [
        {'sheet': {'as': 'main'}},
        {'lua': {'id': 'lua simple', 'sheet': 'main', 'runs': 500, 'return_type': 'integer',
                 'code': 'function evaluate (L1integer1) return L1integer1 * 2 end',
                 'vars': {'L1integer1': 21}, 'expect': {'return': '42'}}},
        {'lua': {'id': 'lua loop', 'sheet': 'main', 'runs': 100, 'return_type': 'integer',
                 'code': 'function evaluate (L1integer1)\n  local t = 0\n  for i = 1, 10000 do t = t + (i * L1integer1) % 13 end\n  return t\nend',
                 'vars': {'L1integer1': 7}}},
        {'lua': {'id': 'lua strings', 'sheet': 'main', 'runs': 200, 'return_type': 'string',
                 'code': 'function evaluate (L1string1)\n  local parts = {}\n  for w in string.gmatch(L1string1, "%a+") do table.insert(parts, string.upper(w)) end\n  return table.concat(parts, "-")\nend',
                 'vars': {'L1string1': 'the quick brown fox jumps over the lazy dog'}}},
    ]
    return {'_generator': GEN, 'name': 'b03_lua',
            'description': 'Calculated field code evaluation', 'steps': steps}


def web_scenario(n):
    steps = sheets(n, seed=3)
    steps += [
        {'login': {'id': 'login', 'user': 'superadmin'}},
        {'http': {'id': 'data page', 'as': 'superadmin', 'path': '/{sheet:main}/data', 'no_body': True,
                  'runs': 20, 'expect': {'status': 200}}},
        {'http': {'id': 'api records page', 'as': 'superadmin', 'method': 'POST',
                  'path': '/api/{sheet:main}/records', 'no_body': True, 'runs': 20,
                  'form': {'draw': 1, 'start': 100, 'length': 100}, 'expect': {'status': 200}}},
        {'http': {'id': 'api records search', 'as': 'superadmin', 'method': 'POST',
                  'path': '/api/{sheet:main}/records', 'no_body': True, 'runs': 20,
                  'form': {'draw': 1, 'start': 0, 'length': 50, 'search[value]': 'alpha'},
                  'expect': {'status': 200}}},
        {'http': {'id': 'record view page', 'as': 'superadmin', 'path': '/record/%d' % first_id(),
                  'no_body': True, 'runs': 20, 'expect': {'status': 200}}},
        {'http': {'id': 'csv download', 'as': 'superadmin', 'method': 'POST', 'path': '/{sheet:main}/data',
                  'csrf': True, 'form': {'download': 1}, 'no_body': True, 'runs': 3}},
    ]
    return {'_generator': GEN, 'name': 'b04_web',
            'description': 'Web and API requests (login, data table, records API, record pages, '
                           'download) on %d records' % n,
            'steps': steps}


def units_fixture(n):
    """The database of the database units (gads-bench units and
    perl/bench_units.pl): the sheets of b01_records, set up by each runner
    before the units time engine calls on it"""
    view = ['string1', 'integer1', 'enum1', 'tree1', 'date1', 'daterange1', 'curval1', 'calc1']
    views = [
        # db/filter_view: the filter of the "filter and" step of b01_records
        {'view': {'as': 'v_filter', 'sheet': 'main', 'name': 'Filter', 'columns': view,
                  'filter': {'condition': 'AND', 'rules': [
                      {'name': 'integer1', 'type': 'integer', 'operator': 'greater', 'value': '5000'},
                      {'name': 'enum1', 'type': 'string', 'operator': 'equal', 'value': 'foo2'},
                  ]}}},
        # db/group_enum: records grouped by enum, with totals
        {'view': {'as': 'v_group', 'sheet': 'main', 'name': 'Group', 'columns': ['integer1', 'calc1'],
                  'groups': ['enum1']}},
        # db/csv_100: a download of the view's columns
        {'view': {'as': 'v_csv', 'sheet': 'main', 'name': 'All', 'columns': view,
                  'sorts': [{'name': 'integer1', 'type': 'asc'}]}},
    ]
    return {'_generator': GEN, 'name': 'units_db',
            'description': 'Fixture of the database units: %d records and the views of the units' % n,
            'steps': sheets(n) + views}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--records', type=int, default=1000)
    a = ap.parse_args()
    os.makedirs(OUT, exist_ok=True)
    for doc in (records_scenario(a.records), import_scenario(a.records), lua_scenario(),
                web_scenario(a.records)):
        write_scenario(os.path.join(OUT, doc['name'] + '.yaml'), doc)
    # (in a directory of its own, so that it is not run as a scenario)
    os.makedirs(os.path.join(OUT, 'fixtures'), exist_ok=True)
    write_scenario(os.path.join(OUT, 'fixtures', 'units_db.yaml'), units_fixture(a.records))


if __name__ == '__main__':
    main()
