package db

import (
	"context"
	"fmt"
)

// The upgrades of a database deployed with an earlier schema, as the
// migrations of GADS (share/migrations, run with DBIx::Class::Migration).
// GADS records the schema version in dbix_class_schema_versions; Go
// deploys without that table, so each upgrade is detected by the schema
// itself, and Upgrade can run on every start.

// upgrade is one schema version step
type upgrade struct {
	name string
	// needed reports whether the database is at the earlier version
	needed func(d *DB) (bool, error)
	// statements are run in order, in one transaction with data
	statements map[Dialect][]string
	data       []string
}

var upgrades = []upgrade{
	{
		// share/migrations/{SQLite,PostgreSQL}/upgrade/114-115/001-auto.sql
		// and _common/upgrade/114-115/002-widget-options.pl
		name: "114-115",
		needed: func(d *DB) (bool, error) {
			has, err := d.hasColumn("widget", "options")
			return !has, err
		},
		statements: map[Dialect][]string{
			SQLite: {
				`ALTER TABLE "dashboard" ADD COLUMN "group_id" integer REFERENCES "group"("id") ON DELETE NO ACTION ON UPDATE NO ACTION`,
				`ALTER TABLE "dashboard" ADD COLUMN "type" varchar(16)`,
				`CREATE INDEX "dashboard_idx_group_id" ON "dashboard" ("group_id")`,
				`ALTER TABLE "instance" ADD COLUMN "record_display" varchar(16)`,
				`ALTER TABLE "widget" ADD COLUMN "options" text`,
			},
			Postgres: {
				`ALTER TABLE "dashboard" ADD COLUMN "group_id" integer`,
				`ALTER TABLE "dashboard" ADD COLUMN "type" character varying(16)`,
				`CREATE INDEX "dashboard_idx_group_id" on "dashboard" ("group_id")`,
				`ALTER TABLE "dashboard" ADD CONSTRAINT "dashboard_fk_group_id" FOREIGN KEY ("group_id") REFERENCES "group" ("id") ON DELETE NO ACTION ON UPDATE NO ACTION DEFERRABLE`,
				`ALTER TABLE "instance" ADD COLUMN "record_display" character varying(16)`,
				`ALTER TABLE "widget" ADD COLUMN "options" text`,
			},
		},
		data: []string{
			// The options of timelines and globes move to the options column
			`UPDATE "widget" SET "options" = "tl_options", "tl_options" = NULL WHERE "type" = 'timeline'`,
			`UPDATE "widget" SET "options" = "globe_options", "globe_options" = NULL WHERE "type" = 'globe'`,
			// Only table dashboards at point of upgrade
			`UPDATE "dashboard" SET "type" = 'table'`,
			`UPDATE "instance" SET "record_display" = 'record'`,
		},
	},
}

// Upgrade brings a database deployed with an earlier schema up to date,
// and returns the names of the upgrades it applied. Each upgrade runs in a
// transaction.
func (d *DB) Upgrade() ([]string, error) {
	var applied []string
	for _, u := range upgrades {
		needed, err := u.needed(d)
		if err != nil {
			return applied, fmt.Errorf("upgrade %s: %w", u.name, err)
		}
		if !needed {
			continue
		}
		if err := d.applyUpgrade(u); err != nil {
			return applied, fmt.Errorf("upgrade %s: %w", u.name, err)
		}
		applied = append(applied, u.name)
	}
	return applied, nil
}

func (d *DB) applyUpgrade(u upgrade) error {
	if d.InTxn() {
		return fmt.Errorf("a transaction is open")
	}
	ctx := context.Background()
	if _, err := d.conn.ExecContext(ctx, "BEGIN"); err != nil {
		return err
	}
	for _, q := range append(append([]string{}, u.statements[d.Dialect]...), u.data...) {
		if _, err := d.conn.ExecContext(ctx, q); err != nil {
			d.conn.ExecContext(ctx, "ROLLBACK")
			return fmt.Errorf("%w (%s)", err, q)
		}
	}
	_, err := d.conn.ExecContext(ctx, "COMMIT")
	return err
}

// hasColumn reports whether a table has a column
func (d *DB) hasColumn(table, column string) (bool, error) {
	if d.Dialect == Postgres {
		rows, err := d.TryQuery(`SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`, table, column)
		return len(rows) > 0, err
	}
	rows, err := d.TryQuery(`SELECT 1 FROM pragma_table_info(?) WHERE "name" = ?`, table, column)
	return len(rows) > 0, err
}
