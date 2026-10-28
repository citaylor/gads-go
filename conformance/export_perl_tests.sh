#!/bin/bash
# Export the conformance scenarios as standalone Perl tests for a GADS
# checkout:
#   ./export_perl_tests.sh DEST      (DEST is the GADS root, or any dir)
# writes DEST/t/conformance: one .t file per scenario, the runner modules,
# the scenarios and their recorded (golden) observations. Run them from the
# GADS root with: prove -r t/conformance   (add -j8 to run in parallel)
set -e
HERE="$(cd "$(dirname "$0")" && pwd)"
DEST="${1:?Usage: $0 DEST}"
OUT="$DEST/t/conformance"
rm -rf "$OUT"
mkdir -p "$OUT/lib/GADS/Conformance" "$OUT/scenarios" "$OUT/golden" "$OUT/webconf"
cp "$HERE"/perl/lib/GADS/Conformance/{Patches,Runner,Test,Web,WebSchema}.pm "$OUT/lib/GADS/Conformance/"
cp "$HERE"/perl/webconf/* "$OUT/webconf/"
for f in "$HERE"/scenarios/*.yaml; do
    name="$(basename "$f" .yaml)"
    [ -f "$HERE/results/perl/$name.json" ] || { echo "No recorded result for $name" >&2; exit 1; }
    cp "$f" "$OUT/scenarios/"
    cp "$HERE/results/perl/$name.json" "$OUT/golden/"
    cat > "$OUT/$name.t" <<T
use FindBin;
use lib "\$FindBin::Bin/lib";
use GADS::Conformance::Test;
GADS::Conformance::Test::run('$name');
T
done
cp "$HERE/STANDALONE.md" "$OUT/README.md"
echo "Exported $(ls "$HERE"/scenarios/*.yaml | wc -l) tests to $OUT"
