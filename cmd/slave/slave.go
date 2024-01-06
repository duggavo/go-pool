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
package main

import (
	"context"
	"encoding/hex"
	"go-pool/config"
	"go-pool/logger"
	"go-pool/slave"
	"go-pool/stratum"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/duggavo/go-monero/rpc"
	"github.com/duggavo/go-monero/rpc/daemon"
)

type Infos struct {
	Height         uint64
	FutureHeight   uint64
	SeedHash       string
	Difficulty     uint64
	BlockReward    uint64
	MajorVersion   uint
	LastTemplateAt int64

	sync.RWMutex
}

var CurInfo Infos

// blockhashing_blob
// 39 bytes...
// 4 bytes nonce
// x bytes nonce extra
// ...

type SIGUSR1 struct {
}

func (s SIGUSR1) String() string {
	return "SIGUSR1"
}
func (s SIGUSR1) Signal() {
}

var sigc chan os.Signal

// the Refresher Thread
func Refresher() {
	sigc = make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGUSR1)
	go func() {
		for {
			s := <-sigc

			logger.Debug("received SIGUSR1:", s)

			// get new height

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			height, err := client.GetHeight(ctx)
			cancel()
			if err != nil {
				logger.Warn(err)
				continue
			}

			CurInfo.Lock()
			if height.Height != CurInfo.Height {
				CurInfo.Height = height.Height
				go OnNewBlock()

				ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
				blockheader, err := client.GetLastBlockHeader(ctx)
				cancel()
				if err != nil {
					logger.Warn(err)
					CurInfo.Unlock()
					continue
				}

				CurInfo.MajorVersion = blockheader.BlockHeader.MajorVersion
			}
			CurInfo.Unlock()
		}
	}()

	go func() {
		for {
			time.Sleep(10 * time.Second)
			srv.ConnsMut.RLock()
			slave.SendStats(len(srv.Connections))
			srv.ConnsMut.RUnlock()
		}
	}()
	for {
		CurInfo.RLock()
		curHeight := CurInfo.Height
		CurInfo.RUnlock()

		if curHeight != 0 {
			time.Sleep(5 * time.Second)
		} else {
			CurInfo.Lock()
			CurInfo.Height = 1
			CurInfo.Unlock()
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		height, err := client.GetHeight(ctx)
		cancel()
		if err != nil {
			logger.Warn(err)
			continue
		}

		CurInfo.Lock()
		if height.Height != CurInfo.Height || time.Now().Unix()-CurInfo.LastTemplateAt > int64(config.Cfg.SlaveConfig.TemplateTimeout) {
			CurInfo.LastTemplateAt = time.Now().Unix()
			if CurInfo.Height != height.Height {
				logger.Debug("New height:", CurInfo.Height, "->", height.Height)
			} else {
				logger.Debug("Refreshing template")
			}
			CurInfo.Height = height.Height
			CurInfo.Unlock()
			go OnNewBlock()
		} else {
			CurInfo.Unlock()
		}

		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		blockheader, err := client.GetLastBlockHeader(ctx)
		cancel()
		if err != nil {
			logger.Warn(err)
			continue
		}

		CurInfo.Lock()
		CurInfo.MajorVersion = blockheader.BlockHeader.MajorVersion
		CurInfo.Unlock()
	}
}
func OnNewBlock() {
	srv.ConnsMut.Lock()
	defer srv.ConnsMut.Unlock()

	for _, v := range srv.Connections {
		c := v
		go func() {
			c.Lock()
			defer c.Unlock()

			if config.Cfg.UseP2Pool {
				// Do nothing
			} else {
				c.LastJob = c.CurrentJob

				CurInfo.RLock()
				// update difficulty
				if c.NextDiff < float64(config.Cfg.SlaveConfig.MinDiff) {
					c.NextDiff = float64(config.Cfg.SlaveConfig.MinDiff)
				} else if c.NextDiff >= float64(CurInfo.Difficulty) {
					c.NextDiff = float64(CurInfo.Difficulty - 1)
				}
				CurInfo.RUnlock()

				c.CurrentJob.Diff = uint64(c.NextDiff)

				if c.Nicehash {
					// generate job & send job response
					curJob, jobBlob, nicehashByte, err := GetNicehashJob(c.CurrentJob.Diff)
					if err != nil {
						logger.Error(err)
						srv.Kick(c.Id)
						return
					}
					c.CurrentJob.NicehashByte = nicehashByte
					c.CurrentJob.Blob = jobBlob
					c.CurrentJob.HashingBlob, err = hex.DecodeString(curJob.Blob)
					if err != nil {
						logger.Error(err)
						srv.Kick(c.Id)
						return
					}
					c.CurrentJob.JobID = curJob.JobID
					jb := &stratum.JobNotification{
						Jsonrpc: "2.0",
						Method:  "job",
						Params:  *curJob,
					}
					err = c.Send(jb)
					if err != nil {
						srv.Kick(c.Id)
					}
				} else {
					// generate job & send job response
					curJob, jobBlob, err := GetJob(c.CurrentJob.Diff)
					if err != nil {
						logger.Error(err)
						srv.Kick(c.Id)
						return
					}
					c.CurrentJob.Blob = jobBlob
					c.CurrentJob.HashingBlob, err = hex.DecodeString(curJob.Blob)
					if err != nil {
						logger.Error(err)
						srv.Kick(c.Id)
						return
					}
					c.CurrentJob.JobID = curJob.JobID
					jb := &stratum.JobNotification{
						Jsonrpc: "2.0",
						Method:  "job",
						Params:  *curJob,
					}
					err = c.Send(jb)
					if err != nil {
						srv.Kick(c.Id)
					}
				}
			}
		}()
	}

	go func() {
		time.Sleep(time.Second)
		ClearNicehashJob()
	}()
}

var client *daemon.Client
var srv *stratum.Server

func main() {
	go slave.StartSlaveClient()

	rpcClient, err := rpc.NewClient(config.Cfg.DaemonRpc)
	if err != nil {
		panic(err)
	}
	client = daemon.NewClient(rpcClient)

	logger.Info("Using daemon RPC " + config.Cfg.DaemonRpc)

	go Refresher()

	srv = &stratum.Server{}
	go srv.Start(config.Cfg.SlaveConfig.PoolPort, config.Cfg.SlaveConfig.PoolPortTls)

	time.Sleep(time.Second)
	for {
		conn := <-srv.NewConnections
		go HandleConnection(conn)
	}
}
