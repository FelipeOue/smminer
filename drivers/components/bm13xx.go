package components

// From esp-miner
type BM13xxRequest struct {
	// Chip Identification
	PREAMBLE          []byte
	CHIP_ID           []byte
	CHIP_RESPONSE_LEN int

	// COMMAND FIELD BITS ----------------------
	//
	// Request Types
	TYPE_JOB byte
	TYPE_CMD byte
	//
	// Group Types
	GROUP_SINGLE byte
	GROUP_ALL    byte
	//
	// Commands Types
	CMD_JOB        byte
	CMD_SETADDRESS byte
	CMD_WRITE      byte
	CMD_READ       byte
	CMD_INACTIVE   byte
	//
	// COMMAND FIELDS -------------------------

	// Responses Types
	RESPONSE_CMD byte
	RESPONSE_JOB byte

	// Timing Parameters
	SLEEP_TIME int
	FREQ_MULT  float64

	// Memory Addresses
	CHIP_ADDR               byte
	PLL0_PARAMETER          byte
	ROLLING_RANGE           byte
	TICKET_MASK             byte
	MISC_CONTROL            byte
	ORDERED_CLOCK_ENABLE    byte
	FAST_UART_CONFIGURATION byte
	CORE_REGISTER_CONTROL   byte
	ANALOG_MUX_CONTROL      byte
	DRIVE_STRENGTH          byte
	PLL3_PARAMETER          byte
	CLOCK_ORDER_CONTROL_0   byte
	CLOCK_ORDER_CONTROL_1   byte
	VERSION_MASK            byte
	REG_A8                  byte
}

// ChipByName returns the chip request descriptor for a given chip model string.
func ChipByName(name string) BM13xxRequest {
	switch name {
	case "BM1368":
		return BM1368
	case "BM1397":
		return BM1397
	default:
		return BM1368 // safe fallback
	}
}
