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

package p2pool

import (
	"encoding/hex"
	"fmt"
	"time"
)

const TOTAL_TIMEOUT = 25 * time.Second

type P2PoolClient struct {
	Address string // 127.0.0.1:3332 for P2Pool, 127.0.0.1:3331 for Xmrig Proxy pointing to P2Pool
	Jobs    chan *MultiClientJob

	// The original P2Pool job difficulty
	JobDiff uint64

	client Client
}

func (p *P2PoolClient) Stop() {
	err := p.client.conn.Close()
	if err != nil {
		fmt.Println(err)
	}
	if p.Jobs != nil {
		p.Jobs <- nil // send a nil job to indicate that it should stop
	}
}

func (p *P2PoolClient) SubmitShare(nonce []byte, jobid, result string) (*Response, error) {
	return p.client.SubmitWork(hex.EncodeToString(nonce), jobid, result)
}

// This function only returns when the client is closed
// either because of timeout/too many failed attempts, or because it was stopped with client.Stop()
func (p *P2PoolClient) Start() {
	lastReceived := time.Now()

	goto start
start:

	err, code, msg, jobChan := p.client.Connect(p.Address, false, "XMRig/6.21.0", "login", "x", "myrigid")
	if err != nil {
		fmt.Println(err, "code", code, "msg", msg)
		time.Sleep(time.Second)
		goto redo
	}

	for {
		curJob := <-jobChan
		if curJob == nil || !p.client.IsAlive() {
			fmt.Println("job: client is not alive")

			break
		}

		lastReceived = time.Now()

		p.Jobs <- curJob
	}

redo:
	if time.Since(lastReceived) > TOTAL_TIMEOUT {
		return
	}

	time.Sleep(time.Second)
	p.client = Client{}
	goto start
}
