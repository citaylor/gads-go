package gads

import "sort"

// Instances lists the tables (instances) of the site (GADS::Instances).
type Instances struct {
	e     *Engine
	user  *User
	all   []*Layout
	index map[int64]*Layout
	cache []*Layout
}

// NewInstances creates the list of instances for a user.
func (e *Engine) NewInstances(user *User) *Instances {
	return &Instances{e: e, user: user}
}

// layouts returns a layout of every instance of the site, sorted by name.
func (is *Instances) layouts() []*Layout {
	if is.cache != nil {
		return is.cache
	}
	out := []*Layout{}
	for _, id := range is.e.DB.QueryInt64s(`SELECT "me"."id" FROM "instance" "me"` + is.e.siteWhere("me")) {
		out = append(out, is.e.NewLayout(id, is.user))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	is.cache = out
	return out
}

// All returns the layouts of the instances the user can access.
func (is *Instances) All() []*Layout {
	if is.all == nil {
		is.all = []*Layout{}
		for _, l := range is.layouts() {
			if l.UserCanAnything() {
				is.all = append(is.all, l)
			}
		}
	}
	return is.all
}

// Layout returns the layout of an instance, or nil if it does not exist.
func (is *Instances) Layout(instanceID int64) *Layout {
	if is.index == nil {
		is.index = map[int64]*Layout{}
		for _, l := range is.layouts() {
			is.index[l.InstanceID] = l
		}
	}
	return is.index[instanceID]
}

// IsValid returns the instance ID if the user can access the instance, or 0.
func (is *Instances) IsValid(instanceID int64) int64 {
	for _, l := range is.All() {
		if l.InstanceID == instanceID {
			return instanceID
		}
	}
	return 0
}

// LayoutByShortname returns the accessible layout with the identifier, or
// nil (layout_by_shortname with no_errors).
func (is *Instances) LayoutByShortname(name string) *Layout {
	for _, l := range is.All() {
		if l.Identifier() == name {
			return l
		}
	}
	return nil
}
