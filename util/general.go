package util

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"math/rand"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// copy of structs from stratum.go
type StratumJob struct {
	ID             string
	PrevBlockHash  string
	CoinbasePrefix string
	ExtraNonce1    string
	CoinbaseSuffix string
	MerkleBranches []string
	BlockVersion   string
	NetworkTarget  string
	PoolDifficulty string
	BlockTimestamp string
	ForgetSubmits  bool
	ExtraNonce2    string
	MerkleRoot     string
}

type StratumWork struct {
	ID             string
	WorkID         string
	Hash           string
	Difficulty     float64
	Nonce          string
	Version        string
	ExtraNonce2    string
	BlockTimestamp string
}

// generate uid in hex format
func GenerateUID() string {
	uid := rand.Intn(255)
	return hex.EncodeToString([]byte{byte(uid)})
}

func Ping(host string) int {
	cmdArgs := []string{host}

	// this avoids using administrator privileges on windows
	if runtime.GOOS != "windows" {
		cmdArgs = append([]string{"-c", "4"}, cmdArgs...)
	}

	pingCmd := exec.Command("ping", cmdArgs...)
	pingOutput, _ := pingCmd.CombinedOutput()

	re := regexp.MustCompile(`=(\d+\.?\d*)\s?ms`)
	matches := re.FindStringSubmatch(string(pingOutput))
	if len(matches) < 2 {
		return 0
	}

	timeMsFloat, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0
	}

	return int(timeMsFloat)
}

// convert number to hashrate scale
func FormatDiff(diff float64) string {
	if diff < 1000 {
		return strconv.FormatFloat(diff, 'f', 0, 64)
	}

	units := []string{"K", "M", "G", "T", "P", "E", "Z", "Y"}
	unitIndex := -1
	value := float64(diff)

	for value >= 1000 && unitIndex < len(units)-1 {
		value /= 1000
		unitIndex++
	}

	var s string
	if value == float64(uint64(value)) {
		// Whole number case 1000 → 1K
		s = fmt.Sprintf("%d", uint64(value))
	} else {
		// Decimal point case 1450 → 1.45K
		s = strconv.FormatFloat(value, 'f', -1, 64)
		// Trim to 2 decimal
		if dot := strings.Index(s, "."); dot != -1 && len(s) > dot+3 {
			s = s[:dot+3]
		}
		s = strings.TrimSuffix(s, ".00")
	}

	return s + units[unitIndex]
}

// Reverse byte order of a uint32
func ReverseUint32(x uint32) uint32 {
	return (x << 24) | ((x << 8) & 0x00FF0000) | ((x >> 8) & 0x0000FF00) | (x >> 24)
}

// Reverse byte order of a byte slice
func ReverseBytes(b []byte) []byte {
	reversed := make([]byte, len(b))
	for i := range b {
		reversed[i] = b[len(b)-1-i]
	}
	return reversed
}

// ReverseWord32 takes a 64-character hex string and reverses the byte order within
// each 4-byte and join at the result
func ReverseWord32(hash string) (string, error) {
	if len(hash) != 64 {
		return "", fmt.Errorf("hash must be 64 hex characters, got %d", len(hash))
	}

	// Validate that it's valid hex
	if _, err := hex.DecodeString(hash); err != nil {
		return "", fmt.Errorf("invalid hex string: %v", err)
	}

	var result strings.Builder
	result.Grow(64)
	// Process each 8-character (4-byte) chunk
	for i := 0; i < 64; i += 8 {
		chunk := hash[i : i+8]

		// Decode 8-character hex string into 4 bytes
		bytes, _ := hex.DecodeString(chunk)

		// Reverse the 4 bytes: little-endian byte order
		for j := 3; j >= 0; j-- {
			result.WriteString(fmt.Sprintf("%02x", bytes[j]))
		}
	}

	return result.String(), nil
}

// Reverse hex string (treating as hex bytes)
func ReverseHexString(s string) string {
	hexBytes, _ := hex.DecodeString(s)
	return hex.EncodeToString(ReverseBytes(hexBytes))
}

// Convert string to hex bytes
func StringToHex(s string) []byte {
	hexBytes, _ := hex.DecodeString(s)
	return hexBytes
}

// Convert uint32 to custom length of bytes
func PadLeft(s uint32, length int) []byte {
	b := make([]byte, length)
	for i := range length {
		b[length-1-i] = byte(s >> (i * 8))
	}
	return b
}

func BytesToFloat64(b []byte) float64 {
	i := new(big.Int).SetBytes(b)
	f, _ := new(big.Rat).SetInt(i).Float64()
	return f
}

// convert uint32 to bytes
func Uint32ToBytes(n uint32) []byte {
	b := make([]byte, 4)
	b[0] = byte(n >> 24)
	b[1] = byte(n >> 16)
	b[2] = byte(n >> 8)
	b[3] = byte(n)
	return b
}

func IncrementBitmask(value, mask uint32) uint32 {
	if mask == 0 {
		return value
	}
	carry := (value & mask) + (mask & -mask)
	overflow := carry & ^mask
	newValue := (value & ^mask) | (carry & mask)
	if overflow != 0 {
		carryMask := overflow << 1
		newValue = IncrementBitmask(newValue, carryMask)
	}
	return newValue
}

// Receives a stratumjob with custom nonce, version and a diff that
// the result must be above to be valid
func SHA256dValidator(job StratumJob, nonce []byte, version []byte) float64 {
	// Prepare header
	prevBlockReversed, _ := ReverseWord32(job.PrevBlockHash)

	header := make([]byte, 0, 80)
	header = append(header, ReverseBytes(version)...)                         // 4 bytes
	header = append(header, StringToHex(prevBlockReversed)...)                // 32 bytes
	header = append(header, StringToHex(job.MerkleRoot)...)                   // 32 bytes
	header = append(header, ReverseBytes(StringToHex(job.BlockTimestamp))...) // 4 bytes
	header = append(header, ReverseBytes(StringToHex(job.NetworkTarget))...)  // 4 bytes
	header = append(header, ReverseBytes(nonce)...)                           // 4 bytes
	hash := ReverseBytes(Sha256d(header))

	// Convert to float for comparison
	hashValue := BytesToFloat64(hash)

	// Check if hash is less than or equal to target
	return TARGET_DIFF_ONE / hashValue
}

// Modified CRC-5/USB with 0x13 polynomial. Normally used by Bitmain.
func CRC5_BM(data []byte) byte {
	crc := byte(0x1F)
	for _, b := range data {
		for i := 0; i < 8; i++ {
			bit := (b >> 7) & 1
			b <<= 1
			newBit := (crc >> 4) ^ bit
			newBit &= 1
			crc = ((crc << 1) | newBit) ^ (newBit << 2)
			crc &= 0x1F
		}
	}
	return crc
}

// extracted from cgminer
var crc16Table = [256]uint16{
	0x0000, 0x1021, 0x2042, 0x3063, 0x4084, 0x50A5, 0x60C6, 0x70E7,
	0x8108, 0x9129, 0xA14A, 0xB16B, 0xC18C, 0xD1AD, 0xE1CE, 0xF1EF,
	0x1231, 0x0210, 0x3273, 0x2252, 0x52B5, 0x4294, 0x72F7, 0x62D6,
	0x9339, 0x8318, 0xB37B, 0xA35A, 0xD3BD, 0xC39C, 0xF3FF, 0xE3DE,
	0x2462, 0x3443, 0x0420, 0x1401, 0x64E6, 0x74C7, 0x44A4, 0x5485,
	0xA56A, 0xB54B, 0x8528, 0x9509, 0xE5EE, 0xF5CF, 0xC5AC, 0xD58D,
	0x3653, 0x2672, 0x1611, 0x0630, 0x76D7, 0x66F6, 0x5695, 0x46B4,
	0xB75B, 0xA77A, 0x9719, 0x8738, 0xF7DF, 0xE7FE, 0xD79D, 0xC7BC,
	0x48C4, 0x58E5, 0x6886, 0x78A7, 0x0840, 0x1861, 0x2802, 0x3823,
	0xC9CC, 0xD9ED, 0xE98E, 0xF9AF, 0x8948, 0x9969, 0xA90A, 0xB92B,
	0x5AF5, 0x4AD4, 0x7AB7, 0x6A96, 0x1A71, 0x0A50, 0x3A33, 0x2A12,
	0xDBFD, 0xCBDC, 0xFBBF, 0xEB9E, 0x9B79, 0x8B58, 0xBB3B, 0xAB1A,
	0x6CA6, 0x7C87, 0x4CE4, 0x5CC5, 0x2C22, 0x3C03, 0x0C60, 0x1C41,
	0xEDAE, 0xFD8F, 0xCDEC, 0xDDCD, 0xAD2A, 0xBD0B, 0x8D68, 0x9D49,
	0x7E97, 0x6EB6, 0x5ED5, 0x4EF4, 0x3E13, 0x2E32, 0x1E51, 0x0E70,
	0xFF9F, 0xEFBE, 0xDFDD, 0xCFFC, 0xBF1B, 0xAF3A, 0x9F59, 0x8F78,
	0x9188, 0x81A9, 0xB1CA, 0xA1EB, 0xD10C, 0xC12D, 0xF14E, 0xE16F,
	0x1080, 0x00A1, 0x30C2, 0x20E3, 0x5004, 0x4025, 0x7046, 0x6067,
	0x83B9, 0x9398, 0xA3FB, 0xB3DA, 0xC33D, 0xD31C, 0xE37F, 0xF35E,
	0x02B1, 0x1290, 0x22F3, 0x32D2, 0x4235, 0x5214, 0x6277, 0x7256,
	0xB5EA, 0xA5CB, 0x95A8, 0x8589, 0xF56E, 0xE54F, 0xD52C, 0xC50D,
	0x34E2, 0x24C3, 0x14A0, 0x0481, 0x7466, 0x6447, 0x5424, 0x4405,
	0xA7DB, 0xB7FA, 0x8799, 0x97B8, 0xE75F, 0xF77E, 0xC71D, 0xD73C,
	0x26D3, 0x36F2, 0x0691, 0x16B0, 0x6657, 0x7676, 0x4615, 0x5634,
	0xD94C, 0xC96D, 0xF90E, 0xE92F, 0x99C8, 0x89E9, 0xB98A, 0xA9AB,
	0x5844, 0x4865, 0x7806, 0x6827, 0x18C0, 0x08E1, 0x3882, 0x28A3,
	0xCB7D, 0xDB5C, 0xEB3F, 0xFB1E, 0x8BF9, 0x9BD8, 0xABBB, 0xBB9A,
	0x4A75, 0x5A54, 0x6A37, 0x7A16, 0x0AF1, 0x1AD0, 0x2AB3, 0x3A92,
	0xFD2E, 0xED0F, 0xDD6C, 0xCD4D, 0xBDAA, 0xAD8B, 0x9DE8, 0x8DC9,
	0x7C26, 0x6C07, 0x5C64, 0x4C45, 0x3CA2, 0x2C83, 0x1CE0, 0x0CC1,
	0xEF1F, 0xFF3E, 0xCF5D, 0xDF7C, 0xAF9B, 0xBFBA, 0x8FD9, 0x9FF8,
	0x6E17, 0x7E36, 0x4E55, 0x5E74, 0x2E93, 0x3EB2, 0x0ED1, 0x1EF0,
}

func CRC16(buffer []byte) uint16 {
	crc := uint16(0)
	for _, b := range buffer {
		crc = crc16Table[byte(crc>>8)^b] ^ (crc << 8)
	}
	return crc
}

func CRC16False(buffer []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range buffer {
		crc = crc16Table[byte(crc>>8)^b] ^ (crc << 8)
	}
	return crc
}
