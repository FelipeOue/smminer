package main

import (
	"encoding/hex"
	"fmt"
	"smminer/drivers"
	"smminer/util"
	"strconv"
	"sync"
	"time"
)

// Here will be all miners handlers to receive stratum structs directly,
// each function must have to implement the mining loop with routines and
// logging without returning anything, only make changes to global variables
// like extranonce2 to avoid duplicated jobs.

// Drivers can not change global variables from the main package, any pointer
// provided to a driver must be read-only.

// In case of any returned error, the main function will try to re-call.
// Warnings must be sent to logging and truly fatal errors can be returned.

var (
	// common variables for all miners
	ExtraNonce1    = 0
	LastMinerWork  []StratumWork
	MinerWorkMutex sync.Mutex
	BestShare      = float64(0)
)

func CPUMiner(done chan struct{}) error {
	defer func() {
		if r := recover(); r != nil {
			util.AppLog("PANIC", fmt.Sprintf("Panic in CPUMiner: %v", r), "")
			fmt.Printf("Panic in CPUMiner: %v\n", r)
		}
	}()
	util.AppLog("MINER", "Starting CPU miner...", "")
	for {
		select {
		case <-done:
			return nil
		default:
			// increment nonce2
			StratumNonce2Increment()
			// start mining with current job if not already started
			if drivers.CPU.Status == "OFF" {
				drivers.StartCPUMining(util.StratumJob(LastStratumJob), &PoolDifficulty)
			}
			// wait until cpu status turns off again
			for drivers.CPU.Status == "ON" {
				time.Sleep(1 * time.Second)
			}
		}
	}
}

func AonMiner(done chan struct{}) error {
	defer func() {
		if r := recover(); r != nil {
			util.AppLog("PANIC", fmt.Sprintf("Panic in aonminer driver: %v", r), "")
			close(done)
		}
	}()

	util.AppLog("MINER", "Starting aonminer driver...", "")

	connectedDevices := -1
	consecutiveFailures := 0
	lastJobChange := time.Now()
	forceJobUpdate := true
	var stratumJobBuffer []StratumJob
	for {

		select {
		case <-done:
			util.AppLog("MINER", "Closing all device ports before closing...", "")
			for _, device := range drivers.AonDriver.ConnectedDevices {
				if active, _ := util.SerialFTDIToggleRTS(device); active {
					util.AppLog("MINER", fmt.Sprintf("Turning off device from port %d\n", device.Info.Port), "")
					time.Sleep(500 * time.Millisecond)
					util.SerialFTDIToggleRTS(device)
					time.Sleep(500 * time.Millisecond)
				}
				device.Close()
			}
			return nil
		default:
			time.Sleep(time.Millisecond * 1)
		}

		processingTime := time.Now()
		if connectedDevices != len(drivers.AonDriver.ConnectedDevices) || len(drivers.AonDriver.ConnectedDevices) == 0 {
			// scan and connect to devices
			drivers.AonJobMutex.Lock()
			if drivers.AonUserConfig.HubVidPid != "1234:0001" && drivers.AonUserConfig.HubVidPid != "" {
				util.AppLog("MINER", "Resetting USB Hub...", "")
				err := util.UsbReset(drivers.AonUserConfig.HubVidPid)
				if err != nil {
					util.AppLog("MINER", err.Error(), "")
					time.Sleep(3 * time.Second)
				} else {
					util.AppLog("MINER", "Waiting for device enumeration...", "")
					time.Sleep(10 * time.Second)
				}
			}
			devices := drivers.AsicScan([]int{})
			if len(devices) == 0 {
				consecutiveFailures++
				// On first failure or after 3 consecutive failures,
				// forcibly cycle the USB device with terminal commands.
				if consecutiveFailures == 1 || consecutiveFailures >= 3 {
					util.AppLog("MINER", fmt.Sprintf("Scan failure #%d, cycling USB device 0403:6015...", consecutiveFailures), "")
					err := util.UsbReset("0403:6015")
					if err != nil {
						util.AppLog("MINER", "USB reset error: "+err.Error(), "")
						time.Sleep(3 * time.Second)
					} else {
						util.AppLog("MINER", "USB reset OK, waiting for enumeration...", "")
						time.Sleep(10 * time.Second)
						consecutiveFailures = 0
					}
				} else {
					time.Sleep(time.Second * 1)
				}
			}
			if connectedDevices == -1 {
				util.AppLog("MINER", "Found "+strconv.Itoa(len(devices))+" compatible device(s)", "")
			}
			// Initialize devices
			if len(devices) > 0 {
				consecutiveFailures = 0
				util.AppLog("MINER", "Initializing device(s)...", "")
				var initialized *int
				initialized = new(int)
				for _, device := range devices {
					go func(device *util.SerialDevice) {
						drivers.AsicInit(device, initialized)
						util.SerialFTDILatency(device.Device, 2)
						drivers.AonDriver.ConnectedDevices = append(drivers.AonDriver.ConnectedDevices, device)
					}(device)
					time.Sleep(time.Millisecond * 200)
				}
				// wait until all is initialized
				for *initialized < len(devices) {
					time.Sleep(time.Millisecond * 500)
				}
				if len(devices) > 0 {
					go drivers.AsicReadAll(&PoolDifficulty)
				}
			}
			connectedDevices = len(drivers.AonDriver.ConnectedDevices)

			for range devices {
				stratumJobBuffer = append(stratumJobBuffer, StratumJob{})
			}
			lastJobChange = time.Now()
			drivers.AonJobMutex.Unlock()
		}

		if len(drivers.AonDriver.ConnectedDevices) == 0 {
			continue
		}

		if LastStratumJob.ForgetSubmits {
			forceJobUpdate = true
		}

		if stratumJobBuffer[0].PrevBlockHash != LastStratumJob.PrevBlockHash ||
			(time.Since(lastJobChange) >= time.Second*5 && LastStratumJob.BlockTimestamp != stratumJobBuffer[0].BlockTimestamp) ||
			forceJobUpdate {
			StratumMutex.Lock()
			// Get base ExtraNonce2 value
			baseExtraNonce2Int, _ := strconv.ParseUint(LastStratumJob.ExtraNonce2, 16, 32)

			// Add a copy of last job to the buffer by the number of devices
			// Each device gets a unique ExtraNonce2
			for i := range drivers.AonDriver.ConnectedDevices {
				stratumJobBuffer[i] = LastStratumJob
				// Give each device a unique ExtraNonce2 by adding device index
				uniqueExtraNonce2 := baseExtraNonce2Int + uint64(i) + 1
				stratumJobBuffer[i].ExtraNonce2 = hex.EncodeToString(util.PadLeft(uint32(uniqueExtraNonce2), PoolExtraNonce2Len))
			}
			StratumMutex.Unlock()
			lastJobChange = time.Now()
			forceJobUpdate = false
		}

		// edit job for each device
		for i := range drivers.AonDriver.ConnectedDevices {
			extraNonce2Int, err := strconv.ParseUint(stratumJobBuffer[i].ExtraNonce2, 16, 32)
			if err == nil {
				// Increment by a larger value to ensure uniqueness across iterations
				extraNonce2Int += uint64(len(drivers.AonDriver.ConnectedDevices))
				stratumJobBuffer[i].ExtraNonce2 = hex.EncodeToString(util.PadLeft(uint32(extraNonce2Int), PoolExtraNonce2Len))
			}
			stratumJobBuffer[i].MerkleRoot, _ = util.ComputeMerkleRoot(
				stratumJobBuffer[i].CoinbasePrefix,
				stratumJobBuffer[i].ExtraNonce1,
				stratumJobBuffer[i].ExtraNonce2,
				stratumJobBuffer[i].CoinbaseSuffix,
				stratumJobBuffer[i].MerkleBranches,
			)
			//log.Printf("generated merkleroot: %s , DevID: %d", stratumJobBuffer[i].MerkleRoot, i)
		}
		processingTimeD := time.Duration(time.Since(processingTime))
		// sleep for processing time + 3.01ms + 500us (USB 1.1 Full-Speed OR FT230XQ)
		// sleep for processing time + 2.1ms + 500us (USB 2.0 High-Speed)
		//time.Sleep(processingTimeD + time.Millisecond*3 + time.Microsecond*500)
		time.Sleep((time.Millisecond * time.Duration(4+drivers.AonUserConfig.JobTimer)) - processingTimeD)
		for i := range drivers.AonDriver.ConnectedDevices {
			device := drivers.AonDriver.ConnectedDevices[i]
			go drivers.AsicSendJob(i, device, util.StratumJob(stratumJobBuffer[i]), &PoolDifficulty)
		}
	}
}

// check finished work from all miners
func MinerReceiver() {
	defer func() {
		if r := recover(); r != nil {
			util.AppLog("PANIC", fmt.Sprintf("Panic in MinerReceiver: %v", r), "")
			fmt.Printf("Panic in MinerReceiver: %v\n", r)
		}
	}()
	// CPU
	// restart mining if job need change
	if LastStratumJob.ForgetSubmits {
		util.AppLog("MINER", "New job, pool requested job change.", "")
		drivers.StopCPUMining()
		StratumMutex.Lock()
		LastStratumJob.ForgetSubmits = false
		StratumMutex.Unlock()
	}
	if len(drivers.CPU.FinishedWork) > 0 {
		drivers.CPU.Mutex.Lock()
		work := drivers.CPU.FinishedWork[0]
		drivers.CPU.FinishedWork = drivers.CPU.FinishedWork[1:]
		util.AppDebug("MINER", fmt.Sprintf("CPU found a valid nonce: %s", work.Nonce), "")
		StratumSubmit(StratumWork{
			ID:             work.ID,
			WorkID:         work.WorkID,
			ExtraNonce2:    work.ExtraNonce2,
			BlockTimestamp: work.BlockTimestamp,
			Nonce:          work.Nonce,
			Difficulty:     work.Difficulty,
		}, false)
		defer drivers.CPU.Mutex.Unlock()
		time.Sleep(1 * time.Second)
	}

	// AONMINERS
	// So fast that do not need to change job while mining
	// TODO: discard stale job
	if len(drivers.AONMINER.FinishedWork) > 0 {
		drivers.AONMINER.Mutex.Lock()
		work := drivers.AONMINER.FinishedWork[0]
		drivers.AONMINER.FinishedWork = drivers.AONMINER.FinishedWork[1:]
		drivers.AONMINER.Mutex.Unlock()
		util.AppDebug("MINER", fmt.Sprintf("AonMiner found a valid nonce: %s", work.Nonce), "")
		StratumSubmit(StratumWork{
			ID:             work.ID,
			WorkID:         work.WorkID,
			ExtraNonce2:    work.ExtraNonce2,
			BlockTimestamp: work.BlockTimestamp,
			Nonce:          work.Nonce,
			Difficulty:     work.Difficulty,
			Version:        work.Version,
		}, true)
	}

	if len(drivers.AonFinishedWorks) > 0 {

		if PoolExtraNonce1Changed {
			PoolExtraNonce1Changed = false
			drivers.AonJobMutex.Lock()
			drivers.AonFinishedWorks = nil
			drivers.AonJobMutex.Unlock()
			return
		}

		drivers.AonJobMutex.Lock()
		var worksToProcess []StratumWork
		for _, work := range drivers.AonFinishedWorks {
			worksToProcess = append(worksToProcess, StratumWork{
				ID:             work.ID,
				WorkID:         work.WorkID,
				ExtraNonce2:    work.ExtraNonce2,
				BlockTimestamp: work.BlockTimestamp,
				Nonce:          work.Nonce,
				Difficulty:     work.Difficulty,
				Version:        work.Version,
			})
		}
		drivers.AonFinishedWorks = nil
		drivers.AonJobMutex.Unlock()

		for _, work := range worksToProcess {
			isDuplicate := false
			for i := range LastMinerWork {
				if work.Nonce == LastMinerWork[i].Nonce &&
					work.ExtraNonce2 == LastMinerWork[i].ExtraNonce2 &&
					work.BlockTimestamp == LastMinerWork[i].BlockTimestamp {
					isDuplicate = true
					break
				}
			}
			if isDuplicate {
				continue
			}

			if len(LastMinerWork) >= 50 {
				LastMinerWork = LastMinerWork[len(LastMinerWork)-25:]
			}

			StratumSubmit(work, true)
			LastMinerWork = append(LastMinerWork, work)
		}
	}
}
