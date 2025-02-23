package be_indexer

import (
	"fmt"

	"github.com/echoface/be_indexer/codegen/cache"
	"github.com/echoface/be_indexer/parser"
	"github.com/echoface/be_indexer/util"
	"google.golang.org/protobuf/proto"
)

type (
	IndexerBuilder struct {
		BuilderOption

		indexer BEIndex

		fieldsData map[BEField]*FieldDesc

		idAllocator parser.IDAllocator
	}

	// CacheProvider a interface
	CacheProvider interface {
		// Reset expire all existing cache data
		Reset()

		Get(conjID ConjID) ([]byte, bool)

		Set(conjID ConjID, data []byte)
	}

	BuilderOption struct {
		indexerType     IndexerType
		builderCache    CacheProvider
		badConjBehavior BadConjBehavior // 是否允许一个doc中部分Conjunction解析失败
	}

	BuilderOpt func(builder *IndexerBuilder)

	IndexerType     int
	BadConjBehavior int
)

const (
	ErrorBadConj = 0
	SkipBadConj  = 1
	PanicBadConj = 2

	IndexerTypeDefault = IndexerType(0)
	IndexerTypeCompact = IndexerType(1)
)

func WithBadConjBehavior(v BadConjBehavior) BuilderOpt {
	return func(builder *IndexerBuilder) {
		builder.badConjBehavior = v
	}
}

func WithCacheProvider(provider CacheProvider) BuilderOpt {
	return func(builder *IndexerBuilder) {
		builder.builderCache = provider
	}
}

func WithIndexerType(t IndexerType) BuilderOpt {
	return func(builder *IndexerBuilder) {
		builder.indexerType = t
	}
}

func NewIndexerBuilder(opts ...BuilderOpt) *IndexerBuilder {
	builder := &IndexerBuilder{
		indexer:     NewKGroupsBEIndex(),
		fieldsData:  map[BEField]*FieldDesc{},
		idAllocator: parser.NewIDAllocatorImpl(),
	}
	for _, optFn := range opts {
		optFn(builder)
	}
	builder.initIndexer()
	return builder
}

func NewCompactIndexerBuilder(opts ...BuilderOpt) *IndexerBuilder {
	opts = append(opts, WithIndexerType(IndexerTypeCompact))
	return NewIndexerBuilder(opts...)
}

func (b *IndexerBuilder) Reset() {
	b.initIndexer()
	if b.builderCache != nil {
		b.builderCache.Reset()
	}
}

func (b *IndexerBuilder) initIndexer() {
	switch b.indexerType {
	case IndexerTypeDefault:
		b.indexer = NewKGroupsBEIndex()
	case IndexerTypeCompact:
		b.indexer = NewCompactedBEIndex()
	default:
		util.PanicIf(true, "type:%d not supported", b.indexerType)
	}
}

func (b *IndexerBuilder) ConfigField(field BEField, settings FieldOption) {
	_, err := b.configureField(field, settings)
	util.PanicIfErr(err, "config field:%s with option fail:%+v", field, settings)
}

func (b *IndexerBuilder) AddDocument(docs ...*Document) error {
	for _, doc := range docs {
		util.PanicIf(doc == nil, "nil document not be allowed")
		if err := b.validDocument(doc); err != nil {
			return err
		}
		if err := b.buildDocEntries(doc); err != nil {
			return err
		}
	}
	return nil
}

func (b *IndexerBuilder) BuildIndex() BEIndex {

	b.indexer.setFieldDesc(b.fieldsData)

	err := b.indexer.compileIndexer()
	util.PanicIfErr(err, "fail compile indexer data, err:%+v", err)

	return b.indexer
}

func (b *IndexerBuilder) configureField(field BEField, option FieldOption) (*FieldDesc, error) {
	if _, ok := b.fieldsData[field]; ok {
		return nil, fmt.Errorf("can't configure field:%s twice", field)
	}
	if len(option.Container) == 0 {
		option.Container = HolderNameDefault
		Logger.Infof("not configure container for field:%s, use default", field)
	}

	fieldID := uint64(len(b.fieldsData))
	desc := &FieldDesc{
		ID:          fieldID,
		Field:       field,
		FieldOption: option,
	}
	b.fieldsData[field] = desc
	Logger.Infof("configure field:%s, fieldID:%d\n", field, desc.ID)
	return desc, nil
}

func (b *IndexerBuilder) validDocument(doc *Document) error {
	// util.PanicIf(len(doc.Cons) == 0, "no conjunctions in this document")
	// util.PanicIf(len(doc.Cons) > 0xFF, "number of conjunction need less than 256")
	if len(doc.Cons) == 0 {
		return fmt.Errorf("no conjunctions in this document")
	}
	if len(doc.Cons) > 0xFF {
		return fmt.Errorf("number of conjunction need less than 256")
	}
	return nil
}

func (b *IndexerBuilder) createFieldData(field BEField) *FieldDesc {
	if desc, hit := b.fieldsData[field]; hit {
		return desc
	}
	desc, err := b.configureField(field, FieldOption{
		Container: HolderNameDefault,
	})
	util.PanicIfErr(err, "this should not happened for default settings")
	return desc
}

func (b *IndexerBuilder) buildDocEntries(doc *Document) error {
	util.PanicIf(len(doc.Cons) == 0, "no conjunctions in this document")
	util.PanicIf(len(doc.Cons) > 0xFF, "number of conjunction need less than 256")

ConjLoop:
	for idx, conj := range doc.Cons {

		incSize := conj.CalcConjSize()
		conjID := NewConjID(doc.ID, idx, incSize)

		if incSize == 0 {
			b.indexer.addWildcardEID(NewEntryID(conjID, true))
		}

		var conjIndexingTxs []*IndexingBETx
		if conjIndexingTxs = b.tryUseIndexingTxCache(conjID); conjIndexingTxs == nil {

			var err error
			var needCache bool

			if conjIndexingTxs, needCache, err = b.indexingConjunction(conj, conjID); err != nil {
				switch b.badConjBehavior {
				case SkipBadConj:
					Logger.Errorf("indexing conj:%s fail:%v", conjID.String(), err)
					continue ConjLoop
				case ErrorBadConj:
					return fmt.Errorf("indexing conj:%s fail:%v", conjID.String(), err)
				default:
				}
				util.PanicIf(true, "indexing conj:%s fail:%v", conjID.String(), err)
			}

			if needCache {
				b.tryCacheIndexingTx(conjID, conjIndexingTxs)
			}
		}

		for _, tx := range conjIndexingTxs {
			err := tx.holder.CommitIndexingBETx(*tx)
			util.PanicIfErr(err, "commit indexing data failed")
		}
	}
	return nil
}

// indexingConjunction return (txs []*IndexingBETx, needCache bool, err error)
func (b *IndexerBuilder) indexingConjunction(conj *Conjunction, conjID ConjID) ([]*IndexingBETx, bool, error) {
	incSize := conjID.Size()
	conjIndexingTXs := make([]*IndexingBETx, 0, len(conj.Expressions))

	container := b.indexer.newContainer(incSize)
	var needCacheCnt int

	for field, exprs := range conj.Expressions {
		for _, expr := range exprs {
			desc := b.createFieldData(field)
			holder := container.CreateHolder(desc)

			var err error
			var txData TxData
			if txData, err = holder.IndexingBETx(desc, expr); err != nil {
				return nil, false, fmt.Errorf("indexing field:%s fail:%v", field, err)
			}
			entryID := NewEntryID(conjID, expr.Incl)
			if txData.BetterToCache() {
				needCacheCnt++
			}
			tx := &IndexingBETx{field: desc, holder: holder, EID: entryID, Data: txData}
			conjIndexingTXs = append(conjIndexingTXs, tx)
		}
	}
	return conjIndexingTXs, needCacheCnt > 0, nil
}

func (b *IndexerBuilder) tryUseIndexingTxCache(conjID ConjID) (cachedIndexingTx []*IndexingBETx) {
	if b.builderCache == nil {
		return nil
	}

	txCache := &cache.IndexingTxCache{}
	if data, ok := b.builderCache.Get(conjID); !ok {
		return nil
	} else if err := proto.Unmarshal(data, txCache); err != nil {
		return nil
	}

	container := b.indexer.newContainer(conjID.Size())

	for field, fieldData := range txCache.FieldData {
		desc := b.createFieldData(BEField(field))
		holder := container.CreateHolder(desc)

		var err error
		var txData TxData
		if txData, err = holder.DecodeTxData(fieldData.Data); err != nil {
			Logger.Errorf("doc:%d field:%s indexing field:%s fail:%v", conjID.DocID(), field, err)
			return nil
		}

		eid := EntryID(fieldData.Eid)
		tx := &IndexingBETx{field: desc, holder: holder, EID: eid, Data: txData}
		cachedIndexingTx = append(cachedIndexingTx, tx)
	}
	return cachedIndexingTx
}

func (b *IndexerBuilder) tryCacheIndexingTx(conjID ConjID, txs []*IndexingBETx) {
	if b.builderCache == nil {
		return
	}
	txCache := &cache.IndexingTxCache{
		ConjunctionId: uint64(conjID),
		FieldData:     map[string]*cache.FieldCache{},
	}
	var err error
	for _, tx := range txs {
		var content []byte
		if content, err = tx.Data.Encode(); err != nil {
			LogErr("field:%s TxData encode fail:%v", tx.field, err)
			return
		}

		txCache.FieldData[string(tx.field.Field)] = &cache.FieldCache{
			Eid:  uint64(tx.EID),
			Data: content,
		}
	}
	if data, err := proto.Marshal(txCache); err == nil {
		b.builderCache.Set(conjID, data)
	}
}
