package drivers

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"smminer/drivers/components"
	"smminer/util"
	"sync"
	"time"
)

type BMJob struct {
	MidstateID     byte
	JobID          byte
	MidstatesCount byte
	StartingNonce  []byte
	NetworkTarget  []byte
	BlockTimestamp []byte
	MerkleRoot     []byte
	Midstates      [][]byte

	PrevBlockHash []byte
	Version       []byte

	StratumJob util.StratumJob
}

// Miner interface for device info, control and status
type AonDevice struct {
	SerialPrefix string
	Manufacturer string
	Model        string
	ChipModel    string
	ChipCount    int
	CoreCount    int
	VidPid       string

	Port             *util.SerialDevice
	SerialNumber     string
	Frequency        uint
	Difficulty       float64
	ChipCountChecked uint
	ChipModelChecked string
	Status           string

	ChainChipID []byte // internal chips ids from chain
}

type AonData struct {
	FinishedWork []util.StratumWork
	PendingJob   []BMJob
	LastJobID    byte
	Mutex        sync.Mutex

	cancelReadThreads context.CancelFunc
	ctx               context.Context
}

type AonUserArgs struct {
	Frequency int
	JobTimer  int
	BaudRate  int
	HubVidPid string
}

type AonInfo struct {
	LastReport       time.Time
	WorkTimer        []time.Time
	WorkTotal        []uint64
	ConnectedDevices []*util.SerialDevice
	LastShareTime    time.Time
}

var AonDriver AonInfo
var AonUserConfig AonUserArgs
var ReadErrors []int

var (
	AonFinishedWorks []util.StratumWork
	AonPendingJob    [254][]BMJob
	AonJobMutex      sync.Mutex
	AonReadMutex     sync.Mutex
	AonStratumJob    [254]util.StratumJob
	AonHashRate      float64
	AonBestShare     float64
	AonWriteErrors   int
	AonReadErrors    int
	AonHwErrors      int
	AonErrorMutex    sync.Mutex
)

var (
	AONMINER = AonData{
		cancelReadThreads: nil,
		ctx:               context.Background(),
	}
)

// scan for compatible devices and connect to the specified ports, if no port
// is provided it will connect to all available devices. TODO: add VID/PID, Serial filtering args
func AsicScan(devicePorts []int) []*util.SerialDevice {
	//log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	//util.SerialContext.Close()
	var connectedDevices []*util.SerialDevice
	devices, infos, err := util.SerialScan(0x0403, 0x6015, "ZX01")
	if err != nil {
		util.AppLog("AONDRIVER", "serial scan error: "+err.Error(), "")
		// gousb OpenDevices can return devices even when there's an
		// error (e.g. one device failed while others opened fine).
		// Don't return early — process whatever devices we got.
	}
	if len(devices) == 0 {
		return connectedDevices
	}
	for i, info := range infos {
		util.AppLog("AONDRIVER", fmt.Sprintf("[%v] %s %s (Port: %d)", i, info.Manufacturer, info.ProductName, info.Port), "")
	}
	for i := range devices {
		if infos[i].ProductName == "ZX1 (USB Miner)" {
			device, err := util.SerialConnect(devices[i])
			if err != nil {
				util.AppLog("AONDRIVER", fmt.Sprintf("Failed to connect to device: %v", err), "")
				continue
			}
			util.AppLog("AONDRIVER", fmt.Sprintf("Connected to: %s %s", device.Info.Manufacturer, device.Info.ProductName), "")
			util.SerialFTDISetConfig(device.Device, 115200, 8, util.SerialConfig.STOPBITS1, util.SerialConfig.PARITYNONE)
			connectedDevices = append(connectedDevices, &device)
			time.Sleep(time.Millisecond * 100)
		}
	}
	return connectedDevices
}

func AsicReadAll(poolDifficulty *float64) {
	// initialize timers and reader
	AonDriver.LastShareTime = time.Now()
	AonDriver.WorkTimer = make([]time.Time, len(AonDriver.ConnectedDevices))
	AonDriver.WorkTotal = make([]uint64, len(AonDriver.ConnectedDevices))
	ReadErrors = make([]int, len(AonDriver.ConnectedDevices))
	for devID := range AonDriver.ConnectedDevices {
		AonDriver.WorkTimer[devID] = time.Now()
		AonDriver.WorkTotal[devID] = 0
	}
	AonDriver.LastReport = time.Now()

	for devID, device := range AonDriver.ConnectedDevices {
		go AsicReadJob(devID, device, poolDifficulty)
	}
}

func AsicInit(device *util.SerialDevice, initialized *int) {

	frequencyTarget := int(math.Round(float64(AonUserConfig.Frequency) / 6.25))

	if frequencyTarget > 128 {
		util.AppLog("BITMAIN", fmt.Sprintf("Frequency is too high! New frequency is 800Mhz\n"), "")
		frequencyTarget = 128
	}
	if frequencyTarget < 24 {
		util.AppLog("BITMAIN", fmt.Sprintf("Frequency is too low! New frequency is 150Mhz\n"), "")
		frequencyTarget = 24
	}

	if active, _ := util.SerialFTDIToggleRTS(device); active {
		util.AppLog("BITMAIN", fmt.Sprintf("Toggling RTS of the device port %d\n", device.Info.Port), "")
		time.Sleep(500 * time.Millisecond)
		util.SerialFTDIToggleRTS(device)
		time.Sleep(500 * time.Millisecond)
	} else {
		time.Sleep(500 * time.Millisecond)
		util.SerialFTDIToggleRTS(device)
		time.Sleep(500 * time.Millisecond)
		util.SerialFTDIToggleRTS(device)
		time.Sleep(500 * time.Millisecond)
	}

	c := components.BM1368
	initCommands := [][]byte{
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_READ, 0x05, 0x00, c.CHIP_ADDR},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_READ, 0x05, 0x00, c.CHIP_ADDR},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_INACTIVE, 0x05, 0x00, c.CHIP_ADDR},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_INACTIVE, 0x05, 0x00, c.CHIP_ADDR},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_INACTIVE, 0x05, 0x00, c.CHIP_ADDR},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_SINGLE | c.CMD_SETADDRESS, 0x05, 0x00, c.CHIP_ADDR},

		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.VERSION_MASK, 0x90, 0x00, 0xFF, 0xFF},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.VERSION_MASK, 0x90, 0x00, 0xFF, 0xFF},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.VERSION_MASK, 0x90, 0x00, 0xFF, 0xFF},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.VERSION_MASK, 0x90, 0x00, 0xFF, 0xFF},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.REG_A8, 0x00, 0x07, 0x00, 0x00},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.MISC_CONTROL, 0xFF, 0x0F, 0xC1, 0x00},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.CORE_REGISTER_CONTROL, 0x80, 0x00, 0x8B, 0x00},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.CORE_REGISTER_CONTROL, 0x80, 0x00, 0x80, 0x18},

		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.TICKET_MASK, 0x00, 0x00, 0x00, 0xFF},

		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.DRIVE_STRENGTH, 0x02, 0x11, 0x11, 0x11},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_SINGLE | c.CMD_WRITE, 0x09, 0x00, c.REG_A8, 0x00, 0x07, 0x00, 0x00},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_SINGLE | c.CMD_WRITE, 0x09, 0x00, c.MISC_CONTROL, 0xF0, 0x00, 0xC1, 0x00},

		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_SINGLE | c.CMD_WRITE, 0x09, 0x00, c.CORE_REGISTER_CONTROL, 0x80, 0x00, 0x8B, 0x00},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_SINGLE | c.CMD_WRITE, 0x09, 0x00, c.CORE_REGISTER_CONTROL, 0x80, 0x00, 0x80, 0x18},
		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_SINGLE | c.CMD_WRITE, 0x09, 0x00, c.CORE_REGISTER_CONTROL, 0x80, 0x00, 0x82, 0xAA},

		{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.ANALOG_MUX_CONTROL, 0x00, 0x00, 0x00, 0x03},
	}
	util.AppLog("BITMAIN", fmt.Sprintf("Setting all frequency to %.2fMhz", 6.25*float64(frequencyTarget)), "")
	for freq := 10; freq <= frequencyTarget; freq++ {
		initCommands = append(initCommands, calculatePLL(6.25*float64(freq), 96, 198, c))
	}
	initCommands = append(initCommands, []byte{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.ROLLING_RANGE, 0x00, 0x00, 0x15, 0xA4})

	initCommands = append(initCommands, []byte{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.VERSION_MASK, 0x90, 0x00, 0xFF, 0xFF})
	if AonUserConfig.BaudRate == 1 {
		initCommands = append(initCommands, []byte{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.FAST_UART_CONFIGURATION, 0x11, 0x30, 0x02, 0x00})
	}
	if AonUserConfig.BaudRate == 2 {
		initCommands = append(initCommands, []byte{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.FAST_UART_CONFIGURATION, 0x11, 0x30, 0x01, 0x00})
	}
	// Execute init sequence
	chipDetected := false
	for _, cmd := range initCommands {
		crc := util.CRC5_BM(cmd[2:])
		cmd = append(cmd, crc)

		util.SerialWrite(device, cmd)

		//log.Printf("AsicInit() sent %d bytes\n", n)

		// read (remove later, this is just for testing)
		buf, err := util.SerialRead(device, 64, time.Millisecond*100)
		if err != nil {
			//log.Printf("read error: %v", err)
		} else {
			//log.Printf("AsicInit() read %d bytes\n", len(buf))
			if len(buf) > 4 && chipDetected == false {
				if buf[2] == 0x13 && buf[3] == 0x68 {
					util.AppLog("AONMINER", fmt.Sprintf("BM1368 chip detected on device port %d", device.Info.Port), "")
					device.Info.ChipModel = "BM1368"
				}
				chipDetected = true
			}
		}

		time.Sleep(300 * time.Millisecond)
	}

	if AonUserConfig.BaudRate == 1 {
		util.SerialFTDISetConfig(device.Device, 1000000, 8, util.SerialConfig.STOPBITS1, util.SerialConfig.PARITYNONE)
		util.AppLog("BITMAIN", "ASIC Baudrate was set to 1M", "")
	}
	if AonUserConfig.BaudRate == 2 {
		util.SerialFTDISetConfig(device.Device, 1500000, 8, util.SerialConfig.STOPBITS1, util.SerialConfig.PARITYNONE)
		util.AppLog("BITMAIN", "ASIC Baudrate was set to 1.5M", "")
	}

	util.AppLog("BITMAIN", "Asic initialized, waiting 2 seconds...", "")
	time.Sleep(2000 * time.Millisecond)
	(*initialized)++

	return
}

func AsicSendJob(devid int, device *util.SerialDevice, stratumJob util.StratumJob, poolDifficulty *float64) {
	chipModel := device.Info.ChipModel
	if chipModel == "" {
		chipModel = "BM1368" // default for legacy devices without detection
	}
	req := components.ChipByName(chipModel)

	asicJob := BmConstructJob(AonDevice{ChipModel: chipModel}, stratumJob)

	data := []byte{
		req.PREAMBLE[0],
		req.PREAMBLE[1],
		req.TYPE_JOB | req.GROUP_SINGLE | req.CMD_WRITE,
		0x36,
		asicJob.JobID,
		0x04,
	}
	data = append(data, asicJob.StartingNonce...)
	data = append(data, asicJob.NetworkTarget...)
	data = append(data, asicJob.BlockTimestamp...)
	data = append(data, asicJob.MerkleRoot...)
	data = append(data, asicJob.PrevBlockHash...)
	data = append(data, asicJob.Version...)
	crc := util.CRC16False(data[2:])
	data = append(data, byte((crc>>8)&0xFF), byte(crc&0xFF))
	//log.Printf("%# x", data)
	_, err := util.SerialWrite(device, data)
	if err != nil {
		util.AppLog("AONDRIVER", fmt.Sprintf("write error: %v", err), "")
		AonJobMutex.Lock()
		AonWriteErrors++
		if AonWriteErrors > 5 {
			if len(AonDriver.ConnectedDevices) > 0 {
				for i := range AonDriver.ConnectedDevices {
					if AonDriver.ConnectedDevices[i].Device != nil {
						AonDriver.ConnectedDevices[i].Device.Close()
					}
					if AonDriver.ConnectedDevices[i].Interface != nil {
						AonDriver.ConnectedDevices[i].Interface.Close()
					}
				}
				AonDriver.ConnectedDevices = nil
				AonJobMutex.Unlock()
				return
			}
		}
		AonJobMutex.Unlock()
	} else {
		AonWriteErrors = 0
	}
	// add to pending jobs
	AonJobMutex.Lock()
	defer AonJobMutex.Unlock()
	asicJob.StratumJob = stratumJob
	AonPendingJob[devid] = append(AonPendingJob[devid], asicJob)
	AonStratumJob[devid] = stratumJob
}

func AsicReadJob(devid int, device *util.SerialDevice, poolDifficulty *float64) {
	for {

		if len(AonDriver.ConnectedDevices) == 0 {
			return
		}

		time.Sleep(10 * time.Millisecond)

		chipModel := device.Info.ChipModel
		if chipModel == "" {
			chipModel = "BM1368"
		}
		req := components.ChipByName(chipModel)
		AonJobMutex.Lock()
		stratumJob := AonStratumJob[devid]
		AonJobMutex.Unlock()

		var buf []byte
		buf, err := util.SerialRead(device, 11, 10*time.Millisecond)
		if err != nil {
			//log.Printf("read error: %v\n %# x", err, buf)
			ReadErrors[devid]++
			if ReadErrors[devid] > 10 {
				timeStart := time.Now()
				success := util.SerialUsbReset(device)
				if success {
					util.AppLog("AONDRIVER", fmt.Sprintf("USB Port %d is unstable (RT=%dms)",
						device.Info.Port, time.Since(timeStart).Milliseconds()),
						"",
					)
				} else {
					util.AppLog("AONDRIVER", fmt.Sprintf("Device %s usb dropped (RT=%dms)",
						device.Info.SerialNumber, time.Since(timeStart).Milliseconds()),
						"",
					)
					// close the serial device
					AonJobMutex.Lock()
					if len(AonDriver.ConnectedDevices) > 0 {
						for i := range AonDriver.ConnectedDevices {
							if AonDriver.ConnectedDevices[i].Device != nil {
								AonDriver.ConnectedDevices[i].Device.Close()
							}
							if AonDriver.ConnectedDevices[i].Interface != nil {
								AonDriver.ConnectedDevices[i].Interface.Close()
							}
							//util.SerialContext.Close()
							//util.SerialContext = nil
						}
						AonDriver.ConnectedDevices = nil
					}
					AonJobMutex.Unlock()
					return
				}
			}
		} else {
			ReadErrors[devid] = 0
		}

		if time.Since(AonDriver.LastShareTime) > time.Second*120 {
			AonJobMutex.Lock()
			util.AppLog("AONMINER", "Bad power! Device(s) stopped responding.", "")
			if len(AonDriver.ConnectedDevices) > 0 {
				for i := range AonDriver.ConnectedDevices {
					if AonDriver.ConnectedDevices[i].Device != nil {
						AonDriver.ConnectedDevices[i].Device.Close()
					}
					if AonDriver.ConnectedDevices[i].Interface != nil {
						AonDriver.ConnectedDevices[i].Interface.Close()
					}
					//util.SerialContext.Close()
					//util.SerialContext = nil
				}
				AonDriver.ConnectedDevices = nil
			}
			AonJobMutex.Unlock()
			return
		}

		if len(buf) < 12 {
			padding := make([]byte, 12-len(buf))
			buf = append(buf, padding...)
		}

		//print response buf
		//if buf[0] != 0x00 {
		//	util.AppLog("AONMINER", fmt.Sprintf("ASIC Port %d Read %d bytes: %# x\n", device.Info.Port, len(buf), buf), "")
		//}
		if len(buf) >= 12 && buf[0] != 0x00 {
			i := 0 // used to roll through buffer but was deemed unnecessary after testing
			if buf[i] == req.PREAMBLE[1] && buf[i+1] == req.PREAMBLE[0] {
				// Asic can send old work of previous job so we need to check before adding to AonFinishedWorks
				workID := uint8((buf[i+7] & 0xf0) >> 1)
				for _, pendingJob := range AonPendingJob[devid] {
					if workID == uint8(pendingJob.JobID) {
						nonce, version, _ := bmDecodeWork(chipModel, buf[i:])
						//fmt.Printf("Decoded asic work: jobID %d, nonce %s, version %s, asicID %d\n", workID, hex.EncodeToString(nonce), hex.EncodeToString(version), devid)
						diff := util.SHA256dValidator(pendingJob.StratumJob, nonce, version)
						//fmt.Printf("Nonce difficulty: %.0f\n", diff)
						if diff < 1 {
							AonHwErrors += 255
						}
						if diff >= 255 {
							AonDriver.WorkTotal[devid] += 255
							AonDriver.LastShareTime = time.Now()
							//log.Printf("Nonce difficulty: %.0f, nonce %s, version %s, asicID %d\n", diff, hex.EncodeToString(nonce), hex.EncodeToString(version), devid)
						}
						if diff >= *poolDifficulty {
							blockVersion := binary.BigEndian.Uint32(util.StringToHex(pendingJob.StratumJob.BlockVersion))
							rolledVersion := binary.BigEndian.Uint32(version)
							xorVersion := util.Uint32ToBytes(blockVersion ^ rolledVersion)
							work := util.StratumWork{
								ID:             stratumJob.ID,
								WorkID:         hex.EncodeToString([]byte{workID, uint8(devid)}),
								Hash:           "",
								Nonce:          hex.EncodeToString(nonce),
								Difficulty:     diff,
								Version:        hex.EncodeToString(xorVersion),
								ExtraNonce2:    pendingJob.StratumJob.ExtraNonce2,
								BlockTimestamp: pendingJob.StratumJob.BlockTimestamp,
							}
							AonJobMutex.Lock()
							AonFinishedWorks = append(AonFinishedWorks, work)
							AonJobMutex.Unlock()
						}
					}
				}

			}
		}

		if len(AonPendingJob[devid]) > 14 {
			AonJobMutex.Lock()
			AonPendingJob[devid] = AonPendingJob[devid][len(AonPendingJob[devid])-14:]
			AonJobMutex.Unlock()
		}

		//time.Sleep(20 * time.Millisecond)
		totalHashrate := float64(0)
		AonJobMutex.Lock()
		if time.Since(AonDriver.LastReport) >= time.Second*120 {
			fmt.Printf("--------------------------------------------------------\n")
			factor := 4294967296.0
			for devID, device := range AonDriver.ConnectedDevices {
				seconds := time.Since(AonDriver.WorkTimer[devID]).Seconds()
				if seconds <= 0 {
					seconds = 1
				}
				serialNumber := device.Info.SerialNumber
				for len(serialNumber) < 10 {
					serialNumber += "0"
				}

				hashrate := float64(AonDriver.WorkTotal[devID]) * factor / seconds / 1e9
				fmt.Printf(" %s (%s) Port %d: %.2f GH/s (Avg)\n", device.Info.ProductName, serialNumber[0:10], device.Info.Port, hashrate)
				totalHashrate += hashrate
			}
			// format total hashrate from G up to T
			formatedHashrate := ""
			if totalHashrate >= 1000 {
				formatedHashrate = fmt.Sprintf("%.3fTH", totalHashrate/1000)
			} else {
				formatedHashrate = fmt.Sprintf("%.2fGH", totalHashrate)
			}
			fmt.Printf("--------------------------------------------------------\n")
			fmt.Printf(" Hashrate: %s/s               Best Share: %s\n", formatedHashrate, util.FormatDiff(AonBestShare))
			fmt.Printf("--------------------------------------------------------\n")
			AonDriver.LastReport = time.Now()
		}
		AonJobMutex.Unlock()
	}
}

// convert stratumjob to bmJob
func BmConstructJob(device AonDevice, stratumJob util.StratumJob) BMJob {
	var job BMJob
	AONMINER.LastJobID = (AONMINER.LastJobID + 24) % 128
	if device.ChipModel == "BM1368" {
		// bm1368 have integrated midstate generation, enabled on init sequence
		job = BMJob{
			MidstateID:     0x00, // not used
			JobID:          AONMINER.LastJobID,
			MidstatesCount: 0x01, // there's no perceptive advantage when increased
			StartingNonce:  []byte{0x00, 0x00, 0x00, 0x04},
			NetworkTarget:  util.ReverseBytes(util.StringToHex(stratumJob.NetworkTarget)),
			BlockTimestamp: util.ReverseBytes(util.StringToHex(stratumJob.BlockTimestamp)),
			MerkleRoot:     util.ReverseBytes(util.StringToHex(stratumJob.MerkleRoot)),
			PrevBlockHash:  util.ReverseBytes(util.StringToHex(stratumJob.PrevBlockHash)),
			Version:        util.ReverseBytes(util.StringToHex(stratumJob.BlockVersion)),
		}
		merkleRoot, _ := util.ReverseWord32(stratumJob.MerkleRoot)
		//prevBlockHash, _ := util.ReverseWord32(stratumJob.PrevBlockHash)
		job.MerkleRoot = util.ReverseBytes(util.StringToHex(merkleRoot))
		//job.PrevBlockHash = util.ReverseBytes(util.StringToHex(prevBlockHash))
		// ready :)
	}

	return job
}

// decode job responses from Bitmain chips
func bmDecodeWork(chipModel string, data []byte) (nonce []byte, version []byte, coreID uint) {
	switch chipModel {
	case "BM1368":
		// strip out jobID, version and nonce from data
		jobID := (data[7] & 0xf0) >> 1
		versionBytes := []byte{data[8], data[9]}
		nonceBytes := data[2:6]

		// BM chips hides core group and core id inside nonce and jobID
		nonceValue := binary.LittleEndian.Uint32(nonceBytes)
		coreGroup := uint8((nonceValue >> 25) & 0x7f)
		coreID := jobID & 0x0f

		versionValue := binary.BigEndian.Uint16(versionBytes)
		versionBits := uint32(versionValue) << 13

		version = util.PadLeft((0x20000000 | versionBits), 4)
		nonce = util.ReverseBytes(util.PadLeft(binary.BigEndian.Uint32(nonceBytes), 4))
		coreID = coreGroup*15 + coreID // not so sure about 15 as the total cores in group
	default:
		return nil, nil, 0
	}
	return nonce, version, coreID
}

// --- PLL calculation (moved from drivers/utils.go) ---

const (
	pllFreqMult = 25.0
	pllEpsilon  = 0.0001
)

// PLLParams holds the PLL parameters
type PLLParams struct {
	FBDivider  uint8
	RefDiv     uint8
	PostDiv1   uint8
	PostDiv2   uint8
	ActualFreq float64
}

// pllGetParameters2 finds optimal PLL parameters for target frequency
func pllGetParameters2(targetFreq float64, fbDividerMin, fbDividerMax uint16) PLLParams {
	var bestFreq float64
	var bestRefDiv, bestFBDivider, bestPostDiv1, bestPostDiv2 uint8
	minDiff := math.MaxFloat64
	minVCOFreq := math.MaxFloat64
	minPostDiv := uint16(math.MaxUint16)

	for refDiv := uint8(2); refDiv > 0; refDiv-- {
		for postDiv1 := uint8(7); postDiv1 > 0; postDiv1-- {
			for postDiv2 := uint8(7); postDiv2 > 0; postDiv2-- {
				divider := uint16(refDiv) * uint16(postDiv1) * uint16(postDiv2)
				fbDivider := uint16(math.Round(targetFreq / pllFreqMult * float64(divider)))

				if postDiv1 > postDiv2 &&
					fbDivider >= fbDividerMin && fbDivider <= fbDividerMax {

					newFreq := pllFreqMult * float64(fbDivider) / float64(divider)
					currDiff := math.Abs(targetFreq - newFreq)
					vcoFreq := pllFreqMult * float64(fbDivider) / float64(refDiv)

					// Prioritize: closest frequency, then lowest VCO, then lowest postdiv product
					if currDiff < minDiff ||
						(math.Abs(currDiff-minDiff) < pllEpsilon && vcoFreq < minVCOFreq) ||
						(math.Abs(currDiff-minDiff) < pllEpsilon && math.Abs(vcoFreq-minVCOFreq) < pllEpsilon && uint16(postDiv1)*uint16(postDiv2) < minPostDiv) {

						minDiff = currDiff
						minVCOFreq = vcoFreq
						minPostDiv = uint16(postDiv1) * uint16(postDiv2)
						bestFreq = newFreq
						bestRefDiv = refDiv
						bestFBDivider = uint8(fbDivider)
						bestPostDiv1 = postDiv1
						bestPostDiv2 = postDiv2
					}
				}
			}
		}
	}

	return PLLParams{
		FBDivider:  bestFBDivider,
		RefDiv:     bestRefDiv,
		PostDiv1:   bestPostDiv1,
		PostDiv2:   bestPostDiv2,
		ActualFreq: bestFreq,
	}
}

// calculatePLL computes a PLL configuration command for the given target frequency.
func calculatePLL(targetFreq float64, fbDivMin, fbDivMax uint16, c components.BM13xxRequest) []uint8 {
	params := pllGetParameters2(targetFreq, fbDivMin, fbDivMax)

	var vdoScale uint8
	if float64(params.FBDivider)*pllFreqMult/float64(params.RefDiv) >= 2400 {
		vdoScale = 0x50
	} else {
		vdoScale = 0x40
	}

	postDiv := (((params.PostDiv1 - 1) & 0xf) << 4) | ((params.PostDiv2 - 1) & 0xf)
	return []uint8{c.PREAMBLE[0], c.PREAMBLE[1], c.TYPE_CMD | c.GROUP_ALL | c.CMD_WRITE, 0x09, 0x00, c.PLL0_PARAMETER, vdoScale, params.FBDivider, params.RefDiv, postDiv}
}
