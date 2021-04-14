package be_indexer

import (
	"encoding/json"
	"fmt"
	"github.com/HuanGong/be_indexer/util"
	"io/ioutil"
	"math/rand"
	"testing"
)

func buildTestDoc() []*Document {

	docs := make([]*Document, 0)
	content, e := ioutil.ReadFile("./test_data/test_docs.json")
	if e != nil {
		panic(e)
	}
	if e = json.Unmarshal(content, &docs); e != nil {
		panic(e)
	}
	fmt.Println("total docs:", len(docs))
	return docs
}

func EntriesToDocs(entries Entries) (res []int32) {
	for _, eid := range entries {
		res = append(res, eid.GetConjID().DocID())
	}
	return
}

func TestBEIndex_Retrieve(t *testing.T) {
	LogLevel = infoLevel

	builder := IndexerBuilder{
		Documents: make(map[int32]*Document),
	}

	for _, doc := range buildTestDoc() {
		builder.AddDocument(doc)
	}

	indexer := builder.BuildIndex()

	fmt.Println(indexer.DumpSizeEntries())

	result, e := indexer.Retrieve(map[BEField]Values{
		"age": NewValues(5),
	})
	fmt.Println(e, result)

	result, e = indexer.Retrieve(map[BEField]Values{
		"ip": NewStrValues("localhost"),
	})
	fmt.Println(e, result)

	result, e = indexer.Retrieve(map[BEField]Values{
		"age":  NewIntValues(1),
		"city": NewStrValues("sh"),
		"tag":  NewValues("tag1"),
	})
	fmt.Println(e, result)
}

type MockTargeting struct {
	ID int32
	A  []int
	B  []int
	C  []int
	D  []int
}

func (t *MockTargeting) ToConj() *Conjunction {
	conj := NewConjunction()
	if len(t.A) > 0 {
		conj.In("A", NewIntValues(t.A[0], t.A[1:]...))
	}
	if len(t.B) > 0 {
		conj.In("B", NewIntValues(t.B[0], t.B[1:]...))
	}
	if len(t.C) > 0 {
		conj.In("C", NewIntValues(t.C[0], t.C[1:]...))
	}
	if len(t.D) > 0 {
		conj.In("D", NewIntValues(t.D[0], t.D[1:]...))
	}
	return conj
}

func valueMatch(values, queries []int) bool {
	if len(values) == 0 {
		return true
	}
	for _, v := range queries {
		if util.ContainInt(values, v) {
			return true
		}
	}
	return false
}
func (t *MockTargeting) String() string {
	b, _ := json.Marshal(t)
	return string(b)
}

func (t *MockTargeting) Match(a, b, c, d []int) bool {
	if !valueMatch(t.A, a) {
		return false
	}
	if !valueMatch(t.B, b) {
		return false
	}
	if !valueMatch(t.C, c) {
		return false
	}
	if !valueMatch(t.D, d) {
		return false
	}
	return true
}

func randValue(cnt int) (res []int) {
	cnt = rand.Int() % cnt
	for i := 0; i < cnt; i++ {
		res = append(res, rand.Intn(20))
	}
	return util.DistinctInt(res)
}

func TestBEIndex_Retrieve2(t *testing.T) {
	b := NewIndexerBuilder()
	targets := map[int32]*MockTargeting{}

	for i := 1; i < 5000; i++ {
		target := &MockTargeting{
			ID: int32(i),
			A:  randValue(10),
			B:  randValue(5),
			C:  randValue(2),
			D:  randValue(6),
		}

		conj := target.ToConj()
		if len(conj.Expressions) > 0 {
			doc := NewDocument(target.ID)
			doc.AddConjunction(conj)
			b.AddDocument(doc)

			targets[int32(i)] = target
		}
	}

	index := b.BuildIndex()

	idxRes := map[int32]*MockTargeting{}
	noneIdxRes := map[int32]*MockTargeting{}

	for i := 0; i < 1000; i++ {
		A := randValue(10)
		B := randValue(5)
		C := randValue(2)
		D := randValue(6)

		for id, target := range targets {
			if target.Match(A, B, C, D) {
				noneIdxRes[id] = target
			}
		}
		assigns := Assignments{}
		if len(A) > 0 {
			assigns["A"] = NewIntValues(A[0], A[1:]...)
		}
		if len(B) > 0 {
			assigns["B"] = NewIntValues(B[0], B[1:]...)
		}
		if len(C) > 0 {
			assigns["C"] = NewIntValues(C[0], C[1:]...)
		}
		if len(D) > 0 {
			assigns["D"] = NewIntValues(D[0], D[1:]...)
		}
		ids, _ := index.Retrieve(assigns)
		for _, id := range ids {
			idxRes[id] = targets[id]
		}
		if len(idxRes) != len(noneIdxRes) {
			fmt.Printf("queries:A:%+v, B:%+v, C:%+v, D:%+v\n", A, B, C, D)
			fmt.Printf("noneIdxRes:%d, idxRes:%d\n", len(noneIdxRes), len(idxRes))
			fmt.Printf("IdxRes:%+v\n", idxRes)
			fmt.Printf("noneIdxRes:%+v\n", noneIdxRes)
			fmt.Println(index.DumpSizeEntries())
			panic(nil)
		}
	}
}

/*
gonghuan, k: 2
K:2, res:[32], plgList:total plgs:2
0:
idx:0#72057594037927940#cur:<nil,nil> entries:[<10,true> <19,true> <27,true> <32,true> <54,true> <81,true>]
idx:1#72057594037927946#cur:<nil,nil> entries:[<3,true> <19,true> <35,true> <81,true>]
1:
idx:0#288230376151711758#cur:<nil,nil> entries:[<17,true> <32,true> <37,true>]
idx:1#288230376151711757#cur:<nil,nil> entries:[<19,true> <60,true>]
idx:2#288230376151711747#cur:<nil,nil> entries:[<53,true> <54,true>]
idx:3#288230376151711744#cur:<nil,nil> entries:[<17,true> <33,true>]
*/
func DocIDToIncludeEntries(ids []int32, k int) (res []EntryID) {
	for _, id := range ids {
		res = append(res, NewEntryID(NewConjID(id, 0, k), true))
	}
	return res
}

func TestBEIndex_Retrieve3(t *testing.T) {
	plgs := FieldPostingListGroups{
		NewFieldPostingListGroup(PostingLists{
			{
				entries: DocIDToIncludeEntries([]int32{17, 32, 37}, 2),
			},
			{
				entries: DocIDToIncludeEntries([]int32{17, 33}, 2),
			},
			{
				entries: DocIDToIncludeEntries([]int32{19, 60}, 2),
			},
			{
				entries: DocIDToIncludeEntries([]int32{53, 54}, 2),
			},
		}...),
		NewFieldPostingListGroup(PostingLists{
			{
				entries: DocIDToIncludeEntries([]int32{10, 19, 27, 32, 54, 81}, 2),
			},
			{
				entries: DocIDToIncludeEntries([]int32{3, 19, 35, 81}, 2),
			},
		}...),
	}
	for _, plg := range plgs {
		plg.current = plg.plGroup[0]
	}

	index := &BEIndex{}
	fmt.Println(index.retrieveK(plgs, 2))
}
