package util

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/gousb"
	"go.bug.st/serial/enumerator"
)

const (
	// FTDI requests
	sioSetBaud = 0x03
	sioSetData = 0x04
	// FTDI modem control
	sioSetModemCtrl = 0x01
	sioRTSCTS_HS    = (0x1 << 8)
	sioDTRDSR_HS    = (0x2 << 8)
	sioXONXOFF_HS   = (0x4 << 8)
	sioSetRTS       = 0x2
	sioClrRTS       = 0x0
	// bmRequestType: Host to Device, Vendor, Device
	requestOut = gousb.ControlOut | gousb.ControlVendor | gousb.ControlDevice
)

// map of common baud rates for FTDI FT230X (wValue,wIndex)
var baudDivisors = map[int][2]uint16{
	9600:    {0x4138, 0x0000},
	19200:   {0x809C, 0x0000},
	38400:   {0xC04E, 0x0000},
	57600:   {0x0034, 0x0000},
	115200:  {0x001A, 0x0000},
	230400:  {0x000D, 0x0000},
	460800:  {0x4006, 0x0000}, // bad
	921600:  {0x8003, 0x0000}, // bad
	1000000: {0x0003, 0x0000},
	1500000: {0x0002, 0x0000},
	3000000: {0x0001, 0x0000},
}

type SerialCfg struct {
	STOPBITS1  uint16
	STOPBITS2  uint16
	PARITYNONE uint16
	PARITYODD  uint16
	PARITYEVEN uint16
}

var SerialConfig = SerialCfg{
	STOPBITS1:  0,
	STOPBITS2:  1,
	PARITYNONE: 0,
	PARITYODD:  1,
	PARITYEVEN: 2,
}

type SerialDevice struct {
	Device       *gousb.Device
	Interface    *gousb.Interface
	EndpointIn   *gousb.InEndpoint
	EndpointOut  *gousb.OutEndpoint
	PinStatusRTS uint16
	Info         SerialInfo
	MutexWrite   sync.Mutex
	MutexRead    sync.Mutex
	Close        func()
}

type SerialInfo struct {
	Manufacturer string
	ProductName  string
	SerialNumber string
	PacketSize   int
	UsingHub     bool
	Port         int
	Speed        float64
	ChipModel    string // detected ASIC chip model (e.g. "BM1368", "BM1397", "S97")
}

func SerialConnect(device *gousb.Device) (SerialDevice, error) {
	// Claim the default interface
	intf, done, err := device.DefaultInterface()
	if err != nil {
		return SerialDevice{}, fmt.Errorf("%s.DefaultInterface(): %v", device, err)
	}

	// Get the first IN and OUT bulk endpoints
	var epIn *gousb.InEndpoint
	var epOut *gousb.OutEndpoint
	for _, ep := range intf.Setting.Endpoints {
		if ep.TransferType == gousb.TransferTypeBulk {
			if ep.Direction == gousb.EndpointDirectionIn && epIn == nil {
				epIn, err = intf.InEndpoint(ep.Number)
				if err != nil {
					return SerialDevice{}, fmt.Errorf("Failed to open IN endpoint: %v", err)
				}
			} else if ep.Direction == gousb.EndpointDirectionOut && epOut == nil {
				epOut, err = intf.OutEndpoint(ep.Number)
				if err != nil {
					return SerialDevice{}, fmt.Errorf("Failed to open OUT endpoint: %v", err)
				}
			}
		}
	}

	return SerialDevice{
		Device:       device,
		Interface:    intf,
		EndpointIn:   epIn,
		EndpointOut:  epOut,
		PinStatusRTS: 0,
		Info:         serialGetInfo(device),
		Close:        done,
	}, nil
}

var SerialContext *gousb.Context

func SerialScan(vid, pid uint16, serialPrefix string) ([]*gousb.Device, []SerialInfo, error) {

	// Open all devices that match the VID/PID. Retry up to 3 times on
	// transient errors (e.g. device enumerating, driver settling).
	var devs []*gousb.Device
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if SerialContext == nil {
			SerialContext = gousb.NewContext()
		}
		devs, err = SerialContext.OpenDevices(func(desc *gousb.DeviceDesc) bool {
			return desc.Vendor == gousb.ID(vid) && desc.Product == gousb.ID(pid)
		})
		if err == nil || len(devs) > 0 {
			break
		}
		// Context may be in a bad state after error; close it so the
		// next attempt starts fresh with a new libusb_init().
		SerialContext.Close()
		SerialContext = nil
		time.Sleep(500 * time.Millisecond)
	}
	// Find non-busy devices
	var adevs []*gousb.Device
	var infos []SerialInfo
	for _, d := range devs {
		if err := d.SetAutoDetach(true); err != nil {
			d.Close()
			continue
		}
		if runtime.GOOS == "linux" && isPortBusy(serialGetInfo(d).SerialNumber) {
			continue
		}
		if len(serialGetInfo(d).SerialNumber) > 4 && serialGetInfo(d).SerialNumber[0:len(serialPrefix)] != serialPrefix {
			d.Close()
			continue
		}
		adevs = append(adevs, d)
		infos = append(infos, serialGetInfo(d))
	}

	return adevs, infos, err
}

// Toggle the RTS pin for the FTDI device (TRUE = LOW)
func SerialFTDIToggleRTS(device *SerialDevice) (bool, error) {
	if device.PinStatusRTS == 0 {
		// Set RTS high
		device.PinStatusRTS = 1
		wValue := uint16(sioSetRTS | (sioSetRTS << 8)) // Set RTS
		_, err := device.Device.Control(requestOut, sioSetModemCtrl, wValue, 0, nil)
		return true, err
	} else {
		// Set RTS low
		device.PinStatusRTS = 0
		wValue := uint16(sioClrRTS | (sioSetRTS << 8)) // Clear RTS
		_, err := device.Device.Control(requestOut, sioSetModemCtrl, wValue, 0, nil)
		return true, err
	}
}

func SerialWrite(device *SerialDevice, data []byte) (int, error) {
	device.MutexWrite.Lock()
	defer device.MutexWrite.Unlock()
	n, err := device.EndpointOut.Write(data)
	return n, err
}

func SerialRead(device *SerialDevice, dataLen, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	device.MutexRead.Lock()
	defer device.MutexRead.Unlock()
	var data []byte
	readBuf := make([]byte, dataLen+2)
	n, err := device.EndpointIn.ReadContext(ctx, readBuf)
	if n > 2 { // remove FTDI bytes
		data = readBuf[2:n]
	}
	if err != nil {
		if err == context.DeadlineExceeded {
			log.Printf("Read timed out occurred")
		}
		return data, err
	}
	return data, err
}

func serialGetInfo(device *gousb.Device) SerialInfo {
	manufacturer, _ := device.Manufacturer()
	product, _ := device.Product()
	serial, _ := device.SerialNumber()

	var speedMbps float64
	switch device.Desc.Speed {
	case gousb.SpeedLow:
		speedMbps = 1.5
	case gousb.SpeedFull:
		speedMbps = 12
	case gousb.SpeedHigh:
		speedMbps = 480
	case gousb.SpeedSuper:
		speedMbps = 5000
	default:
		speedMbps = 0
	}

	return SerialInfo{
		Manufacturer: manufacturer,
		ProductName:  product,
		SerialNumber: serial,
		PacketSize:   device.Desc.MaxControlPacketSize * 8,
		Port:         device.Desc.Port,
		Speed:        speedMbps,
		UsingHub:     (device.Desc.Bus > 0),
	}
}

func isPortBusy(serial string) bool {
	ports, _ := enumerator.GetDetailedPortsList()
	portName := ""
	for _, port := range ports {
		if port.IsUSB && port.SerialNumber == serial {
			portName = port.Name
		}
	}
	if len(portName) < 1 {
		return true
	}
	// solve problem with windows (COM10+)
	if runtime.GOOS == "windows" {
		if !strings.HasPrefix(portName, `\\.\`) && len(portName) > 4 {
			portName = `\\.\` + portName
		}
	}
	f, err := os.OpenFile(portName, os.O_RDWR, 0666)
	if err != nil {
		return true
	}
	f.Close()
	return false
}

func SerialFTDISetConfig(dev *gousb.Device, baudRate int, dataBits, stopBits, parity uint16) error {
	// set comunication settings
	wValue := dataBits | (stopBits << 11) | (parity << 8)
	_, err := dev.Control(requestOut, sioSetData, wValue, 0, nil)
	if err != nil {
		return err
	}
	// set baudrate
	div, ok := baudDivisors[baudRate]
	if !ok {
		return fmt.Errorf("unsupported baud rate %d", baudRate)
	}
	_, err = dev.Control(requestOut, sioSetBaud, div[0], div[1], nil)
	return err
}

// Set the latency timer to a FTDI device
func SerialFTDILatency(dev *gousb.Device, latency uint16) error {
	_, err := dev.Control(
		requestOut,
		0x09,    // bRequest: SIO_SET_LATENCY_TIMER_REQUEST
		latency, // wValue: latency
		0,       // wIndex: interface 0
		nil,     // no data
	)

	if err != nil {
		return fmt.Errorf("failed to set FTDI latency: %v", err)
	}

	return nil
}

// SerialUsbReset resets the FTDI device and reinitializes it by serial number.
func SerialUsbReset(serial *SerialDevice) bool {
	if serial == nil || serial.Device == nil {
		return false
	}

	serialNumber := serial.Info.SerialNumber
	if serialNumber == "" {
		return false
	}

	// Close interface and device first to free the configuration
	if serial.Interface != nil {
		defer func() {
			if r := recover(); r != nil {
				// silent panic, should not happen
			}
		}()
		serial.Interface.Close()
		serial.Interface = nil
	}
	if serial.Device != nil {
		serial.Device.Close()
		serial.Device = nil
	}

	ctx := gousb.NewContext()
	defer ctx.Close()

	// Loop until the device with the same serial number reappears
	var dev *gousb.Device
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		devices, _ := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
			return desc.Vendor == gousb.ID(0x0403) &&
				desc.Product == gousb.ID(0x6015)
		})

		for _, d := range devices {
			sn, _ := d.SerialNumber()
			if sn == serialNumber {
				// Keep this device, close all others
				for _, other := range devices {
					if other != d {
						other.Close()
					}
				}
				dev = d
				break
			}
			d.Close()
		}

		if dev != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if dev == nil {
		return false
	}

	// Reset the freshly opened device
	err := dev.Reset()
	if err != nil {
		dev.Close()
		return false
	}

	// Reclaim config/interface
	cfg, err := dev.Config(1)
	if err != nil {
		dev.Close()
		return false
	}
	intf, err := cfg.Interface(0, 0)
	if err != nil {
		dev.Close()
		return false
	}

	epIn, err := intf.InEndpoint(1)
	if err != nil {
		dev.Close()
		return false
	}

	epOut, err := intf.OutEndpoint(2)
	if err != nil {
		dev.Close()
		return false
	}

	serial.Device = dev
	serial.Interface = intf
	serial.EndpointIn = epIn
	serial.EndpointOut = epOut

	return true
}

// Hard reset ALL devices with a given VID:PID (udev rules or root needed, more optimal to usb hubs)
func UsbReset(vidpid string) error {
	// Parse VID:PID from input string
	parts := strings.Split(vidpid, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid VID:PID format, expected format is '1234:5678'")
	}
	vid := strings.TrimSpace(parts[0])
	pid := strings.TrimSpace(parts[1])

	if runtime.GOOS == "linux" {
		cmd := "lsusb -d " + vidpid + " | awk '{gsub(\":\", \"\", $4); print $2 \"/\" $4}'"

		out, err := exec.Command("sh", "-c", cmd).Output()
		if err != nil {
			return fmt.Errorf("failed to execute lsusb command.")
		}

		busDevice := strings.TrimSpace(string(out))
		if busDevice == "" {
			return fmt.Errorf("No USB device found with VID:PID %s", vidpid)
		}

		//fmt.Printf("Found USB device at: %s\n", busDevice)
		out, err = exec.Command("usbreset", busDevice).Output()
		if err != nil {
			return fmt.Errorf("failed to execute usbreset command.")
		}

		return nil

	} else if runtime.GOOS == "windows" {
		// PNPUTIL Syntax: pnputil /restart-device "USB\VID_0403&PID_6015\*"
		hwID := fmt.Sprintf(`USB\VID_%s&PID_%s`, vid, pid)
		cmd := exec.Command("pnputil", "/restart-device", hwID)
		out, err := cmd.CombinedOutput()
		outputStr := string(out)
		if err == nil && !strings.Contains(strings.ToLower(outputStr), "failed") && !strings.Contains(strings.ToLower(outputStr), "error") {
			AppLog("USB", fmt.Sprintf("pnputil restarted USB device %s", vidpid), "")
			return nil
		}

		// Fallback 1: try devcon if available
		devicePattern := fmt.Sprintf("*VID_%s&PID_%s*", vid, pid)
		cmd = exec.Command("devcon", "restart", devicePattern)
		out, err = cmd.CombinedOutput()
		outputStr = string(out)
		if err == nil && strings.Contains(strings.ToLower(outputStr), "restarted") {
			AppLog("USB", fmt.Sprintf("devcon restarted USB device %s", vidpid), "")
			return nil
		}

		// Fallback 2: PowerShell disable/enable cycle
		psCmd := fmt.Sprintf(
			`$d = Get-PnpDevice -InstanceId '%s*' -ErrorAction SilentlyContinue; if ($d) { $d | Disable-PnpDevice -Confirm:$false -ErrorAction SilentlyContinue; Start-Sleep -Seconds 2; $d | Enable-PnpDevice -Confirm:$false -ErrorAction SilentlyContinue }`,
			hwID,
		)
		cmd = exec.Command("powershell", "-NoProfile", "-Command", psCmd)
		out, err = cmd.CombinedOutput()
		if err == nil {
			AppLog("USB", fmt.Sprintf("PowerShell cycled USB device %s", vidpid), "")
			return nil
		}

		return fmt.Errorf("all USB reset methods failed (pnputil, devcon, powershell): %s", strings.TrimSpace(string(out)))
	}

	return fmt.Errorf("usbreset() is only supported on linux and windows")
}
