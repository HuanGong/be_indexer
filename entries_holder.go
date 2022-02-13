package be_indexer

import (
	"fmt"
	"sort"
	"strings"
)

type (
	EntriesHolder interface {
		EnableDebug(debug bool)

		DumpEntries(buffer *strings.Builder)

		// CompileEntries finalize entries status for query, build or make sorted
		// according to the paper, entries must be sorted
		CompileEntries()

		GetEntries(field *fieldDesc, assigns Values) (CursorGroup, error)
		//GetEntries(field *fieldDesc, assigns Values) (FieldCursor, error)

		// AddFieldEID tokenize values and add it to holder container
		AddFieldEID(field *fieldDesc, values Values, eid EntryID) error
	}

	// DefaultEntriesHolder EntriesHolder implement base on hash map holder map<key, Entries>
	DefaultEntriesHolder struct {
		debug     bool
		maxLen    int64 // max length of Entries
		avgLen    int64 // avg length of Entries
		plEntries map[Key]Entries
	}
)

func NewDefaultEntriesHolder() EntriesHolder {
	return &DefaultEntriesHolder{
		plEntries: map[Key]Entries{},
	}
}

func (h *DefaultEntriesHolder) EnableDebug(debug bool) {
	h.debug = debug
}

func (h *DefaultEntriesHolder) DumpEntries(buffer *strings.Builder) {
	for key, entries := range h.plEntries {
		buffer.WriteString(key.String())
		buffer.WriteString(":")
		buffer.WriteString(strings.Join(entries.DocString(), ","))
		buffer.WriteString("\n")
	}
}

func (h *DefaultEntriesHolder) CompileEntries() {
	h.makeEntriesSorted()
}

func (h *DefaultEntriesHolder) GetEntries(field *fieldDesc, assigns Values) (r CursorGroup, e error) {
	var ids []uint64

	for _, vi := range assigns {
		if ids, e = field.Parser.ParseAssign(vi); e != nil {
			return nil, e
		}
		for _, id := range ids {

			key := NewKey(field.ID, id)

			if entries := h.getEntries(key); len(entries) > 0 {

				r = append(r, NewEntriesCursor(newQKey(field.Field, vi), entries))
			}
		}
	}
	return r, nil
}

func (h *DefaultEntriesHolder) AddFieldEID(field *fieldDesc, values Values, eid EntryID) (err error) {
	var ids []uint64
	// NOTE: ids can be replicated if expression contain cross condition
	for _, value := range values {
		if ids, err = field.Parser.ParseValue(value); err != nil {
			return fmt.Errorf("field:%s parser value:%+v fail, err:%s", field.Field, value, err.Error())
		}
		for _, id := range ids {
			h.AppendEntryID(NewKey(field.ID, id), eid)
		}
	}
	return nil
}

func (h *DefaultEntriesHolder) AppendEntryID(key Key, id EntryID) {
	entries, hit := h.plEntries[key]
	if !hit {
		h.plEntries[key] = Entries{id}
	}
	entries = append(entries, id)
	h.plEntries[key] = entries
}

func (h *DefaultEntriesHolder) getEntries(key Key) Entries {
	if entries, hit := h.plEntries[key]; hit {
		return entries
	}
	return nil
}

func (h *DefaultEntriesHolder) makeEntriesSorted() {
	var total int64
	for _, entries := range h.plEntries {
		sort.Sort(entries)
		if h.maxLen < int64(len(entries)) {
			h.maxLen = int64(len(entries))
		}
		total += int64(len(entries))
	}
	if len(h.plEntries) > 0 {
		h.avgLen = total / int64(len(h.plEntries))
	}
}
