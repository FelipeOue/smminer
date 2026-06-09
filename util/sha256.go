package util

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"strings"
)

// Target value that a bitcoin hash must be below to have at least 1 of difficulty.
const TARGET_DIFF_ONE = 0x00000000FFFF0000000000000000000000000000000000000000000000000000

// Default version mask of the stratum protocol (ckpool.conf since it's the most used pool source).
// The asic miner will roll non 0 bits from this mask.
const DEFAULT_VERSION_MASK = 0x1FFFE000

func ComputeMerkleRoot(prefix, extraNonce1Hex, extraNonce2Hex, suffix string, branches []string) (string, error) {
	// Construct coinbase transaction
	coinbaseHex := prefix + extraNonce1Hex + extraNonce2Hex + suffix
	coinbase, err := hex.DecodeString(coinbaseHex)
	if err != nil {
		return "", errors.New("invalid coinbase hex")
	}

	// Compute coinbase hash (Coinbase to "Hash000")
	leftSideBranch := Sha256d(coinbase)

	// Build the Merkle Root
	for _, branch := range branches {
		branchBytes, err := hex.DecodeString(branch)
		if err != nil {
			return "", err
		}
		leftSideBranch = append(leftSideBranch, branchBytes...)
		leftSideBranch = Sha256d(leftSideBranch)
	}

	return hex.EncodeToString(leftSideBranch), err
}

func Sha256d(data []byte) []byte {
	single := sha256.Sum256(data)
	double := sha256.Sum256(single[:])
	return double[:]
}

// Convert share diff to ticket mask for Bitmain chips
func DiffToTicketMask(diff float64) []byte {
	// Calculate number of bits to set (k ≈ log2(difficulty))
	k := math.Ceil(math.Log2(diff))
	if k > 32 {
		k = 32
	}
	if k < 0 {
		k = 0
	}

	mask := uint32((1 << uint(k)) - 1)

	// Convert uint32 to 4 bytes (big-endian)
	result := make([]byte, 4)
	result[0] = byte((mask >> 24) & 0xFF)
	result[1] = byte((mask >> 16) & 0xFF)
	result[2] = byte((mask >> 8) & 0xFF)
	result[3] = byte(mask & 0xFF)

	return result
}

// Convert the provided versionmask to 2 bytes mask for
// Bitmain chips
func GetVersionBytes(versionMask uint32) []byte {
	return []byte{
		byte(((versionMask >> 13) >> 8)),
		byte(((versionMask >> 13) & 0xFF)),
	}
}

// Convert diff to target
func DiffToTarget(diff float64) float64 {
	return 0x00000000FFFF0000000000000000000000000000000000000000000000000000 / diff
}

// Rebuild the hash from a block header inputs and nonce
func RebuildHash(version, prevblock, merkleroot, time, bits, nonce string) string {
	prevblockReversed, _ := ReverseWord32(prevblock)
	var nonceInt uint32 = binary.BigEndian.Uint32(StringToHex(nonce))
	blockHeader := strings.Join([]string{
		ReverseHexString(version),
		prevblockReversed,
		merkleroot,
		ReverseHexString(time),
		ReverseHexString(bits),
	}, "")
	hexBlockHeader, _ := hex.DecodeString(blockHeader)
	blockHeaderWithNonce := append(hexBlockHeader, ReverseBytes(PadLeft(nonceInt, 4))...)
	return hex.EncodeToString(ReverseBytes(Sha256d(blockHeaderWithNonce)))
}

// TEST: Check if the ticket mask matches the conditions to be valid
func CheckTicketMask(reversedHash [32]byte, reversedTicketMask uint32) bool {
	// Condition 1: First 4 bytes == 0
	cond1 := binary.BigEndian.Uint32(reversedHash[0:4]) == 0

	// Condition 2: (bytes[4:7] & reversed_ticket_mask) == 0
	segment := binary.BigEndian.Uint32(reversedHash[4:8])
	cond2 := (segment & reversedTicketMask) == 0

	return cond1 && cond2
}

// SHA256 constants (first 32 bits of fractional parts of cube roots of first 64 primes)
var k = []uint32{
	0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
	0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
	0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
	0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
	0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
	0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
	0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
	0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
}

// SHA256State represents the internal state of SHA256
type SHA256State struct {
	H [8]uint32 // Hash values
}

// rightRotate performs right rotation
func rightRotate(x uint32, n uint) uint32 {
	return (x >> n) | (x << (32 - n))
}

func ch(x, y, z uint32) uint32 {
	return (x & y) ^ (^x & z)
}

func maj(x, y, z uint32) uint32 {
	return (x & y) ^ (x & z) ^ (y & z)
}

// Σ0 function
func sigma0(x uint32) uint32 {
	return rightRotate(x, 2) ^ rightRotate(x, 13) ^ rightRotate(x, 22)
}

// Σ1 function
func sigma1(x uint32) uint32 {
	return rightRotate(x, 6) ^ rightRotate(x, 11) ^ rightRotate(x, 25)
}

// σ0 function
func gamma0(x uint32) uint32 {
	return rightRotate(x, 7) ^ rightRotate(x, 18) ^ (x >> 3)
}

// σ1 function
func gamma1(x uint32) uint32 {
	return rightRotate(x, 17) ^ rightRotate(x, 19) ^ (x >> 10)
}

// processBlock processes a single 512-bit block
func processBlock(state *SHA256State, block []byte) {
	// Prepare message schedule
	w := make([]uint32, 64)

	// Copy first 16 words from block
	for i := 0; i < 16; i++ {
		w[i] = binary.BigEndian.Uint32(block[i*4 : (i+1)*4])
	}

	// Extend the message schedule
	for i := 16; i < 64; i++ {
		w[i] = gamma1(w[i-2]) + w[i-7] + gamma0(w[i-15]) + w[i-16]
	}

	// Initialize working variables
	a, b, c, d, e, f, g, h := state.H[0], state.H[1], state.H[2], state.H[3],
		state.H[4], state.H[5], state.H[6], state.H[7]

	// Main loop
	for i := 0; i < 64; i++ {
		t1 := h + sigma1(e) + ch(e, f, g) + k[i] + w[i]
		t2 := sigma0(a) + maj(a, b, c)
		h = g
		g = f
		f = e
		e = d + t1
		d = c
		c = b
		b = a
		a = t1 + t2
	}

	// Add compressed chunk to current hash value
	state.H[0] += a
	state.H[1] += b
	state.H[2] += c
	state.H[3] += d
	state.H[4] += e
	state.H[5] += f
	state.H[6] += g
	state.H[7] += h
}

// padMessage pads the message according to SHA256 specification
func padMessage(message []byte) []byte {
	msgLen := len(message)
	bitLen := uint64(msgLen) * 8

	// Calculate padding length
	// We need to pad to make the message length 448 (mod 512) bits
	// That's 56 (mod 64) bytes
	padLen := 64 - (msgLen+9)%64
	if padLen == 64 {
		padLen = 0
	}

	// Create padded message
	paddedLen := msgLen + 1 + padLen + 8
	padded := make([]byte, paddedLen)

	// Copy original message
	copy(padded, message)

	// Add 0x80 byte
	padded[msgLen] = 0x80

	// Add bit length as 64-bit big-endian at the end
	binary.BigEndian.PutUint64(padded[paddedLen-8:], bitLen)

	return padded
}

// Calculates SHA256 hash and returns both final hash and midstate
func CalculateSHA256WithMidstate(message []byte) (finalHash []byte, midstate []byte) {
	// Initialize hash values (first 32 bits of fractional parts of square roots of first 8 primes)
	state := SHA256State{
		H: [8]uint32{
			0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
			0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
		},
	}

	// Pad the message
	padded := padMessage(message)

	// Process each 512-bit (64-byte) block
	numBlocks := len(padded) / 64
	var midstateState SHA256State

	for i := 0; i < numBlocks; i++ {
		block := padded[i*64 : (i+1)*64]
		processBlock(&state, block)

		// Save midstate after first block (useful for Bitcoin mining)
		if i == 0 {
			midstateState = state
		}
	}

	// Convert final state to byte array
	finalHash = make([]byte, 32)
	for i := 0; i < 8; i++ {
		binary.BigEndian.PutUint32(finalHash[i*4:(i+1)*4], state.H[i])
	}

	// Convert midstate to byte array
	midstate = make([]byte, 32)
	for i := 0; i < 8; i++ {
		binary.BigEndian.PutUint32(midstate[i*4:(i+1)*4], midstateState.H[i])
	}

	return finalHash, midstate
}
