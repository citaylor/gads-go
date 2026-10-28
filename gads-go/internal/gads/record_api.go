package gads

// APIIndexLayout is Layout->api_index_layout: the column whose values
// identify records in the API (instance api_index_layout_id)
func (l *Layout) APIIndexLayout() *Column {
	id, ok := l.e.DB.QueryInt64(`SELECT "api_index_layout_id" FROM "instance" WHERE "id" = ? AND "api_index_layout_id" IS NOT NULL`, l.InstanceID)
	if !ok {
		return nil
	}
	return l.Column(id)
}

// AsJSON is Record->as_json: the values of the columns of the view (or all
// readable columns) as strings, by short name
func (r *Record) AsJSON(view *View) string {
	var ids []int64
	if view != nil {
		ids = view.Columns()
	} else {
		for _, c := range r.layout.AllUserRead() {
			ids = append(ids, c.ID)
		}
	}
	var out map[string]any
	for _, id := range ids {
		col := r.layout.Column(id)
		if col == nil || col.NameShort == "" {
			continue
		}
		if out == nil {
			out = map[string]any{}
		}
		out[col.NameShort] = r.GetFieldValue(col).AsString()
	}
	if out == nil {
		return "null"
	}
	return EncodeJSON(out)
}
