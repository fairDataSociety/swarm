// Copyright 2018 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.
package storage

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/ethersphere/swarm/chunk"
	"github.com/ethersphere/swarm/log"
	"github.com/ethersphere/swarm/storage/localstore"
	"sync"
)

const (
	PinVersion     = "1.0"
	DONT_PIN       = 0
	WorkerChanSize = 8
)

var PinApiInstance *PinApi
var once sync.Once

type PinApi struct {
	db         *localstore.DB
	netStore   ChunkStore
	fileParams *FileStoreParams
	tag        *chunk.Tags
}

func NewPinApi(lstore *localstore.DB, store ChunkStore, params *FileStoreParams, tags *chunk.Tags) *PinApi {
	pinApi := &PinApi{
		db:         lstore,
		netStore:   store,
		fileParams: params,
		tag:        tags,
	}
	once.Do(func() {
		PinApiInstance = pinApi
	})

	return pinApi
}

func GetPinInstance() *PinApi{
	return PinApiInstance
}

func (p *PinApi) ShowDatabase() string {
	p.db.ShowDatabaseInformation()
	return "Check the swarm log file for the output"
}

func (p *PinApi) AddPinFile(roothash []byte, isRaw bool) error {
	return p.db.AddToPinFileIndex(roothash, isRaw)
}

func (p *PinApi) ListPinFiles() {
	p.db.ListPinnedFiles()
}

func (p *PinApi) ShowChunksOfRootHash(rootHash string) {

	workers := make(chan Reference, WorkerChanSize)
	doneC := make(chan struct{})

	hashFunc := MakeHashFunc(p.fileParams.Hash)
	addr, err :=  hex.DecodeString(rootHash)
	fmt.Println("Address", fmt.Sprintf("%x",addr))
	if err != nil {
		log.Info("Error decoding root hash")
		return
	}
	hashSize := int64(len(addr))
	isEncrypted := len(addr) > hashFunc().Size()
	tag := chunk.NewTag(0, "show-chunks-tag", 0)

	getter := NewHasherStore(p.db, hashFunc, isEncrypted, tag, DONT_PIN)

	workers <- Reference(addr)
	go func() {
		for {
			select {

			case <-doneC:
				// no more chunks to get.. Quit the command
				break

			case ref := <-workers:
				// got a new chunk.. print it.
				chunkData, err := getter.Get(context.TODO(), ref)
				if err != nil {
					log.Info("Error getting chunk data from localstore.")
					break
				}

				datalen := int64(len(chunkData))
				if datalen < 9 {
					log.Info("Invalid chunk data from localstore.")
					break
				}

				subTreeSize := chunkData.Size()
				if subTreeSize > chunk.DefaultSize {
					branches := (datalen - 8) / hashSize
					log.Info("no of branches", branches)
				} else {
					// Data chunk encountered... stop here
					break
				}

				// if tree chunk, get the hashes and put in worker Q

			}
		}

	}()

}

func (p *PinApi) IsHashPinned(addr []byte) bool {
	return p.db.IsHashPinned(addr)
}

func (p *PinApi) PinHash() string {

	// see if hash is valid and present in local DB

	// call loopAndPinHash (hash)

	return "Pin called"
}

//
//
//func (p *PinApi)  UnPinHash(hash Address) string {
//
//	// See if the root hash is pinned
//
//	// call
//
//
//	return "UnPin called"
//}

//func (p *PinApi)  ListPinnedHashes() PinInfo {
//
//
//	return pinInfo
//}
//
//
//
//func loopAndPinHash(Address hashToPin) {
//
//	// for all the chunks in the hash
//
//	//   - Send to the Pin Queue
//
//	//   - When all chunks are pinned without error, return true otherwise false
//
//	//	 All pin ref. increment should be atomic
//
//	//
//
//}
//
//func loopAndUnpinHash(Address hashToUnpin) {
//
//	// for all the chunks in hash
//
//	//  - Send to the unpin Queue
//
//	//  - When all chunks are unpinned without error, return true otherwise false
//
//	//   All unpin ref. decrement should be atomic
//}
//
//
//func pinChunk(hunk chunkToPin) {
//
//	// < This should be spawned as a go-routine, 8 go-routine >
//
//	// read chunk from the pin Queue
//
//	// Increment the pinning reference counter
//
//}
//
//
//func unpinChunk(Chunk chunkToUnpin) {
//
//	// < This should be spawned as a go-routine, 8 go-routine >
//
//	// read the chunk address from the unpin Queue
//
//	// decrement the pinning reference counter
//
//}
