package gads

// HasBrowserCode is Column->has_browser_code: code columns evaluated in the
// browser when editing (shown in the edit page, or a display condition of
// another field)
func (c *Column) HasBrowserCode() bool {
	if !c.IsCode() {
		return false
	}
	if c.ShowInEdit() != 0 {
		return true
	}
	_, ok := c.e.DB.QueryInt64(`SELECT "me"."id" FROM "display_field" "me" WHERE "me"."display_field_id" = ? LIMIT 1`, c.ID)
	return ok
}

// EditColumnsOptions are the options of Record->edit_columns
type EditColumnsOptions struct {
	New, Approval, Child, Modal bool
	Bulk                        string
}

// EditColumns is Record->edit_columns: the columns shown when editing the
// record
func (r *Record) EditColumns(o EditColumnsOptions) []*Column {
	opts := AllOptions{SortByTopics: true, CanChild: o.Child, ExcludeInternal: true}
	switch {
	case o.Approval && o.New:
		opts.UserCanApproveNew = true
	case o.Approval:
		opts.UserCanApproveExisting = true
	case o.New:
		opts.UserCanReadwriteNew = true
	default:
		opts.UserCanReadwriteExisting = true
	}
	cols := r.layout.All(opts)
	filter := func(keep func(*Column) bool) {
		out := cols[:0:0]
		for _, c := range cols {
			if keep(c) {
				out = append(out, c)
			}
		}
		cols = out
	}
	if o.New {
		filter(func(c *Column) bool { return c.Userinput() || c.HasBrowserCode() })
	}
	if o.Modal {
		filter(func(c *Column) bool { return !c.IsCurcommon() || !c.ShowAdd() })
	}
	if o.Bulk == "update" {
		filter(func(c *Column) bool { return c.Type != "file" })
	}
	return cols
}

// Reports is Layout->reports: the reports of the table available to the
// user (by the user's groups)
func (l *Layout) Reports() []map[string]any {
	var out []map[string]any
	user := l.User()
	if user == nil {
		return out
	}
	for _, r := range l.e.DB.Query(`SELECT DISTINCT "me".* FROM "report" "me" LEFT JOIN "report_group" "report_groups" ON "report_groups"."report_id" = "me"."id" WHERE "me"."deleted" IS NULL AND "me"."instance_id" = ? AND "report_groups"."group_id" IN (SELECT "group_id" FROM "user_group" WHERE "user_id" = ?) ORDER BY "me"."id"`, l.InstanceID, user.ID) {
		out = append(out, r)
	}
	return out
}
