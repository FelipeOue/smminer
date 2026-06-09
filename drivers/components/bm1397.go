package components

var BM1397 = BM13xxRequest{
	PREAMBLE:                []byte{0x55, 0xAA},
	CHIP_ID:                 []byte{0x13, 0x97},
	CHIP_RESPONSE_LEN:       9,
	TYPE_JOB:                0x20,
	TYPE_CMD:                0x40,
	GROUP_SINGLE:            0x00,
	GROUP_ALL:               0x10,
	CMD_JOB:                 0x01,
	CMD_SETADDRESS:          0x00,
	CMD_WRITE:               0x01,
	CMD_READ:                0x02,
	CMD_INACTIVE:            0x03,
	RESPONSE_CMD:            0x00,
	RESPONSE_JOB:            0x80,
	SLEEP_TIME:              20,
	FREQ_MULT:               25.0,
	CLOCK_ORDER_CONTROL_0:   0x80,
	CLOCK_ORDER_CONTROL_1:   0x84,
	ORDERED_CLOCK_ENABLE:    0x20,
	CORE_REGISTER_CONTROL:   0x3C,
	PLL3_PARAMETER:          0x68,
	FAST_UART_CONFIGURATION: 0x28,
	MISC_CONTROL:            0x18,
}

type BM1397JobCommand struct {
	Preamble       [2]byte
	Command        [2]byte
	JobID          uint8
	MidstatesCount uint8
	StartingNonce  [4]byte
	NBits          [4]byte
	NTime          [4]byte
	MerkleRoot     [32]byte
	PrevBlockHash  [32]byte
	Version        [4]byte
	CRC16          [4]byte
}
