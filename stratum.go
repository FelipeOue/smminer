package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"smminer/drivers"
	"smminer/util"
	"strconv"
	"strings"
	"sync"
	"time"
)

type StratumRequest struct {
	ID     string      `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

type StratumResponse struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Result []any  `json:"result"`
	Params []any  `json:"params"`
	Error  []any  `json:"error"`
}

type StratumJob struct {
	ID             string
	PrevBlockHash  string
	CoinbasePrefix string
	ExtraNonce1    string
	CoinbaseSuffix string
	MerkleBranches []string
	BlockVersion   string
	NetworkTarget  string // Difficulty (compressed target) to solve a block, also called nBits.
	PoolDifficulty string
	BlockTimestamp string // Also called Ntime. Don't need to be rolled below tens of petahashes.
	ForgetSubmits  bool   // Forget all previous works. Set to FALSE after use.
	ExtraNonce2    string // Extra nonce 2 to be rolled by miner handlers.

	MerkleRoot string // Calculated at every Extranonce2 increment
}

type StratumWork struct {
	ID             string // JobID, only used to track pool response
	WorkID         string // Unique ID for this work, used to track which work found a share
	Hash           string
	Difficulty     float64
	Nonce          string
	Version        string
	ExtraNonce2    string
	BlockTimestamp string
}

var (
	StratumMutex           sync.Mutex // Mutex used to lock changes on Stratum*,Last*,Pool*
	StratumServerPing      int
	StratumConnection      net.Conn
	StratumReadBuffer      *bufio.Reader
	StratumCredentials     []string
	StratumBestShare       float64
	LastStratumResponse    StratumResponse
	LastStratumSubmits     []StratumWork // Previous works submitted to the pool.
	LastStratumJob         StratumJob    // Last received job to work with.
	PoolDifficulty         float64
	PoolExtraNonce1        string         // Instance ID to build merkleroot. Is created on stratumSubscribe() and fixed by session.
	PoolExtraNonce1Changed bool   = false // If using same session failed (in this case will throw away all finishedjobs)
	PoolExtraNonce2Len     int
	PoolDisconnection      bool     // Indicates if the pool connection was lost
	PoolRejections         int  = 0 // consecutive rejections
)

func StratumPing(poolURL string) int {
	host, _, _ := net.SplitHostPort(poolURL)
	util.AppLog("STRATUM", "Executing ping on the pool hostname...", "")
	ping := util.Ping(host)
	StratumServerPing = ping
	util.AppLog("STRATUM", "Ping of "+host+" is "+strconv.Itoa(ping)+"ms", "")
	return ping
}

func StratumConnect(poolURL string) error {
	var (
		err   error
		tries int
	)

	// Close existing connection with buffer if any
	if StratumConnection != nil {
		StratumConnection.Close()
		StratumConnection = nil
		StratumReadBuffer = nil
	}

	PoolDisconnection = false

	for tries = 1; tries <= 5; tries++ {
		StratumConnection, err = net.Dial("tcp", poolURL)
		if err == nil {
			util.AppLog("STRATUM", "Connected to "+poolURL, "")
			StratumReadBuffer = bufio.NewReader(StratumConnection)
			return err
		}

		util.AppLog("STRATUM", "Connection attempt "+strconv.Itoa(tries)+"/5 failed. Trying again...", "")
		time.Sleep(2 * time.Second)
	}

	util.AppLog("STRATUM", "Failed to connect to "+poolURL, err.Error())
	StratumConnection = nil
	return err
}

func StratumSubscribe() (int, error) {

	var isReconnection = false
	var params []string
	params = append(params, "smminer/"+PACKAGE_VERSION) // for testing

	util.AppLog("STRATUM", "Subscribing to pool...", "")

	if len(PoolExtraNonce1) > 0 {
		util.AppLog("STRATUM", "Trying to use previous session...", "")
		params = append(params, PoolExtraNonce1)
		isReconnection = true
	}

	request, err := stratumWrite(StratumRequest{
		ID:     "FFFF",
		Method: "mining.subscribe",
		//Params: []string{"SMMINER/" + PACKAGE_VERSION},
		Params: params,
	})
	if err != nil {
		return request, err
	}

	response, err := stratumRead()
	if err != nil {
		return 0, err
	}

	if len(response.Result) == 0 {
		return 0, fmt.Errorf("pool rejected connection or sent a bad response")
	}

	var (
		subscriptions = response.Result[0].([]any)
		subscription1 = subscriptions[0].([]any)
		//subscription2  = subscriptions[1].([]any)
		extraNonce1    = response.Result[1].(string)
		extraNonce2Len = int(response.Result[2].(float64))
	)
	// some pools send invalid diff values during subscription...
	if value, ok := subscription1[1].(string); ok {
		poolDiff, _ := strconv.ParseFloat(value, 32)
		PoolDifficulty = poolDiff
		if uint32(poolDiff) == 0 {
			util.AppLog("STRATUM", "Pool subscribed with invalid diff value", "")
			util.AppLog("STRATUM", "Difficulty will be set to 1 as default", "")
			PoolDifficulty = 1
		} else {
			util.AppLog("STRATUM", "Pool set default difficulty to "+strconv.FormatFloat(PoolDifficulty, 'f', -1, 64), "")
		}
	} else {
		util.AppLog("STRATUM", "Pool subscribed with invalid diff value", "")
		util.AppLog("STRATUM", "Difficulty will be set to 1 as default", "")
		PoolDifficulty = 1
	}
	if extraNonce1 != PoolExtraNonce1 && isReconnection {
		util.AppLog("STRATUM", "Pool rejected connection to the same session", "")
	}
	PoolExtraNonce1 = extraNonce1
	PoolExtraNonce2Len = extraNonce2Len

	//log.Fatal("ExtraNonce1: " + PoolExtraNonce1)

	return request, err
}

func StratumAuthenticate(username, password string) error {
	// send and save credentials to later use
	_, err := stratumWrite(StratumRequest{
		ID:     "FFFF",
		Method: "mining.authorize",
		Params: []string{username, password},
	})
	StratumCredentials = []string{username, password}
	if err != nil {
		return err
	}

	// some pools will send mining.notify with a job and not
	// answer the authentication request so...
	response, err := stratumRead()
	if len(response.Result) == 0 && len(response.Params) > 0 {
		util.AppLog("STRATUM", "Pool skipped credential validation!", "")
		return err
	}
	if value, ok := response.Result[0].(bool); ok {
		if value {
			util.AppLog("STRATUM", "The user "+StratumCredentials[0]+" has been authenticated", "")
			return err
		} else {
			return errors.New("authentication failed")
		}
	}
	return err
}

// request the pool to change the difficulty, not garanteed to be accepted.
func StratumSuggestDiff(difficulty float64) {
	util.AppLog("STRATUM", "Suggesting difficulty "+strconv.FormatFloat(difficulty, 'f', -1, 64), "")
	_, err := stratumWrite(StratumRequest{
		ID:     "FFFF",
		Method: "mining.suggest_difficulty",
		Params: []float64{difficulty},
	})
	if err != nil {
		util.AppLog("STRATUM", "Failed to suggest difficulty: "+strconv.FormatFloat(difficulty, 'f', -1, 64), "")
	}
}

var LastBlockHeight uint64

// receives notifications from pool and update the global variables.
func StratumReceiver() error {
	response, _ := stratumRead()
	StratumMutex.Lock()
	if response.Method == "mining.notify" {
		// convert merkle branchs interface
		merkleBranches := make([]string, len(response.Params[4].([]interface{})))
		for i, branch := range response.Params[4].([]interface{}) {
			merkleBranches[i] = branch.(string)
		}

		if len(merkleBranches) > 0 && response.Params[1].(string) != "" {
			blockHeightHex := response.Params[2].(string)[86 : 86+6]
			blockHeight, _ := strconv.ParseUint(util.ReverseHexString(blockHeightHex), 16, 32)
			if LastBlockHeight != blockHeight {
				util.AppLog("STRATUM", "New block detected at height "+strconv.FormatUint(blockHeight-1, 10), "")
			}

			LastBlockHeight = blockHeight

			LastStratumJob = StratumJob{
				ID:             response.Params[0].(string),
				PrevBlockHash:  response.Params[1].(string),
				CoinbasePrefix: response.Params[2].(string),
				CoinbaseSuffix: response.Params[3].(string),
				MerkleBranches: merkleBranches,
				BlockVersion:   response.Params[5].(string),
				NetworkTarget:  response.Params[6].(string),
				BlockTimestamp: response.Params[7].(string),
				ForgetSubmits:  response.Params[8].(bool),
				ExtraNonce1:    PoolExtraNonce1,
				ExtraNonce2:    hex.EncodeToString(util.PadLeft(0, PoolExtraNonce2Len)),
			}

			util.AppDebug("stratumReceiver", "New job received: "+strings.Join([]string{
				"ID: " + LastStratumJob.ID,
				"PrevBlockHash: " + LastStratumJob.PrevBlockHash,
				"CoinbasePrefix: " + LastStratumJob.CoinbasePrefix[:30] + "...",
				"CoinbaseSuffix: " + LastStratumJob.CoinbaseSuffix[:30] + "...",
				"MerkleBranches: " + LastStratumJob.MerkleBranches[0] + "...",
				"BlockVersion: " + LastStratumJob.BlockVersion,
				"NetworkDifficulty: " + LastStratumJob.NetworkTarget,
				"BlockTimestamp: " + LastStratumJob.BlockTimestamp,
				"ForgetSubmits: " + strconv.FormatBool(LastStratumJob.ForgetSubmits),
				"ExtraNonce1: " + LastStratumJob.ExtraNonce1,
				"ExtraNonce2: " + LastStratumJob.ExtraNonce2,
			}, "\n"), "")
		}
	}
	if response.Method == "mining.set_difficulty" {
		PoolDifficulty = response.Params[0].(float64)
		LastStratumJob.PoolDifficulty = strconv.FormatFloat(PoolDifficulty, 'f', 3, 64)
		util.AppLog("STRATUM", "Pool difficulty set to "+strconv.FormatFloat(float64(PoolDifficulty), 'f', -1, 64), "")
	}

	// job have a id != FFFF
	if response.ID != "FFFF" && response.ID != "" {
		// check if the response is for a submit
		for i, submit := range LastStratumSubmits {
			if response.ID == submit.WorkID {
				shareDiff := strconv.FormatFloat(submit.Difficulty, 'f', 0, 64)
				if PoolDifficulty > 1 {
					// format share diff without decimal points
					shareDiff = util.FormatDiff(submit.Difficulty)
				}
				if len(response.Result) > 0 && response.Result[0] != nil {
					if accepted, ok := response.Result[0].(bool); ok && accepted {
						util.AppLog("STRATUM", "Share accepted "+submit.Nonce+" "+shareDiff+"/"+strconv.FormatFloat(PoolDifficulty, 'f', -1, 64), "")
						// TODO: Move aonbestshare ta hell outa of here, stratum.go is not the place to driver logic
						if submit.Difficulty > StratumBestShare {
							StratumBestShare = submit.Difficulty
							drivers.AonBestShare = submit.Difficulty
							//util.AppLog("STRATUM", "New best share found: "+util.FormatDiff(BestShare), "")
						}
						PoolRejections = 0
					} else {
						util.AppLog("STRATUM", "Share rejected "+submit.Nonce+" "+shareDiff+"/"+strconv.FormatFloat(PoolDifficulty, 'f', -1, 64), "")
						if len(response.Error) > 1 && response.Error[0] != nil {
							util.AppLog("STRATUM", "Pool error message: "+response.Error[0].(string), "")
						}
						PoolRejections++
					}
				} else {
					util.AppLog("STRATUM", "Share rejected (Stale) "+submit.Nonce+" "+shareDiff+"/"+strconv.FormatFloat(PoolDifficulty, 'f', -1, 64), "")
					PoolRejections++
				}
				// remove the submit from the list
				LastStratumSubmits = append(LastStratumSubmits[:i], LastStratumSubmits[i+1:]...)
				break
			}
		}
	}

	if PoolRejections > 3 {
		util.AppLog("STRATUM", "Too many rejections, forcing job change...", "")
		PoolRejections = 0
		LastStratumJob.ForgetSubmits = true
	}

	defer StratumMutex.Unlock()
	return nil
}

// Set version mask to enable version rolling
func StratumSetVersionMask(versionMask uint32) error {
	util.AppLog("STRATUM", "Setting version mask to "+strconv.FormatUint(uint64(versionMask), 16), "")
	_, err := stratumWrite(StratumRequest{
		ID:     "FFFF",
		Method: "mining.configure",
		Params: []any{
			[]string{"version-rolling"},
			map[string]string{"version-rolling.mask": strconv.FormatUint(uint64(versionMask), 16)},
		},
	})
	if err != nil {
		util.AppLog("STRATUM", "Failed to set version mask to "+strconv.FormatUint(uint64(versionMask), 16), "")
	}
	return err
}

// Submit work to the pool
func StratumSubmit(work StratumWork, versionRolling bool) error {
	params := []string{
		StratumCredentials[0],
		work.ID,
		work.ExtraNonce2,
		work.BlockTimestamp,
		work.Nonce,
	}

	if versionRolling {
		params = append(params, work.Version)
	}

	request := StratumRequest{
		ID:     work.WorkID,
		Method: "mining.submit",
		Params: params,
	}

	_, err := stratumWrite(request)
	if err != nil {
		return err
	}

	// check if it's accepted on the stratumReceiver
	// for now just store a copy of the job to track
	if len(LastStratumSubmits) == 10 {
		LastStratumSubmits = LastStratumSubmits[1:]
	}
	LastStratumSubmits = append(LastStratumSubmits, work)

	return err
}

// update the current job with a incremented extranonce2
func StratumNonce2Increment() error {
	StratumMutex.Lock()
	// convert to uint32 and increment the ExtraNonce2 on global stratum job
	extraNonce2Int, err := strconv.ParseUint(LastStratumJob.ExtraNonce2, 16, 32)
	if err == nil {
		extraNonce2Int++
		LastStratumJob.ExtraNonce2 = hex.EncodeToString(util.PadLeft(uint32(extraNonce2Int), PoolExtraNonce2Len))
	}
	// generate a new merkleroot with the new extranonce2
	merkleRoot, err := util.ComputeMerkleRoot(
		LastStratumJob.CoinbasePrefix,
		LastStratumJob.ExtraNonce1,
		LastStratumJob.ExtraNonce2,
		LastStratumJob.CoinbaseSuffix,
		LastStratumJob.MerkleBranches,
	)
	LastStratumJob.MerkleRoot = merkleRoot
	StratumMutex.Unlock()

	return err
}

// Locks the code to wait for a job if there's none available
func StratumWaitJob() {
	if len(LastStratumJob.PrevBlockHash) == 0 {
		util.AppLog("STRATUM", "Waiting for the first job...", "")
		for len(LastStratumJob.PrevBlockHash) == 0 {
			time.Sleep(1 * time.Second)
		}
	}
	LastStratumJob.ForgetSubmits = false
}

func stratumWrite(request StratumRequest) (int, error) {
	if StratumConnection == nil {
		return 0, errors.New("stratum connection is null")
	}
	StratumConnection.SetWriteDeadline(time.Now().Add(3 * time.Second))
	requestBytes, _ := json.Marshal(request)

	util.AppDebug("stratumWrite", " -> "+string(requestBytes), "")
	response, err := StratumConnection.Write(append(requestBytes, '\n'))

	if err != nil {
		PoolDisconnection = true
	}

	return response, err
}

func stratumRead() (StratumResponse, error) {
	// add delay to process the last sent request
	//time.Sleep(time.Duration(StratumServerPing/2) * time.Microsecond)

	if StratumConnection == nil || StratumReadBuffer == nil {
		return StratumResponse{}, errors.New("stratum connection not initialized")
	}

	StratumConnection.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	rawResponse, err := StratumReadBuffer.ReadString('\n')

	util.AppDebug("stratumRead", " <- "+rawResponse, "")
	//log.Println(rawResponse)

	response := StratumResponse{}
	stratumResponseFormat(&rawResponse)
	json.Unmarshal([]byte(rawResponse), &response)
	return response, err
}

// Correct bad pool response
func stratumResponseFormat(rawResponse *string) {
	re := regexp.MustCompile(`"result"\s*:\s*(true|false|null)\b`)
	re2 := regexp.MustCompile(`"error"\s*:\s*(true|false|null)\b`)
	*rawResponse = re.ReplaceAllString(*rawResponse, `"result":[$1]`)
	*rawResponse = re2.ReplaceAllString(*rawResponse, `"error":[$1]`)
	util.AppDebug("stratumResponseFormat", "  "+*rawResponse, "")
	//log.Println("stratumResponseFormat", "  "+*rawResponse)
}
