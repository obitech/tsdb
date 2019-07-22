package testutil

import (
	"context"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/tsdb"
	"github.com/prometheus/tsdb/labels"
	"github.com/prometheus/tsdb/tsdbutil"
)

// OpenTestDB opens a test Database
func OpenTestDB(t testing.TB, opts *tsdb.Options) (db *tsdb.DB, close func()) {
	tmpdir, err := ioutil.TempDir("", "test")
	Ok(t, err)

	db, err = tsdb.Open(tmpdir, nil, nil, opts)
	Ok(t, err)

	// Do not close the test database by default as it will deadlock on test failures.
	return db, func() {
		Ok(t, os.RemoveAll(tmpdir))
	}
}

// CreateBlock creates a block with given set of series and returns its dir.
func CreateBlock(tb testing.TB, dir string, series []tsdb.Series) string {
	head := createHead(tb, series)
	compactor, err := tsdb.NewLeveledCompactor(context.Background(), nil, log.NewNopLogger(), []int64{1000000}, nil)
	Ok(tb, err)

	Ok(tb, os.MkdirAll(dir, 0777))

	// Add +1 millisecond to block maxt because block intervals are half-open: [b.MinTime, b.MaxTime).
	// Because of this block intervals are always +1 than the total samples it includes.
	ulid, err := compactor.Write(dir, head, head.MinTime(), head.MaxTime()+1, nil)
	Ok(tb, err)
	return filepath.Join(dir, ulid.String())
}

func createHead(tb testing.TB, series []tsdb.Series) *tsdb.Head {
	head, err := tsdb.NewHead(nil, nil, nil, 2*60*60*1000)
	Ok(tb, err)
	defer head.Close()

	app := head.Appender()
	for _, s := range series {
		ref := uint64(0)
		it := s.Iterator()
		for it.Next() {
			t, v := it.At()
			if ref != 0 {
				err := app.AddFast(ref, t, v)
				if err == nil {
					continue
				}
			}
			ref, err = app.Add(s.Labels(), t, v)
			Ok(tb, err)
		}
		Ok(tb, it.Err())
	}
	err = app.Commit()
	Ok(tb, err)
	return head
}

const (
	defaultLabelName  = "labelName"
	defaultLabelValue = "labelValue"
)

type sample struct {
	t int64
	v float64
}

func (s sample) T() int64 {
	return s.t
}

func (s sample) V() float64 {
	return s.v
}

// GenSeries generates series with a given number of labels and values.
func GenSeries(totalSeries, labelCount int, mint, maxt int64) []tsdb.Series {
	if totalSeries == 0 || labelCount == 0 {
		return nil
	}

	series := make([]tsdb.Series, totalSeries)

	for i := 0; i < totalSeries; i++ {
		lbls := make(map[string]string, labelCount)
		lbls[defaultLabelName] = strconv.Itoa(i)
		for j := 1; len(lbls) < labelCount; j++ {
			lbls[defaultLabelName+strconv.Itoa(j)] = defaultLabelValue + strconv.Itoa(j)
		}
		samples := make([]tsdbutil.Sample, 0, maxt-mint+1)
		for t := mint; t < maxt; t++ {
			samples = append(samples, sample{t: t, v: rand.Float64()})
		}
		series[i] = newSeries(lbls, samples)
	}
	return series
}

type mockSeries struct {
	labels   func() labels.Labels
	iterator func() tsdb.SeriesIterator
}

func newSeries(l map[string]string, s []tsdbutil.Sample) tsdb.Series {
	return &mockSeries{
		labels:   func() labels.Labels { return labels.FromMap(l) },
		iterator: func() tsdb.SeriesIterator { return newListSeriesIterator(s) },
	}
}
func (m *mockSeries) Labels() labels.Labels         { return m.labels() }
func (m *mockSeries) Iterator() tsdb.SeriesIterator { return m.iterator() }

type listSeriesIterator struct {
	list []tsdbutil.Sample
	idx  int
}

func newListSeriesIterator(list []tsdbutil.Sample) *listSeriesIterator {
	return &listSeriesIterator{list: list, idx: -1}
}

func (it *listSeriesIterator) At() (int64, float64) {
	s := it.list[it.idx]
	return s.T(), s.V()
}

func (it *listSeriesIterator) Next() bool {
	it.idx++
	return it.idx < len(it.list)
}

func (it *listSeriesIterator) Seek(t int64) bool {
	if it.idx == -1 {
		it.idx = 0
	}
	// Do binary search between current position and end.
	it.idx = sort.Search(len(it.list)-it.idx, func(i int) bool {
		s := it.list[i+it.idx]
		return s.T() >= t
	})

	return it.idx < len(it.list)
}

func (it *listSeriesIterator) Err() error {
	return nil
}
