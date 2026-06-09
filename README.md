# SMMiner

SMMiner is a Bitcoin mining client written in Go, targeting **AonMiner ZX1 USB ASIC** devices (based on BM1368). It uses the Stratum protocol to connect to mining pools.

## Hardware & Dependencies
- AonMiners ZX1 USB ASIC (FTDI 0403:6015)
- [usbreset](https://github.com/jkulesza/usbreset) (Linux, for USB hub auto-reset, also included in usbutils Ubuntu APT)
- libusb
- Zadig (Windows, for WinUSB or libusbK drivers)

## Quick Start

```bash
# Build (Linux/MacOS)
go build -o smminer

# Build (Windows on Msys2-Mingw)
CGO_ENABLED=1 go build -ldflags '-linkmode external -extldflags "-static"' .

# Run (solo mining on ckpool)
./smminer -o "stratum+tcp://solo.ckpool.org:3333" -u "YOUR_BTC_ADDRESS" -p "x"
```

For a complete example with USB hub reset support, see [`automine.sh`](automine.sh).

## Linux udev Setup

Run once to allow non-root USB access:

```bash
sudo bash miner-rules.sh
```

Then unplug and replug your devices.

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-o` | *(required)* | Pool URL (with or without `stratum+tcp://`) |
| `-u` | *(required)* | Pool worker name / BTC address |
| `-p` | `x` | Pool password |
| `--aon-frequency` | `150` | ASIC frequency in MHz |
| `--aon-job-timer` | `20` | ASIC job timing in seconds |
| `--suggest-diff` | `500` | Suggested pool difficulty |
| `--aon-baudrate` | `1` | Baud rate: `1` = 1M, `2` = 1.5M |
| `--aon-usb-hub` | `1234:0001` | USB Hub VID:PID for hard resets |
| `--version` | `false` | Print version and exit |

## Safety Warnings

- **Always stop with `CTRL+C`** — never force-close the terminal window.
- If the miner is force-closed, the ASIC may lock up. Disconnect and reconnect the USB cable to recover.
- The miner may delay shutdown during critical operations — wait for it to finish.

## Project Structure

```
smminer/
├── smminer.go          # Entry point, CLI parsing, main loop
├── stratum.go          # Stratum protocol (pool connection, job handling, submission)
├── miner.go            # Miner controllers (AonMiner, CPUMiner, MinerReceiver)
├── drivers/
│   ├── aonminer.go     # AonMiner ASIC driver (init, job dispatch, read loop)
│   ├── cpu.go          # CPU miner (for testing, enable via CPU_MODE constant in smminer.go)
│   └── components/     # BM13xx chip definitions
│       ├── bm13xx.go   # Common chip interface + ChipByName lookup
│       └── bm1368.go   # BM1368 chip constants
├── util/
│   ├── general.go      # Hex/bit utilities, format helpers, SHA256d validator
│   ├── sha256.go       # SHA256 implementation and midstate computation
│   ├── cli.go          # Logging and CLI helpers
│   └── usb.go          # USB serial communication (FTDI)
├── automine.sh         # Example mining script
├── miner-rules.sh      # Linux udev rules setup
├── go.mod
├── go.sum
└── LICENSE
```

Requires Go 1.24+.

## License

MIT — see [LICENSE](LICENSE).
