package GADS::Conformance::WebSchema;

# Schema "class" handed to Dancer2::Plugin::DBIC: instead of connecting to a
# database it returns the scenario's existing schema, so that the web app and
# the runner share one in-memory database.

use strict;
use warnings;

our $SCHEMA;

sub connect { $SCHEMA or die "No conformance schema set" }

1;
