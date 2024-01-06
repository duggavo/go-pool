/*
 * This file is part of go-pool.
 *
 * go-pool is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * go-pool is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with go-pool. If not, see <http://www.gnu.org/licenses/>.
 */

// package p2pool implements a P2Pool stratum client that listens to jobs and can submit shares
package p2pool

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"go-pool/config"
	"go-pool/logger"
	"io"
	"net"
	"sync"
	"time"
)

const SUBMIT_WORK_JSONRPC_ID = 541

type Job struct {
	Blob   string `json:"blob"`
	JobID  string `json:"job_id"`
	Target string `json:"target"`
	Algo   string `json:"algo"`
}

type RXJob struct {
	Job
	Height   uint64 `json:"height"`
	SeedHash string `json:"seed_hash"`
}

type ForknoteJob struct {
	Job
	MajorVersion int `json:"blockMajorVersion"`
	MinorVersion int `json:"blockMinerVersion"`
}

type MultiClientJob struct {
	RXJob
	NetworkDifficulty int64  `json:"net_diff"`
	Reward            int64  `json:"reward"`
	ConnNonce         uint32 `json:"nonce"`
}

type SubmitWorkResult struct {
	Status string `json:"status"`
}

type Response struct {
	ID      uint64 `json:"id"`
	Jsonrpc string `json:"jsonrpc"`
	Method  string `json:"method"`

	Job    *MultiClientJob  `json:"params"` // used to send jobs over the connection
	Result *json.RawMessage `json:"result"` // used to return SubmitWork results

	Error interface{} `json:"error"`
}

type loginResponse struct {
	ID      uint64 `json:"id"`
	Jsonrpc string `json:"jsonrpc"`
	Result  *struct {
		ID  string          `json:"id"`
		Job *MultiClientJob `job:"job"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	// our own custom field for reporting login warnings without forcing disconnect from error:
	Warning *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"warning"`
}

type Client struct {
	address         string
	conn            net.Conn
	responseChannel chan *Response

	clientId string

	mutex sync.Mutex

	alive bool // true when the stratum client is connected. Set to false upon call to Close(), or when Connect() is called but
	// a new connection is yet to be established.
}

func (cl *Client) String() string {
	// technically this should be locked to access address, but then
	// we risk deadlock if we try to use it when client is already locked.
	return "client:" + cl.address
}

func (cl *Client) IsAlive() bool {
	cl.mutex.Lock()
	defer cl.mutex.Unlock()
	return cl.alive
}

// Connect to the stratum server port with the given login info. Returns error if connection could
// not be established, or if the stratum server itself returned an error. In the latter case,
// code and message will also be specified. If the stratum server returned just a warning, then
// error will be nil, but code & message will be specified.
func (cl *Client) Connect(
	address string, useTLS bool, agent string,
	uname, pw, rigid string) (err error, code int, message string, jobChan <-chan *MultiClientJob) {
	cl.Close() // just in case caller forgot to call close before trying a new connection
	cl.mutex.Lock()
	defer cl.mutex.Unlock()
	cl.address = address

	if !useTLS {
		cl.conn, err = net.DialTimeout("tcp", address, time.Second*30)
	} else {
		cl.conn, err = tls.Dial("tcp", address, nil /*Config*/)
	}
	if err != nil {
		fmt.Println("Dial failed:", err, cl)
		return err, 0, "", nil
	}
	// send login
	loginRequest := &struct {
		ID     uint64      `json:"id"`
		Method string      `json:"method"`
		Params interface{} `json:"params"`
	}{
		ID:     5121,
		Method: "login",
		Params: &struct {
			Login string `json:"login"`
			Pass  string `json:"pass"`
			RigID string `json:"rigid"`
			Agent string `json:"agent"`
		}{
			Login: uname,
			Pass:  pw,
			RigID: rigid,
			Agent: agent,
		},
	}

	data, err := json.Marshal(loginRequest)
	if err != nil {
		fmt.Println("json marshalling failed:", err, "for client")
		return err, 0, "", nil
	}
	cl.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	logger.NetDev("sending to p2pool:", string(data))
	data = append(data, '\n')
	if _, err = cl.conn.Write(data); err != nil {
		fmt.Println("writing request failed:", err, "for client")
		return err, 0, "", nil
	}

	// Now read the login response
	response := &loginResponse{}
	cl.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	rdr := bufio.NewReaderSize(cl.conn, config.MAX_REQUEST_SIZE)
	err = readJSON(response, rdr)
	if err != nil {
		fmt.Println("readJSON failed for client:", err)
		return err, 0, "", nil
	}
	if response.Result == nil {
		if response.Error != nil {
			fmt.Println("Didn't get job result from login response:", response.Error)
			return errors.New("stratum server error"), response.Error.Code, response.Error.Message, nil
		}
		fmt.Println("Malformed login response:", response)
		return errors.New("malformed login response"), 0, "", nil
	}

	cl.responseChannel = make(chan *Response)
	cl.alive = true
	jc := make(chan *MultiClientJob)
	if response.Result.Job == nil {
		fmt.Println("malformed login response result:", response.Result)
		return errors.New("malformed login response case 2"), 0, "", nil
	}

	cl.clientId = response.Result.ID

	go dispatchJobs(cl.conn, jc, response.Result.Job, cl.responseChannel)
	if response.Warning != nil {
		return nil, response.Warning.Code, response.Warning.Message, jc
	}
	return nil, 0, "", jc
}

// if error is returned then client will be closed and put in not-alive state
func (cl *Client) submitRequest(submitRequest interface{}, expectedResponseID uint64) (*Response, error) {
	cl.mutex.Lock()
	if !cl.alive {
		cl.mutex.Unlock()
		return nil, errors.New("client not alive")
	}
	data, err := json.Marshal(submitRequest)
	if err != nil {
		fmt.Println("json marshalling failed:", err, "for client")
		cl.mutex.Unlock()
		return nil, err
	}
	cl.conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
	logger.NetDev("sending to p2pool:", string(data))
	data = append(data, '\n')
	if _, err = cl.conn.Write(data); err != nil {
		fmt.Println("writing request failed:", err, "for client")
		cl.mutex.Unlock()
		return nil, err
	}
	respChan := cl.responseChannel
	cl.mutex.Unlock()

	// await the response
	response := <-respChan
	if response == nil {
		fmt.Println("got nil response, closing")
		return nil, fmt.Errorf("submit work failure: nil response")
	}
	if response.ID != expectedResponseID {
		fmt.Println("got unexpected response:", response.ID, "wanted:", expectedResponseID, "Closing connection.")
		return nil, fmt.Errorf("submit work failure: unexpected response")
	}
	return response, nil
}

// If error is returned by this method, then client will be closed and put in not-alive state.
func (cl *Client) SubmitWork(nonce, jobid, result string) (*Response, error) {
	submitRequest := &struct {
		ID     uint64      `json:"id"`
		Method string      `json:"method"`
		Params interface{} `json:"params"`
	}{
		ID:     SUBMIT_WORK_JSONRPC_ID,
		Method: "submit",
		Params: &struct {
			ID     string `json:"id"`
			JobID  string `json:"job_id"`
			Nonce  string `json:"nonce"`
			Result string `json:"result"`
		}{cl.clientId, jobid, nonce, result},
	}
	return cl.submitRequest(submitRequest, SUBMIT_WORK_JSONRPC_ID)
}

func (cl *Client) Close() {
	cl.mutex.Lock()
	defer cl.mutex.Unlock()
	if !cl.alive {
		return
	}
	cl.alive = false
	cl.conn.Close()
}

// dispatchJobs will forward incoming jobs to the JobChannel until error is received or the
// connection is closed. Client will be in not-alive state on return.
func dispatchJobs(conn net.Conn, jobChan chan<- *MultiClientJob, firstJob *MultiClientJob, responseChan chan<- *Response) {
	defer func() {
		close(jobChan)
		close(responseChan)
	}()
	jobChan <- firstJob
	reader := bufio.NewReaderSize(conn, config.MAX_REQUEST_SIZE)
	for {
		response := &Response{}
		conn.SetReadDeadline(time.Now().Add(3600 * time.Second))
		err := readJSON(response, reader)
		if err != nil {
			fmt.Println("readJSON failed, closing client:", err)
			break
		}
		if response.Method != "job" {
			if response.ID == SUBMIT_WORK_JSONRPC_ID {
				responseChan <- response
				continue
			}
			fmt.Println("Unexpected response from stratum server. Ignoring:", *response)
			continue
		}
		if response.Job == nil {
			fmt.Println("Didn't get job as expected from stratum server, closing client:", *response)
			break
		}
		jobChan <- response.Job
	}
}

func readJSON(response interface{}, reader *bufio.Reader) error {
	data, isPrefix, err := reader.ReadLine()

	logger.NetDev("received from p2pool:", string(data))

	if isPrefix {
		fmt.Println("oversize request")
		return errors.New("oversize request")
	} else if err == io.EOF {
		fmt.Println("eof")
		return err
	} else if err != nil {
		fmt.Println("error reading:", err)
		return err
	}
	err = json.Unmarshal(data, response)
	if err != nil {
		fmt.Println("failed to unmarshal json stratum login response:", err)
		return err
	}
	return nil
}
