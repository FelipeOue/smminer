package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"smminer/drivers"
	"smminer/util"
	"strings"
	"syscall"
	"time"
)

const (
	PACKAGE_VERSION   = "0.9.6"
	ASIC_TIMEOUT_MULT = 1.0
	ASIC_MIDSTATES    = 4
	// CPU_MODE enables CPU mining instead of the ASIC driver.
	// This is for TESTING only — set to false for normal operation.
	CPU_MODE   = false
	DEBUG_MODE = false
	// LOG_TO_FILE enables writing log entries to log.txt on disk.
	LOG_TO_FILE = false
)

func main() {

	util.DebugMode = DEBUG_MODE
	util.LogToFile = LOG_TO_FILE

	util.AppLog("MINER", "smminer "+PACKAGE_VERSION+" (beta) was started.", "")
	util.AppLog("MINER", "Target is AonMiners USB devices", "")

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)

	var poolURL string
	var poolUser string
	var poolPass string
	var aonFreq int
	var aonJobTimer int
	var suggestDiff int
	var baudRate int
	var hubVidPid string
	var showVersion bool

	flag.StringVar(&poolURL, "o", "", "pool URL")
	flag.StringVar(&poolUser, "u", "", "username")
	flag.StringVar(&poolPass, "p", "x", "password")
	flag.IntVar(&aonFreq, "aon-frequency", 150, "ASIC Frequency")
	flag.IntVar(&aonJobTimer, "aon-job-timer", 20, "ASIC Job timing")
	flag.IntVar(&suggestDiff, "suggest-diff", 500, "Suggest difficulty")
	flag.IntVar(&baudRate, "aon-baudrate", 1, "Set Baudrate (1=1M, 2=1.5M)")
	flag.StringVar(&hubVidPid, "aon-usb-hub", "1234:0001", "Define HUB vid:pid for hard resets")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")

	flag.Parse()

	if showVersion {
		fmt.Printf("smminer v%s\n", PACKAGE_VERSION)
		os.Exit(0)
	}

	poolURL = strings.TrimPrefix(poolURL, "stratum+tcp://")

	if poolURL == "" {
		util.AppLog("MINER", "Pool URL is required", "")
		os.Exit(1)
	}
	if poolUser == "" {
		util.AppLog("MINER", "Username is required", "")
		os.Exit(1)
	}
	if poolPass == "" {
		poolPass = "x"
	}
	if baudRate > 2 || baudRate < 1 {
		util.AppLog("MINER", "Invalid baudrate", "")
		os.Exit(1)
	}

	_ = StratumConnect(poolURL)
	_, err := StratumSubscribe()
	if err != nil {
		util.AppLog("STRATUM", "stratumSubscribe error:"+err.Error(), "")
		util.AppLog("STRATUM", "Fatal: stratum subscribe failed", "")
		return
	}

	err = StratumAuthenticate(poolUser, poolPass)
	if err != nil {
		util.AppLog("STRATUM", "stratumAuthenticate error:"+err.Error(), "")
	}

	go func() {
		for {
			StratumReceiver()
			//time.Sleep(1 * time.Second)
			time.Sleep(time.Millisecond * 500)
		}
	}()
	time.Sleep(1 * time.Second)
	StratumSuggestDiff(float64(suggestDiff))

	StratumWaitJob()

	done := make(chan struct{})
	shutdownChan := make(chan struct{})

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic in main: %v\n", r)
			close(done)
			<-shutdownChan
			os.Exit(1)
		}
	}()

	if CPU_MODE {
		util.AppLog("MINER", "CPU_MODE enabled — running CPU miner for testing", "")
		go func() {
			CPUMiner(done)
			close(shutdownChan)
		}()
	} else {
		StratumSetVersionMask(util.DEFAULT_VERSION_MASK)
		if aonFreq != -1 && aonJobTimer != -1 {
			drivers.AonUserConfig.Frequency = aonFreq
			drivers.AonUserConfig.JobTimer = aonJobTimer
			drivers.AonUserConfig.BaudRate = baudRate
			drivers.AonUserConfig.HubVidPid = hubVidPid
			go func() {
				AonMiner(done)
				close(shutdownChan)
			}()
		}
	}

	go func() {
		for {
			MinerReceiver()
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// TODO: Move CPU Hashrate reporting to drivers/cpu.go
	go func() {
		for {
			// shows hashrate
			drivers.CPU.Mutex.Lock()
			hashrate := drivers.CPU.Hashrate
			drivers.CPU.Mutex.Unlock()
			if hashrate > 0 {
				fmt.Print("\n\033[92m-----------------------------------------------------------\033[0m\n")
				util.AppLog("MINER:", fmt.Sprintf("CPU Hashrate: %sH/s", util.FormatDiff(float64(hashrate))), "")
				fmt.Print("\033[92m-----------------------------------------------------------\033[0m\n\n")
			}
			time.Sleep(30 * time.Second)
		}
	}()

	go func() {
		for {
			// handles diconnection
			if PoolDisconnection {
				util.AppLog("STRATUM", "Reconnecting to pool...", "")
				StratumMutex.Lock()
				_ = StratumConnect(poolURL)
				_, err := StratumSubscribe()
				if err != nil {
					util.AppLog("STRATUM", "stratumSubscribe error: "+err.Error(), "")
					time.Sleep(1 * time.Second)
					continue
				}

				err = StratumAuthenticate(poolUser, poolPass)
				if err != nil {
					util.AppLog("STRATUM", "stratumAuthenticate error: "+err.Error(), "")
					time.Sleep(1 * time.Second)
					continue
				}
				StratumSuggestDiff(float64(suggestDiff))
				PoolDisconnection = false
				//StratumMutex.Unlock()
			}
			if PoolDisconnection == false && StratumMutex.TryLock() {
				StratumMutex.Unlock()
			}
			time.Sleep(time.Millisecond * 500)
		}
	}()

	util.AppLog("MINER", "Running. Press Ctrl-C to stop...", "")
	<-stopChan
	close(done)
	<-shutdownChan
	util.AppLog("MINER", "Shutting down...", "")
}
