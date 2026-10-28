# GADS conformance tests (standalone)

These tests were exported from the GADS Go-rewrite conformance suite by
`export_perl_tests.sh`. Each `NNN_name.t` runs one scenario
(`scenarios/NNN_name.yaml`) against this GADS checkout and makes one subtest
for each step. A subtest checks two things:

- the expectations written in the scenario;
- that the step's observation is identical to the recorded reference run
  (`golden/NNN_name.json`).

The GADS code is not changed. `lib/GADS/Conformance/Patches.pm` patches some
CPAN modules at runtime so that runs are deterministic: it freezes the
clock, makes JSON output canonical and makes random tokens predictable.

## Running

From the GADS root:

    prove -r t/conformance           # all scenarios
    prove -r -j8 t/conformance       # in parallel
    prove t/conformance/000_smoke.t  # one scenario

They need the same modules as the GADS tests, plus `YAML::XS`. If it is
missing, the tests skip.

`GADS_CONFORMANCE_NO_GOLDEN=1` checks only the scenario expectations. Use it
after an intended change in behaviour, when the recorded observations no
longer apply.

The golden observations were recorded with SQLite and particular versions
of the CPAN modules. A few observations include text that comes from those
modules, such as warning messages from Getopt::Long and DBIx::Class. On
other module versions they can differ even though GADS behaves the same.
