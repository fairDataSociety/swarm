// Copyright 2018 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package localstore

import (
	"bytes"
	"context"
	"github.com/ethersphere/swarm/shed"
	"io/ioutil"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/ethersphere/swarm/chunk"
)

// TestDB_collectGarbageWorker tests garbage collection runs
// by uploading and syncing a number of chunks.
func TestDB_collectGarbageWorker(t *testing.T) {
	testDB_collectGarbageWorker(t)
}

// TestDB_collectGarbageWorker_multipleBatches tests garbage
// collection runs by uploading and syncing a number of
// chunks by having multiple smaller batches.
func TestDB_collectGarbageWorker_multipleBatches(t *testing.T) {
	// lower the maximal number of chunks in a single
	// gc batch to ensure multiple batches.
	defer func(s uint64) { gcBatchSize = s }(gcBatchSize)
	gcBatchSize = 2

	testDB_collectGarbageWorker(t)
}

// testDB_collectGarbageWorker is a helper test function to test
// garbage collection runs by uploading and syncing a number of chunks.
func testDB_collectGarbageWorker(t *testing.T) {
	t.Helper()

	chunkCount := 150

	db, cleanupFunc := newTestDB(t, &Options{
		Capacity: 100,
	})
	testHookCollectGarbageChan := make(chan uint64)
	defer setTestHookCollectGarbage(func(collectedCount uint64) {
		select {
		case testHookCollectGarbageChan <- collectedCount:
		case <-db.close:
		}
	})()
	defer cleanupFunc()

	addrs := make([]chunk.Address, 0)

	// upload random chunks
	for i := 0; i < chunkCount; i++ {
		ch := generateTestRandomChunk()

		_, err := db.Put(context.Background(), chunk.ModePutUpload, ch)
		if err != nil {
			t.Fatal(err)
		}

		err = db.Set(context.Background(), chunk.ModeSetSync, ch.Address())
		if err != nil {
			t.Fatal(err)
		}

		addrs = append(addrs, ch.Address())

	}

	gcTarget := db.gcTarget()

	for {
		select {
		case <-testHookCollectGarbageChan:
		case <-time.After(10 * time.Second):
			t.Error("collect garbage timeout")
		}
		gcSize, err := db.gcSize.Get()
		if err != nil {
			t.Fatal(err)
		}
		if gcSize == gcTarget {
			break
		}
	}

	t.Run("pull index count", newItemsCountTest(db.pullIndex, int(gcTarget)))

	t.Run("gc index count", newItemsCountTest(db.gcIndex, int(gcTarget)))

	t.Run("gc size", newIndexGCSizeTest(db))

	// the first synced chunk should be removed
	t.Run("get the first synced chunk", func(t *testing.T) {
		_, err := db.Get(context.Background(), chunk.ModeGetRequest, addrs[0])
		if err != chunk.ErrChunkNotFound {
			t.Errorf("got error %v, want %v", err, chunk.ErrChunkNotFound)
		}
	})

	t.Run("only first inserted chunks should be removed", func(t *testing.T) {
		for i := 0; i < (chunkCount - int(gcTarget)); i++ {
			_, err := db.Get(context.Background(), chunk.ModeGetRequest, addrs[i])
			if err != chunk.ErrChunkNotFound {
				t.Errorf("got error %v, want %v", err, chunk.ErrChunkNotFound)
			}
		}
	})

	// last synced chunk should not be removed
	t.Run("get most recent synced chunk", func(t *testing.T) {
		_, err := db.Get(context.Background(), chunk.ModeGetRequest, addrs[len(addrs)-1])
		if err != nil {
			t.Fatal(err)
		}
	})
}

// Pin a file, upload chunks to go past the gc limit to trigger GC,
// check if the pinned files are still around
func TestPinGC(t *testing.T) {

	chunkCount := 150
	pinChunksCount := 50
	dbCapacity := uint64(100)

	db, cleanupFunc := newTestDB(t, &Options{
		Capacity: dbCapacity,
	})
	testHookCollectGarbageChan := make(chan uint64)
	defer setTestHookCollectGarbage(func(collectedCount uint64) {
		select {
		case testHookCollectGarbageChan <- collectedCount:
		case <-db.close:
		}
	})()
	defer cleanupFunc()

	addrs := make([]chunk.Address, 0)
	pinAddrs := make([]chunk.Address, 0)

	// upload random chunks
	for i := 0; i < chunkCount; i++ {
		ch := generateTestRandomChunk()

		_, err := db.Put(context.Background(), chunk.ModePutUpload, ch)
		if err != nil {
			t.Fatal(err)
		}

		err = db.Set(context.Background(), chunk.ModeSetSync, ch.Address())
		if err != nil {
			t.Fatal(err)
		}

		addrs = append(addrs, ch.Address())

		// Pin the chunks at the beginning to make sure they are not removed by GC
		if i < pinChunksCount {
			err = db.Set(context.Background(), chunk.ModeSetPin, ch.Address())
			if err != nil {
				t.Fatal(err)
			}
			pinAddrs = append(pinAddrs, ch.Address())
		}
	}
	gcTarget := db.gcTarget()

	for {
		select {
		case <-testHookCollectGarbageChan:
		case <-time.After(10 * time.Second):
			t.Error("collect garbage timeout")
		}
		gcSize, err := db.gcSize.Get()
		if err != nil {
			t.Fatal(err)
		}
		if gcSize == gcTarget {
			break
		}
	}

	t.Run("pin Index count", newItemsCountTest(db.pinIndex, int(pinChunksCount)))

	t.Run("gc exclude index count", newItemsCountTest(db.gcExcludeIndex, int(0)))

	t.Run("pull index count", newItemsCountTest(db.pullIndex, int(gcTarget) + pinChunksCount))

	t.Run("gc index count", newItemsCountTest(db.gcIndex, int(gcTarget)))

	t.Run("gc size", newIndexGCSizeTest(db))

	t.Run("pinned chunk not in gc Index", func(t *testing.T) {
		err := db.gcIndex.Iterate(func(item shed.Item) (stop bool, err error) {
			for _, pinHash := range pinAddrs {
				if bytes.Equal(pinHash, item.Address) {
					t.Fatal("pin chunk present in gcIndex")
				}
			}
			return false, nil
		}, nil)
		if err != nil {
			t.Fatal("could not iterate gcIndex")
		}
	})

	t.Run("pinned chunks exists", func(t *testing.T) {
		for _, hash := range pinAddrs {
			_, err := db.Get(context.Background(), chunk.ModeGetRequest, hash)
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("first chunks after pinned chunks should be removed", func(t *testing.T) {
		for i := pinChunksCount; i < (int(dbCapacity) - int(gcTarget)); i++ {
			_, err := db.Get(context.Background(), chunk.ModeGetRequest, addrs[i])
			if err != chunk.ErrChunkNotFound {
				t.Fatal(err)
			}
		}
	})


}

// Upload chunks, pin those chunks, add to GC after it is pinned
// check if the pinned files are still around
func TestGCAfterPin(t *testing.T) {

	chunkCount := 50

	db, cleanupFunc := newTestDB(t, &Options{
		Capacity: 100,
	})
	defer cleanupFunc()

	pinAddrs := make([]chunk.Address, 0)

	// upload random chunks
	for i := 0; i < chunkCount; i++ {
		ch := generateTestRandomChunk()

		_, err := db.Put(context.Background(), chunk.ModePutUpload, ch)
		if err != nil {
			t.Fatal(err)
		}

		// Pin before adding to GC in ModeSetSync
		err = db.Set(context.Background(), chunk.ModeSetPin, ch.Address())
		if err != nil {
			t.Fatal(err)
		}
		pinAddrs = append(pinAddrs, ch.Address())

		err = db.Set(context.Background(), chunk.ModeSetSync, ch.Address())
		if err != nil {
			t.Fatal(err)
		}
	}

	t.Run("pin Index count", newItemsCountTest(db.pinIndex, int(chunkCount)))

	t.Run("gc exclude index count", newItemsCountTest(db.gcExcludeIndex, int(chunkCount)))

	t.Run("gc index count", newItemsCountTest(db.gcIndex, int(0)))

	for _, hash := range pinAddrs {
		_, err := db.Get(context.Background(), chunk.ModeGetRequest, hash)
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestDB_collectGarbageWorker_withRequests is a helper test function
// to test garbage collection runs by uploading, syncing and
// requesting a number of chunks.
func TestDB_collectGarbageWorker_withRequests(t *testing.T) {
	db, cleanupFunc := newTestDB(t, &Options{
		Capacity: 100,
	})
	defer cleanupFunc()

	testHookCollectGarbageChan := make(chan uint64)
	defer setTestHookCollectGarbage(func(collectedCount uint64) {
		testHookCollectGarbageChan <- collectedCount
	})()

	addrs := make([]chunk.Address, 0)

	// upload random chunks just up to the capacity
	for i := 0; i < int(db.capacity)-1; i++ {
		ch := generateTestRandomChunk()

		_, err := db.Put(context.Background(), chunk.ModePutUpload, ch)
		if err != nil {
			t.Fatal(err)
		}

		err = db.Set(context.Background(), chunk.ModeSetSync, ch.Address())
		if err != nil {
			t.Fatal(err)
		}

		addrs = append(addrs, ch.Address())
	}

	// set update gc test hook to signal when
	// update gc goroutine is done by closing
	// testHookUpdateGCChan channel
	testHookUpdateGCChan := make(chan struct{})
	resetTestHookUpdateGC := setTestHookUpdateGC(func() {
		close(testHookUpdateGCChan)
	})

	// request the latest synced chunk
	// to prioritize it in the gc index
	// not to be collected
	_, err := db.Get(context.Background(), chunk.ModeGetRequest, addrs[0])
	if err != nil {
		t.Fatal(err)
	}

	// wait for update gc goroutine to finish for garbage
	// collector to be correctly triggered after the last upload
	select {
	case <-testHookUpdateGCChan:
	case <-time.After(10 * time.Second):
		t.Fatal("updateGC was not called after getting chunk with ModeGetRequest")
	}

	// no need to wait for update gc hook anymore
	resetTestHookUpdateGC()

	// upload and sync another chunk to trigger
	// garbage collection
	ch := generateTestRandomChunk()
	_, err = db.Put(context.Background(), chunk.ModePutUpload, ch)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Set(context.Background(), chunk.ModeSetSync, ch.Address())
	if err != nil {
		t.Fatal(err)
	}
	addrs = append(addrs, ch.Address())

	// wait for garbage collection

	gcTarget := db.gcTarget()

	var totalCollectedCount uint64
	for {
		select {
		case c := <-testHookCollectGarbageChan:
			totalCollectedCount += c
		case <-time.After(10 * time.Second):
			t.Error("collect garbage timeout")
		}
		gcSize, err := db.gcSize.Get()
		if err != nil {
			t.Fatal(err)
		}
		if gcSize == gcTarget {
			break
		}
	}

	wantTotalCollectedCount := uint64(len(addrs)) - gcTarget
	if totalCollectedCount != wantTotalCollectedCount {
		t.Errorf("total collected chunks %v, want %v", totalCollectedCount, wantTotalCollectedCount)
	}

	t.Run("pull index count", newItemsCountTest(db.pullIndex, int(gcTarget)))

	t.Run("gc index count", newItemsCountTest(db.gcIndex, int(gcTarget)))

	t.Run("gc size", newIndexGCSizeTest(db))

	// requested chunk should not be removed
	t.Run("get requested chunk", func(t *testing.T) {
		_, err := db.Get(context.Background(), chunk.ModeGetRequest, addrs[0])
		if err != nil {
			t.Fatal(err)
		}
	})

	// the second synced chunk should be removed
	t.Run("get gc-ed chunk", func(t *testing.T) {
		_, err := db.Get(context.Background(), chunk.ModeGetRequest, addrs[1])
		if err != chunk.ErrChunkNotFound {
			t.Errorf("got error %v, want %v", err, chunk.ErrChunkNotFound)
		}
	})

	// last synced chunk should not be removed
	t.Run("get most recent synced chunk", func(t *testing.T) {
		_, err := db.Get(context.Background(), chunk.ModeGetRequest, addrs[len(addrs)-1])
		if err != nil {
			t.Fatal(err)
		}
	})
}

// TestDB_gcSize checks if gcSize has a correct value after
// database is initialized with existing data.
func TestDB_gcSize(t *testing.T) {
	dir, err := ioutil.TempDir("", "localstore-stored-gc-size")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	baseKey := make([]byte, 32)
	if _, err := rand.Read(baseKey); err != nil {
		t.Fatal(err)
	}
	db, err := New(dir, baseKey, nil)
	if err != nil {
		t.Fatal(err)
	}

	count := 100

	for i := 0; i < count; i++ {
		ch := generateTestRandomChunk()

		_, err := db.Put(context.Background(), chunk.ModePutUpload, ch)
		if err != nil {
			t.Fatal(err)
		}

		err = db.Set(context.Background(), chunk.ModeSetSync, ch.Address())
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = New(dir, baseKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	t.Run("gc index size", newIndexGCSizeTest(db))
}

// setTestHookCollectGarbage sets testHookCollectGarbage and
// returns a function that will reset it to the
// value before the change.
func setTestHookCollectGarbage(h func(collectedCount uint64)) (reset func()) {
	current := testHookCollectGarbage
	reset = func() { testHookCollectGarbage = current }
	testHookCollectGarbage = h
	return reset
}

// TestSetTestHookCollectGarbage tests if setTestHookCollectGarbage changes
// testHookCollectGarbage function correctly and if its reset function
// resets the original function.
func TestSetTestHookCollectGarbage(t *testing.T) {
	// Set the current function after the test finishes.
	defer func(h func(collectedCount uint64)) { testHookCollectGarbage = h }(testHookCollectGarbage)

	// expected value for the unchanged function
	original := 1
	// expected value for the changed function
	changed := 2

	// this variable will be set with two different functions
	var got int

	// define the original (unchanged) functions
	testHookCollectGarbage = func(_ uint64) {
		got = original
	}

	// set got variable
	testHookCollectGarbage(0)

	// test if got variable is set correctly
	if got != original {
		t.Errorf("got hook value %v, want %v", got, original)
	}

	// set the new function
	reset := setTestHookCollectGarbage(func(_ uint64) {
		got = changed
	})

	// set got variable
	testHookCollectGarbage(0)

	// test if got variable is set correctly to changed value
	if got != changed {
		t.Errorf("got hook value %v, want %v", got, changed)
	}

	// set the function to the original one
	reset()

	// set got variable
	testHookCollectGarbage(0)

	// test if got variable is set correctly to original value
	if got != original {
		t.Errorf("got hook value %v, want %v", got, original)
	}
}
