package be_indexer

import (
	"sync"

	"github.com/echoface/be_indexer/parser"
)

const (
	WildcardFieldName = BEField("_Z_")
)

var (
	wildcardQKey = newQKey(WildcardFieldName, 0)
)

type (
	FieldDesc struct {
		ID     uint64
		Field  BEField
		option FieldOption

		Parser parser.FieldValueParser
	}

	FieldOption struct {
		Parser    string
		Container string // specify Entries holder for all tokenized value Entries
	}

	IndexerSettings struct {
		FieldConfig map[BEField]FieldOption
	}

	BEIndex interface {
		// addWildcardEID interface used by builder
		addWildcardEID(id EntryID)

		// set fields desc/settings
		setFieldDesc(fieldsData map[BEField]*FieldDesc)

		// newContainer indexer need return a valid Container for k size
		newContainer(k int) *EntriesContainer

		// compileIndexer prepare indexer and optimize index data
		compileIndexer()

		// Retrieve scan index data and retrieve satisfied document
		Retrieve(queries Assignments, opt ...IndexOpt) (DocIDList, error)

		// RetrieveWithCollector scan index data and retrieve satisfied document
		RetrieveWithCollector(Assignments, ResultCollector, ...IndexOpt) error

		// DumpEntries debug api
		DumpEntries() string

		DumpEntriesSummary() string
	}

	indexBase struct {
		// fieldsData a field settings and resource, if not configured, it will use default parser and container
		// for expression values;
		fieldsData map[BEField]*FieldDesc

		// wildcardEntries hold all entry id that conjunction size is zero;
		wildcardEntries Entries
	}
)

func (bi *indexBase) setFieldDesc(fieldsData map[BEField]*FieldDesc) {
	bi.fieldsData = fieldsData
}

// addWildcardEID append wildcard entry id to Z set
func (bi *indexBase) addWildcardEID(id EntryID) {
	bi.wildcardEntries = append(bi.wildcardEntries, id)
}

// collectorPool default collect pool
var collectorPool = sync.Pool{
	New: func() interface{} {
		return NewDocIDCollector()
	},
}

func PickCollector() *DocIDCollector {
	return collectorPool.Get().(*DocIDCollector)
}

func PutCollector(c *DocIDCollector) {
	if c == nil {
		return
	}
	c.Reset()
	collectorPool.Put(c)
}

type (
	retrieveContext struct {
		dumpStepInfo bool

		dumpEntriesDetail bool

		collector ResultCollector

		assigns Assignments
	}

	IndexOpt func(ctx *retrieveContext)
)

func WithStepDetail() IndexOpt {
	return func(ctx *retrieveContext) {
		ctx.dumpStepInfo = true
	}
}

func WithDumpEntries() IndexOpt {
	return func(ctx *retrieveContext) {
		ctx.dumpEntriesDetail = true
	}
}

// WithCollector specify a user defined collector
func WithCollector(fn ResultCollector) IndexOpt {
	return func(ctx *retrieveContext) {
		ctx.collector = fn
	}
}

func newRetrieveCtx(ass Assignments, opts ...IndexOpt) retrieveContext {
	ctx := retrieveContext{}
	ctx.assigns = ass
	for _, fn := range opts {
		fn(&ctx)
	}
	return ctx
}
