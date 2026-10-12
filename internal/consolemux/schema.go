package consolemux

import _ "embed"

// PGSchemaSQL is the domain DDL mirrored by migration 0300.
//
//go:embed pgschema.sql
var PGSchemaSQL string
