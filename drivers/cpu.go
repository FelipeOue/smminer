package drivers

import (
	"context"
	"encoding/hex"
	"smminer/util"
	"strings"
	"sync"
	"time"
)

// The shared StratumJob and StratumWork types are in util/general.go

type CPUData struct {
	Threads         int
	Mutex           sync.Mutex
	FinishedWork    []util.StratumWork
	FinishedThreads int
	Hashrate        int
	Status          string

	cancelFunc context.CancelFunc
	ctx        context.Context
}

var CPU = CPUData{
	Threads:         8,
	Mutex:           sync.Mutex{},
	FinishedWork:    []util.StratumWork{},
	FinishedThreads: 0,
	Hashrate:        0,
	Status:          "OFF",

	cancelFunc: nil,
	ctx:        context.Background(),
}

// Start to mine. Extranonce2 rolling is necessary
// to be done by the caller of this function
func StartCPUMining(job util.StratumJob, poolDifficulty *float64) {
	var ctxLocal context.Context
	ctxLocal, CPU.cancelFunc = context.WithCancel(context.Background())
	CPU.ctx = ctxLocal

	// [TEST] MODIFY JOB TO A HARDCODED AND KNOWN ONE
	// job.ID = "1"
	// job.PrevBlockHash = "c8dbd8ba69795c2296f8edbddfb673242ea20ce8000110cc0000000000000000"
	// job.BlockVersion = "20000000"
	// job.NetworkTarget = "17022b91"
	// job.BlockTimestamp = "68a9e2a8"
	// job.ForgetSubmits = false
	// job.MerkleRoot = "b9b1019261b5874e9486a178a9e5cf25141a53d96523142e3c9110882d239722"

	prevblockReversed, _ := util.ReverseWord32(job.PrevBlockHash)
	//log.Printf("PrevBlockHash (Original): %s \n PrevBlockHash (Reversed): %s", job.PrevBlockHash, prevblockReversed)
	// build block header
	blockHeader := strings.Join([]string{
		util.ReverseHexString(job.BlockVersion),
		prevblockReversed,
		job.MerkleRoot,
		util.ReverseHexString(job.BlockTimestamp),
		util.ReverseHexString(job.NetworkTarget),
	}, "")
	//log.Printf("Block Header: %s", blockHeader)
	//hexBlockHeader, _ := hex.DecodeString(blockHeader)
	//hexBlockHeader = util.ReverseBytes(hexBlockHeader)

	// reset finished threads counter
	CPU.Mutex.Lock()
	CPU.FinishedThreads = 0
	CPU.Status = "ON"
	CPU.Mutex.Unlock()

	// mining core loop with threads
	for i := 0; i < CPU.Threads; i++ {
		var chunkSize uint32 = 0xFFFFFFFF / uint32(CPU.Threads)
		var nonceInt uint32 = chunkSize * uint32(i)

		go func(i int) {
			timeMsFloat := float64(time.Now().UnixMilli())
			nonceCount := 0

			for nonceInt < chunkSize*uint32(i+1) {
				select {
				case <-CPU.ctx.Done():
					//log.Printf("Thread %d closed", i)
					if (i + 1) == CPU.Threads {
						CPU.Status = "OFF"
					}
					CPU.Mutex.Lock()
					CPU.FinishedThreads++
					CPU.Mutex.Unlock()
					return
				default:
				}
				//nonceInt = 911410603
				// add nonce to the block header
				//blockHeaderWithNonce := append(hexBlockHeader, util.ReverseBytes(util.PadLeft(nonceInt, 4))...)
				hexBlockHeader, _ := hex.DecodeString(blockHeader)
				blockHeaderWithNonce := append(hexBlockHeader, util.ReverseBytes(util.PadLeft(nonceInt, 4))...)
				//log.Println("blockHeaderWithNonce: ", hex.EncodeToString(blockHeaderWithNonce))
				hash := util.ReverseBytes(util.Sha256d(blockHeaderWithNonce))

				// check if the hash is below target
				if hash[0] == 0x00 && hash[1] == 0x00 && hash[2] == 0x00 { // check optimization
					if util.BytesToFloat64(hash) <= util.DiffToTarget(*poolDifficulty) {
						//log.Print("\n\n\n\n\n" + hex.EncodeToString(hash) + " found with nonce: " + hex.EncodeToString(util.PadLeft(nonceInt, 4)))
						CPU.Mutex.Lock()
						work := util.StratumWork{
							ID:             job.ID,
							WorkID:         util.GenerateUID(),
							Hash:           hex.EncodeToString(hash),
							Nonce:          hex.EncodeToString(util.PadLeft(nonceInt, 4)),
							ExtraNonce2:    job.ExtraNonce2,
							BlockTimestamp: job.BlockTimestamp,
							Difficulty:     util.TARGET_DIFF_ONE / util.BytesToFloat64(hash),
						}
						CPU.FinishedWork = append(CPU.FinishedWork, work)
						CPU.Mutex.Unlock()
					}
				}

				nonceInt++
				nonceCount++

				// show speed in hashes per second every x nonces
				if nonceCount == 1000000 {
					//log.Printf("diff is %f\n", *poolDifficulty)
					elapsed := float64(time.Now().UnixMilli()) - timeMsFloat
					hashrate := float64(nonceCount) / (elapsed / 1000.0)
					//log.Printf("Thread: %v Nonce: %s Hashrate: %.0f H/s", i, hex.EncodeToString(util.PadLeft(nonceInt, 4)), hashrate)
					timeMsFloat = float64(time.Now().UnixMilli())
					nonceCount = 0
					CPU.Mutex.Lock()
					CPU.Hashrate = int(hashrate * float64(CPU.Threads))
					CPU.Mutex.Unlock()
				}
			}

			// thread exhausted all nonces
			CPU.Mutex.Lock()
			CPU.FinishedThreads++
			CPU.Mutex.Unlock()
		}(i)
	}
}

// Cancels all running threads
func StopCPUMining() {
	if CPU.cancelFunc != nil {
		CPU.cancelFunc()
	}
}
