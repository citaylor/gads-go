#!/usr/bin/env python3
"""Template Toolkit language conformance scenarios.

GADS renders its HTML with Template Toolkit (views/*.tt). The Go port
renders the same templates with its own TT implementation (internal/tt), so
these scenarios pin down the TT language behaviour that the GADS views rely
on: directives, chomping, expressions, interpolation, virtual methods and
filters. Each case renders a template with the Dancer2 engine options
(ANYCASE, ABSOLUTE, ENCODING UTF-8).
"""
import os
import yaml

HERE = os.path.dirname(os.path.abspath(__file__))

VARS = {
    'name': 'World',
    'zero': 0,
    'szero': '0',
    'empty': '',
    'num': 42,
    'neg': -3,
    'float': 3.5,
    'html': '<a href="x">Tom & "Jerry"</a>',
    'list': ['b', 'a', 'c'],
    'nums': [10, 2, 33, 4],
    'empty_list': [],
    'hash': {'one': 1, 'two': 2, 'three': 3},
    'nested': {'a': {'b': {'c': 'deep'}}, 'l': [{'x': 1}, {'x': 2}]},
    'people': [
        {'name': 'Zoe', 'age': 30},
        {'name': 'adam', 'age': 25},
        {'name': 'Bob', 'age': 40},
    ],
    'text': 'The quick brown fox',
    'multiline': "line1\nline2\n\nline4",
    'utf': 'café ☃',
}

CASES = [
    # Plain text and GET
    ('text', 'Hello', {}),
    ('get_var', 'Hello [% name %]!', {}),
    ('get_explicit', '[% GET name %]', {}),
    ('get_lowercase', '[% get name %]', {}),
    ('undef_var', '[[% missing %]]', {}),
    ('undef_deep', '[[% missing.foo.bar %]]', {}),
    ('number', '[% num %] [% neg %] [% float %]', {}),
    ('zero', '[% zero %][% szero %]', {}),
    ('utf8', '[% utf %] ü', {}),
    ('hash_dot', '[% hash.one %]-[% hash.two %]', {}),
    ('nested_dot', '[% nested.a.b.c %] [% nested.l.1.x %]', {}),
    ('list_index', '[% list.0 %][% list.2 %][% list.5 %]', {}),
    ('dynamic_key', '[% k = "two"; hash.$k %] [% hash.${k} %]', {}),
    ('comment', 'a[%# a comment %]b', {}),
    ('comment_multi', 'a[% # comment\n name %]b', {}),

    # Chomping
    ('chomp_pre', 'a\n  [%- name %]', {}),
    ('chomp_post', '[% name -%]\n  b', {}),
    ('chomp_both', 'a\n[%- name -%]\nb', {}),
    ('chomp_none', 'a\n[% name %]\nb', {}),
    ('chomp_tilde', 'a\n\n[%~ name ~%]\n\nb', {}),
    ('chomp_plus', 'a\n[%+ name +%]\nb', {}),
    ('chomp_post_spaces', '[% name -%]   \nb', {}),
    ('chomp_post_nonl', '[% name -%]   b', {}),
    ('chomp_pre_multi', 'a\n\n   [%- name %]', {}),
    ('chomp_if_block', '<ul>\n  [%- IF 1 %]\n  <li>x</li>\n  [%- END %]\n</ul>', {}),

    # SET and assignment
    ('set', '[% SET x = 5 %][% x %]', {}),
    ('set_implicit', '[% x = "hi" %][% x %]', {}),
    ('set_multi', '[% a = 1 b = 2 %][% a %][% b %]', {}),
    ('set_semicolon', '[% a = 1; b = a + 1; b %]', {}),
    ('set_dotted', '[% foo.bar = 3; foo.bar %]', {}),
    ('set_hash_key', '[% h = {}; h.x = 1; h.y = "2"; h.x _ h.y %]', {}),
    ('default', '[% DEFAULT x = "dflt" %][% x %] [% name = "keep"; DEFAULT name = "no"; name %]', {}),
    ('default_empty', '[% e = ""; DEFAULT e = "set"; e %]', {}),
    ('call', '[% CALL list.push("z") %][% list.join(",") %]', {}),
    ('list_literal', '[% l = [1, 2, 3] %][% l.size %][% l.1 %]', {}),
    ('list_literal_nocomma', '[% l = [1 2 3] %][% l.join("-") %]', {}),
    ('list_range', '[% l = [1..4] %][% l.join(",") %]', {}),
    ('hash_literal', '[% h = { a => 1, "b" = 2, c => "x" } %][% h.a %][% h.b %][% h.c %]', {}),
    ('hash_literal_nested', '[% h = { a => { b => [1, { c => "d" }] } } %][% h.a.b.1.c %]', {}),

    # Strings and interpolation
    ('string_single', "[% 'a $name b' %]", {}),
    ('string_double', '[% "a $name b" %]', {}),
    ('string_double_braces', '[% "a ${name}b ${hash.one}" %]', {}),
    ('string_double_dotted', '[% "x $hash.two y" %]', {}),
    ('string_escapes', r'[% "tab\there\nnl \"q\" \$name" %]', {}),
    ('string_single_escape', r"[% 'it\'s \\ \n' %]", {}),
    ('concat', '[% name _ "!" _ num %]', {}),
    ('concat_assign', '[% s = "a"; s = s _ "b"; s %]', {}),

    # Expressions
    ('arith', '[% 1 + 2 %] [% 7 - 10 %] [% 3 * 4 %] [% 7 / 2 %] [% 7 % 3 %] [% 7 mod 3 %] [% 7 div 2 %]', {}),
    ('arith_prec', '[% 1 + 2 * 3 %] [% (1 + 2) * 3 %]', {}),
    ('arith_float', '[% 10 / 3 %] [% 0.1 + 0.2 %] [% 2.50 %] [% 1.5 * 2 %] [% 22 / 7 * 1000 %]', {}),
    ('arith_exp_parse', '[% 1e3 %]', {}),
    ('arith_strings', '[% "3" + "4" %] [% "abc" + 1 %]', {}),
    ('compare_num', '[% IF 10 > 9 %]y[% END %][% IF 2 >= 2 %]y[% END %][% IF 1 < 0 %]n[% END %][% IF 3 <= 3 %]y[% END %]', {}),
    ('compare_str', '[% IF "abc" == "abc" %]eq[% END %][% IF "a" != "b" %]ne[% END %][% IF "10" == "10.0" %]num[% ELSE %]str[% END %]', {}),
    ('compare_lt_str', '[% IF "10" < "9" %]strcmp[% ELSE %]numcmp[% END %]', {}),
    ('logic', '[% IF 1 && 0 %]a[% END %][% IF 1 || 0 %]b[% END %][% IF !0 %]c[% END %][% IF 1 and 1 %]d[% END %][% IF 0 or 1 %]e[% END %][% IF not 0 %]f[% END %]', {}),
    ('logic_upper', '[% IF 1 AND 1 %]a[% END %][% IF 0 OR 1 %]b[% END %][% IF NOT 0 %]c[% END %]', {}),
    ('or_value', '[% missing || "fallback" %] [% name || "no" %] [% zero || "z" %]', {}),
    ('and_value', '[% name && "yes" %]|[% zero && "yes" %]', {}),
    ('ternary', '[% num > 10 ? "big" : "small" %] [% zero ? "t" : "f" %]', {}),
    ('ternary_nested', '[% x = 5; x < 3 ? "a" : x < 6 ? "b" : "c" %]', {}),
    ('not_prec', '[% IF !missing && name %]ok[% END %]', {}),
    ('truth', '[% FOREACH v IN [0, "0", "", "0.0", "00", " ", "a", 1] %][% v ? "T" : "F" %][% END %]', {}),
    ('truth_list', '[% IF empty_list %]T[% ELSE %]F[% END %][% IF list %]T[% END %][% IF hash %]T[% END %]', {}),

    # Conditionals
    ('if_else', '[% IF zero %]a[% ELSIF num == 42 %]b[% ELSE %]c[% END %]', {}),
    ('if_else_lower', '[% if zero %]a[% elsif num == 1 %]b[% else %]c[% end %]', {}),
    ('unless', '[% UNLESS zero %]u[% END %][% UNLESS num %]n[% ELSE %]e[% END %]', {}),
    ('if_postfix', '[% "yes" IF num %][% "no" IF zero %][% "u" UNLESS zero %]', {}),
    ('if_nested', '[% IF 1 %][% IF 0 %]x[% ELSE %][% IF 1 %]deep[% END %][% END %][% END %]', {}),
    ('switch', '[% SWITCH name %][% CASE "a" %]A[% CASE ["x", "World"] %]W[% CASE %]D[% END %]', {}),
    ('switch_default', '[% SWITCH num %][% CASE 1 %]one[% CASE DEFAULT %]other[% END %]', {}),
    ('switch_lower', '[% switch "b" %][% case "a" %]A[% case "b" %]B[% end %]', {}),

    # Loops
    ('foreach', '[% FOREACH i IN list %][% i %],[% END %]', {}),
    ('foreach_eq', '[% FOREACH i = nums %][% i %] [% END %]', {}),
    ('foreach_lower', '[% foreach i in list %][% i %][% end %]', {}),
    ('for', '[% FOR i IN [1,2] %][% i %][% END %]', {}),
    ('foreach_noloopvar', '[% FOREACH people %][% name %];[% END %][% name %]', {}),
    ('foreach_loop', '[% FOREACH i IN list %][% loop.index %]/[% loop.count %]/[% loop.size %]/[% loop.max %]/[% loop.first %]/[% loop.last %]/[% loop.prev %]/[% loop.next %] [% END %]', {}),
    ('foreach_loop_parity', '[% FOREACH i IN list %][% loop.parity %][% loop.odd %][% loop.even %] [% END %]', {}),
    ('foreach_hash', '[% FOREACH p IN hash %][% p.key %]=[% p.value %];[% END %]', {}),
    ('foreach_hash_pairs', '[% FOREACH p IN hash.pairs %][% p.key %]=[% p.value %];[% END %]', {}),
    ('foreach_hash_keys', '[% FOREACH k IN hash.keys.sort %][% k %][% END %]', {}),
    ('foreach_scalar', '[% FOREACH i IN name %][% i %][% END %]', {}),
    ('foreach_undef', '[% FOREACH i IN missing %][% i %][% END %]x', {}),
    ('foreach_postfix', '[% i _ "," FOREACH i IN list %]', {}),
    ('foreach_next_last', '[% FOREACH i IN [1..6] %][% NEXT IF i == 2 %][% LAST IF i == 5 %][% i %][% END %]', {}),
    ('foreach_nested_loop', '[% FOREACH a IN [1,2] %][% FOREACH b IN ["x","y"] %][% loop.index %][% a %][% b %] [% END %][% loop.index %]|[% END %]', {}),
    ('foreach_var_restored', '[% i = "orig" %][% FOREACH i IN [1,2] %][% END %][% i %]', {}),
    ('foreach_assign_inside', '[% total = 0 %][% FOREACH n IN nums %][% total = total + n %][% END %][% total %]', {}),
    ('while', '[% i = 0 %][% WHILE i < 3 %][% i %][% i = i + 1 %][% END %]', {}),

    # Blocks, INCLUDE, PROCESS, WRAPPER, MACRO
    ('block_process', '[% BLOCK b %]in-b [% x %][% END %][% x = 1 %][% PROCESS b %]', {}),
    ('block_include_args', '[% BLOCK b %][% x %]-[% y %][% END %][% INCLUDE b x = "X" y = name %]', {}),
    ('include_localises', '[% BLOCK b %][% x = "changed" %][% END %][% x = "orig" %][% INCLUDE b %][% x %]', {}),
    ('process_shares', '[% BLOCK b %][% x = "changed" %][% END %][% x = "orig" %][% PROCESS b %][% x %]', {}),
    ('include_file', '[% INCLUDE inc.tt v = 3 %]', {'inc.tt': 'file:[% v %]:[% name %]'}),
    ('include_file_subdir', '[% INCLUDE sub/inc.tt %]', {'sub/inc.tt': 'sub [% name %]'}),
    ('include_file_quoted', '[% INCLUDE "inc.tt" %]', {'inc.tt': 'q'}),
    ('include_dynamic', '[% f = "inc.tt" %][% INCLUDE $f %]', {'inc.tt': 'dyn'}),
    ('include_interp', '[% f = "inc" %][% INCLUDE "$f.tt" %]', {'inc.tt': 'interp'}),
    ('include_file_trailing_nl', '[[% INCLUDE inc.tt %]]', {'inc.tt': 'with nl\n'}),
    ('include_hash_arg_deep', '[% INCLUDE inc.tt h.a = 1 %]', {'inc.tt': '[% h.a %]'}),
    ('process_file_vars', '[% PROCESS inc.tt %][% set_in_inc %]', {'inc.tt': '[% set_in_inc = "leaked" %]'}),
    ('include_block_in_file', '[% PROCESS lib.tt %][% INCLUDE fromlib %]', {'lib.tt': '[% BLOCK fromlib %]lib-block[% END %]'}),
    ('include_missing', '[% INCLUDE nonexist.tt %]', {}),
    ('wrapper', '[% WRAPPER w %]inner[% END %]', {}),
    ('wrapper_block', '[% BLOCK w %]<[% content %]>[% END %][% WRAPPER w %]inner [% name %][% END %]', {}),
    ('wrapper_file_args', '[% WRAPPER wrap.tt title = "T" %]body[% END %]', {'wrap.tt': '<h1>[% title %]</h1>[% content %]'}),
    ('macro', '[% MACRO m(a, b) BLOCK %][% a %]+[% b %][% END %][% m(1, 2) %] [% m("x") %]', {}),
    ('macro_directive', '[% MACRO up(s) GET s.upper %][% up("abc") %]', {}),
    ('macro_named_args', '[% MACRO m(a) BLOCK %][% a %][% b %][% END %][% m(1, b = 2) %]', {}),
    ('macro_noargs', '[% MACRO hello BLOCK %]hello [% name %][% END %][% hello %]', {}),
    ('macro_localises', '[% MACRO m BLOCK %][% x = 2 %][% END %][% x = 1 %][% m %][% x %]', {}),
    ('return', '[% BLOCK b %]a[% RETURN %]b[% END %][% INCLUDE b %]c', {}),
    ('stop', 'a[% STOP %]b', {}),
    ('insert', '[% INSERT inc.tt %]', {'inc.tt': 'raw [% name %]'}),
    ('include_list_arg', '[% INCLUDE inc.tt items = [1, 2] %]', {'inc.tt': '[% items.size %]'}),
    ('include_multiline_args', '[% INCLUDE inc.tt\n    a = 1\n    b = "two"\n%]', {'inc.tt': '[% a %][% b %]'}),

    # Filters
    ('filter_html', '[% html | html %]', {}),
    ('filter_html_entity', '[% utf | html_entity %] [% html | html_entity %]', {}),
    ('filter_uri', '[% "a b&c/d?e=fé" | uri %]', {}),
    ('filter_url', '[% "a b&c/d?e=f" | url %]', {}),
    ('filter_upper_lower', '[% name | upper %] [% name | lower %] [% "abc" | ucfirst %] [% "ABC" | lcfirst %]', {}),
    ('filter_trim_collapse', '[[% "  a  b  " | trim %]] [[% "  a   b  " | collapse %]]', {}),
    ('filter_replace', '[% "a.b.c" | replace("\\.", "-") %] [% "foo" | replace("o", "0") %]', {}),
    ('filter_html_para', '[% multiline | html_para %]', {}),
    ('filter_html_break', '[% multiline | html_break %]', {}),
    ('filter_html_line_break', '[% multiline | html_line_break %]', {}),
    ('filter_chain', '[% "<b>" | upper | html %]', {}),
    ('filter_block', '[% FILTER html %]<p>[% name %]</p>[% END %]', {}),
    ('filter_block_upper', '[% FILTER upper %]abc[% END %]', {}),
    ('filter_postfix', '[% name FILTER upper %]', {}),
    ('filter_null', '[% "x" | null %]y', {}),
    ('filter_format', '[% 3.14159 | format("%.2f") %] [% 5 | format("%03d") %]', {}),
    ('filter_truncate', '[% text | truncate(10) %] [% text | truncate(10, "..") %]', {}),
    ('filter_repeat', '[% "ab" | repeat(3) %]', {}),
    ('filter_remove', '[% "a1b2c3" | remove("[0-9]") %]', {}),
    ('filter_indent', '[% multiline | indent(2) %]', {}),
    ('filter_indent_str', '[% "a\nb" | indent("> ") %]', {}),
    ('filter_squote', '[% "it\'s" | squote %]', {}),
    ('filter_xml', '[% html | xml %]', {}),
    ('filter_undef', '[[% missing | html %]]', {}),
    ('filter_unknown', '[% name | nofilter %]', {}),

    # Scalar virtual methods
    ('vm_length', '[% name.length %] [% utf.length %] [% num.length %]', {}),
    ('vm_upper', '[% name.upper %] [% name.lower %] [% s = "abc"; s.ucfirst %] [% s = "ABC"; s.lcfirst %]', {}),
    ('vm_defined', '[% name.defined ? "d" : "u" %][% missing.defined ? "d" : "u" %][% empty.defined ? "d" : "u" %]', {}),
    ('vm_match', '[% IF name.match("^W") %]m[% END %][% d = "2020-10-05"; m = d.match("(\\d+)-(\\d+)") %][% m.0 %]/[% m.1 %][% IF name.match("x") %]n[% END %]', {}),
    ('vm_match_global', '[% d = "a1b22c333"; m = d.match("(\\d+)", 1) %][% m.join(",") %]', {}),
    ('vm_match_nocapture', '[% d = "abc"; m = d.match("b") %][% m.size %]:[% m.0 %]', {}),
    ('vm_search', '[% s = "abc"; s.search("b") ? "y" : "n" %][% s.search("^b") ? "y" : "n" %]', {}),
    ('vm_replace', '[% s = "a-b-c"; s.replace("-", "+") %] [% s = "aaa"; s.replace("a", "$0b") %] [% s = "x1y2"; s.replace("(\\d)", "<$1>") %] [% s = "a.b"; s.replace(".", "_") %]', {}),
    ('vm_split', '[% x = "a,b,,c,,"; s = x.split(",") %][% s.size %]:[% s.join("|") %] [% x = " a b  c"; x.split.join("|") %]', {}),
    ('vm_split_limit', '[% x = "a:b:c:d"; x.split(":", 2).join("|") %]', {}),
    ('vm_substr', '[% text.substr(4, 5) %] [% text.substr(16) %]', {}),
    ('vm_repeat', '[% x = "-"; x.repeat(5) %]', {}),
    ('vm_trim', '[% x = "  x y  " %][[% x.trim %]] [% x = "  a   b " %][[% x.collapse %]]', {}),
    ('vm_chunk', '[% x = "abcdefg"; x.chunk(3).join(",") %] [% x.chunk(-3).join(",") %]', {}),
    ('vm_list_on_scalar', '[% name.list.size %] [% name.list.0 %]', {}),
    ('vm_item', '[% name.item %] [% hash.item("two") %] [% list.item(1) %]', {}),
    ('vm_html', '[% html.html %]', {}),
    ('vm_dquote', '[% x = "a\\"b\\nc"; x.dquote %]', {}),
    ('vm_size_scalar', '[% name.size %]', {}),
    ('vm_hash_on_scalar', '[% name.hash.value %]', {}),

    # List virtual methods
    ('vm_list_size', '[% list.size %] [% list.max %] [% empty_list.size %] [% empty_list.max %]', {}),
    ('vm_list_first_last', '[% list.first %] [% list.last %] [% nums.first(2).join(",") %] [% nums.last(2).join(",") %]', {}),
    ('vm_list_join', '[% list.join %]|[% list.join(", ") %]|[% nums.join("") %]', {}),
    ('vm_list_sort', '[% list.sort.join(",") %] [% nums.sort.join(",") %] [% nums.nsort.join(",") %]', {}),
    ('vm_list_sort_key', '[% FOREACH p IN people.sort("name") %][% p.name %] [% END %]', {}),
    ('vm_list_sort_case', '[% l = ["b", "A", "c", "B"]; l.sort.join(",") %]', {}),
    ('vm_list_nsort_key', '[% FOREACH p IN people.nsort("age") %][% p.age %] [% END %]', {}),
    ('vm_list_reverse', '[% list.reverse.join(",") %]', {}),
    ('vm_list_push_pop', '[% l = [1,2]; l.push(3); l.unshift(0); l.join(",") %] [% l.pop %][% l.shift %] [% l.join(",") %]', {}),
    ('vm_list_push_multi', '[% l = []; l.push(1, 2); CALL l.push("a"); l.join(",") %]', {}),
    ('vm_list_push_output', '[[% l = []; l.push(1) %]]', {}),
    ('vm_list_grep', '[% l = ["apple", "banana", "cherry"]; l.grep("an").join(",") %] [% l = ["a1", "b", "c2"]; l.grep("\\d").size %]', {}),
    ('vm_list_unique', '[% l = [1, 2, 1, 3, 2]; l.unique.join(",") %]', {}),
    ('vm_list_merge', '[% l = [1, 2]; l.merge([3], [4, 5]).join(",") %] [% l.size %]', {}),
    ('vm_list_slice', '[% l = [0..9]; l.slice(2, 4).join(",") %] [% l = [0..5]; l.slice(3).join(",") %]', {}),
    ('vm_list_splice', '[% l = [1..5]; CALL l.splice(1, 2); l.join(",") %]', {}),
    ('vm_list_defined', '[% list.defined(1) ? "y" : "n" %][% list.defined(7) ? "y" : "n" %][% list.defined ? "y" : "n" %]', {}),
    ('vm_list_hash', '[% l = ["a", 1, "b", 2]; h = l.hash; h.b %]', {}),
    ('vm_list_import', '[% l = [1]; CALL l.import([2, 3]); l.join(",") %]', {}),
    ('vm_list_item_neg', '[% list.-1 %] [% list.item(-2) %]', {}),
    ('vm_list_exists', '[% list.exists(1) ? "y" : "n" %]', {}),
    ('vm_list_column', '[% nested.l.0.x %][% nested.l.1.x %]', {}),

    # Hash virtual methods
    ('vm_hash_keys', '[% hash.keys.sort.join(",") %]', {}),
    ('vm_hash_values', '[% hash.values.nsort.join(",") %]', {}),
    ('vm_hash_size', '[% hash.size %] [% e = {}; e.size %]', {}),
    ('vm_hash_exists', '[% hash.exists("one") ? "y" : "n" %][% hash.exists("nine") ? "y" : "n" %]', {}),
    ('vm_hash_defined', '[% hash.defined("one") ? "y" : "n" %][% hash.defined("x") ? "y" : "n" %][% hash.defined ? "y" : "n" %]', {}),
    ('vm_hash_delete', '[% h = { a => 1, b => 2 }; h.delete("a"); h.keys.join(",") %]', {}),
    ('vm_hash_each', '[% h = { a => 1 }; h.each.join(",") %]', {}),
    ('vm_hash_sort', '[% hash.sort.join(",") %] [% hash.nsort.join(",") %]', {}),
    ('vm_hash_pairs', '[% h = { b => 2, a => 1 }; FOREACH p IN h.pairs %][% p.key %][% p.value %][% END %]', {}),
    ('vm_hash_list', '[% h = { a => 1 }; FOREACH p IN h.list %][% p.key %]=[% p.value %][% END %]', {}),
    ('vm_hash_import', '[% h = { a => 1 }; h.import({ b => 2 }); h.keys.sort.join(",") %]', {}),
    ('vm_hash_item', '[% hash.item("three") %]', {}),
    ('vm_hash_dot_size_key', '[% h = { size => "key-wins" }; h.size %]', {}),
    ('hash_stringify', '[% x = hash ? "T" : "F" %][% x %]', {}),

    # Misc syntax used by the GADS views
    ('multi_directive', '[% IF 1; "a"; ELSE; "b"; END %]', {}),
    ('multi_directive_newlines', '[%\n  x = 1;\n  y = 2;\n  IF x < y;\n    "lt";\n  END;\n%]', {}),
    ('block_in_if', '[% IF 1 %][% BLOCK later %]L[% END %][% END %][% INCLUDE later %]', {}),
    ('block_defined_after', '[% INCLUDE after %][% BLOCK after %]after-def[% END %]', {}),
    ('nested_includes', '[% BLOCK o %]o([% INCLUDE i %])[% END %][% BLOCK i %]i[% x %][% END %][% INCLUDE o x = 9 %]', {}),
    ('method_on_literal', '[% "abc".length %] [% [3, 1, 2].sort.0 %]', {}),
    ('negative_number', '[% x = -5; x %] [% 3 - -2 %]', {}),
    ('chained_vmethods', '[% text.split(" ").reverse.join(" ").upper %]', {}),
    ('param_from_var', '[% n = 2; list.$n %]', {}),
    ('list_in_string', '[% "x" _ list.size %]', {}),
    ('equals_undef', '[% IF missing == "" %]eq[% END %][% IF !missing %]false[% END %]', {}),
    ('string_number_eq', '[% IF num == "42" %]y[% END %][% IF "1.0" == "1" %]y[% ELSE %]n[% END %]', {}),
    ('assign_list_element', '[% l = [1,2,3]; l.1 = "x"; l.join(",") %]', {}),
    ('numeric_string_output', '[% x = "007" %][% x %] [% x + 0 %]', {}),
    ('large_numbers', '[% 1000000 * 1000000 %] [% 2 / 3 %] [% 1 / 4 %]', {}),
    ('increment', '[% i = 1; i = i + 1; i %]', {}),
    ('quote_in_html_attr', '<input value="[% html | html %]">', {}),
    ('text_with_percent', '100% done [% "50%" %]', {}),
    ('unclosed_tag', 'a [% name', {}),
    ('parse_error', '[% IF %]', {}),
    ('missing_end', '[% IF 1 %]a', {}),
]


def main():
    steps = []
    for case in CASES:
        cid, template, files = case
        step = {'id': cid, 'template': template, 'vars': VARS, 'allow_error': True}
        if files:
            step['files'] = files
        steps.append({'template': step})
    scenario = {
        'name': '100_tt_language',
        'source': ['Template Toolkit (views/*.tt rendering)'],
        'description': __doc__.strip(),
        'steps': steps,
    }

    class NoAlias(yaml.SafeDumper):
        def ignore_aliases(self, data):
            return True

    with open(os.path.join(HERE, '..', 'scenarios', '100_tt_language.yaml'), 'w') as fh:
        yaml.dump(scenario, fh, Dumper=NoAlias, sort_keys=False, allow_unicode=True, width=1000)


if __name__ == '__main__':
    main()
