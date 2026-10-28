package tt

import (
	"reflect"
	"strings"
)

// Iterator is Template::Iterator (the FOREACH "loop" variable).
type Iterator struct {
	data                    []any
	started                 bool
	size, max, index, count int
	first, last             int
	prev, next              any
}

// AsLister may be implemented by objects that provide as_list.
type AsLister interface {
	TTAsList() *List
}

// NewIterator is Template::Iterator->new($data)
func NewIterator(data any) *Iterator {
	if !True(data) {
		return &Iterator{}
	}
	switch t := data.(type) {
	case *Hash:
		var items []any
		for _, k := range sortedKeys(t) {
			v, _ := t.Get(k)
			items = append(items, HashOf("key", k, "value", v))
		}
		return &Iterator{data: items}
	case *List:
		return &Iterator{data: t.Items}
	case AsLister:
		return &Iterator{data: t.TTAsList().Items}
	case ListLike:
		return &Iterator{data: t.TTList().Items}
	}
	return &Iterator{data: []any{data}}
}

func (it *Iterator) getFirst() (any, bool) {
	it.started = true
	it.size = len(it.data)
	if it.size == 0 {
		return nil, true
	}
	it.max = it.size - 1
	it.index = 0
	it.count = 1
	it.first = 1
	if it.size > 1 {
		it.last = 0
	} else {
		it.last = 1
	}
	it.prev = nil
	if len(it.data) > 1 {
		it.next = it.data[1]
	} else {
		it.next = nil
	}
	return it.data[0], false
}

func (it *Iterator) getNext() (any, bool) {
	if !it.started || it.index >= it.max {
		return nil, true
	}
	it.index++
	it.count = it.index + 1
	it.first = 0
	if it.index == it.max {
		it.last = 1
	} else {
		it.last = 0
	}
	it.prev = it.data[it.index-1]
	if it.index+1 < len(it.data) {
		it.next = it.data[it.index+1]
	} else {
		it.next = nil
	}
	return it.data[it.index], false
}

func (it *Iterator) method(name string) ([]any, error) {
	if !it.started {
		// values before get_first are undefined
		switch name {
		case "size", "max", "index", "count", "number", "first", "last", "prev", "next":
			return nil, nil
		}
	}
	switch name {
	case "size":
		return []any{int64(it.size)}, nil
	case "max":
		return []any{int64(it.max)}, nil
	case "index":
		return []any{int64(it.index)}, nil
	case "count", "number":
		return []any{int64(it.count)}, nil
	case "first":
		return []any{int64(it.first)}, nil
	case "last":
		return []any{int64(it.last)}, nil
	case "prev":
		return []any{it.prev}, nil
	case "next":
		return []any{it.next}, nil
	case "odd":
		if it.count%2 != 0 {
			return []any{int64(1)}, nil
		}
		return []any{int64(0)}, nil
	case "even":
		if it.count%2 != 0 {
			return []any{int64(0)}, nil
		}
		return []any{int64(1)}, nil
	case "parity":
		if it.count%2 != 0 {
			return []any{"odd"}, nil
		}
		return []any{"even"}, nil
	}
	if strings.Contains(strings.ToUpper(name), "NUMBER") {
		return []any{int64(it.count)}, nil
	}
	return nil, nil
}

func reflectPointer(p any) uintptr {
	v := reflect.ValueOf(p)
	switch v.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Map, reflect.Slice, reflect.UnsafePointer, reflect.Chan:
		return v.Pointer()
	}
	return 0
}
