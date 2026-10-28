package gads

// ForTable is Datum->for_table: the value of a datum for a row of the data
// table (records API)
func ForTable(d Datum) map[string]any {
	col := d.Column()
	ret := map[string]any{
		"type":      col.Type,
		"column_id": col.ID,
		"name":      col.Name,
	}
	strs := func(s []string) []any {
		out := make([]any, 0, len(s))
		for _, v := range s {
			out = append(out, v)
		}
		return out
	}
	switch x := d.(type) {
	case *TreeDatum:
		// As Perl: text_all (an array) in an array
		ret["values"] = []any{strs(x.TextAll())}
	case *CurcommonDatum:
		ret["parent_layout_identifier"] = col.LayoutParent().Identifier()
		ret["curval_record_id"] = nullIfZero(x.base().record.RecordID())
		ret["limit_rows"] = nullIfZero(col.LimitRows())
		values := []any{}
		for _, v := range x.Values() {
			fields := []any{}
			for _, f := range col.CurvalFields() {
				fields = append(fields, ForTable(v.Record.GetFieldValue(f)))
			}
			values = append(values, map[string]any{
				"record_id":  v.ID,
				"version_id": nil,
				"fields":     fields,
			})
		}
		ret["values"] = values
	case *CountDatum:
		ret["type"] = "count"
		ret["values"] = []any{x.AsString()}
	case *IntegerDatum:
		ret["values"] = append([]any{}, x.Values()...)
	case *RagDatum:
		ret["values"] = append([]any{}, x.Values()...)
	case *PersonDatum:
		vals := []any{}
		if !x.Blank() {
			site := col.Layout().e.Site()
			for _, v := range x.ValueHash() {
				details := []any{}
				if email := v["email"]; truthy(email) {
					details = append(details, map[string]any{"value": email, "type": "email"})
				}
				for _, ft := range [][2]any{
					{v["freetext1"], site.str("register_freetext1_name")},
					{v["freetext2"], site.str("register_freetext2_name")},
				} {
					if !truthy(ft[0]) {
						continue
					}
					details = append(details, map[string]any{"definition": emptyNilStr(ft[1].(string)), "value": ft[0], "type": "text"})
				}
				vals = append(vals, map[string]any{"text": v["value"], "details": details})
			}
		}
		ret["values"] = vals
	case *IDDatum:
		if s := x.AsString(); s != "" && s != "0" {
			ret["values"] = []any{s}
		} else {
			ret["values"] = []any{}
		}
		ret["parent_id"] = nullIfZero(x.base().record.ParentID())
	case *FileDatum:
		files := []any{}
		for _, f := range x.Files() {
			files = append(files, f)
		}
		ret["values"] = files
	case *CalcDatum:
		ret["values"] = strs(x.AsStrings())
	case *SerialDatum:
		ret["values"] = []any{x.AsString()}
	default:
		// Date, daterange, enum and string: text_all
		ret["values"] = strs(d.TextAll())
	}
	return ret
}

func nullIfZero(i int64) any {
	if i == 0 {
		return nil
	}
	return i
}

func emptyNilStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
