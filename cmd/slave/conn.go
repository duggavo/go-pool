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
	"bufio"
	"context"
	"encoding/hex"
	"go-pool/address"
	"go-pool/config"
	"go-pool/logger"
	"go-pool/p2pool"
	"go-pool/slave"
	"go-pool/stratum"
	"go-pool/template"
	"go-pool/util"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/duggavo/go-monero/rpc/daemon"
)

func HandleConnection(conn *stratum.Connection) {
	// read login request
	req := stratum.RequestLogin{}
	conn.Conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	reader := bufio.NewReaderSize(conn.Conn, config.MAX_REQUEST_SIZE)
	err := stratum.ReadJSON(&req, reader)
	if err != nil {
		logger.Debug("ReadJSON failed in server:", err)
		srv.Kick(conn.Id)
		return
	}
	reqParams := req.Params
	if reqParams.Login == "" {
		logger.Debug("client sent a malformed login request")
		srv.Kick(conn.Id)
		return
	}
	if reqParams.Pass == "" {
		reqParams.Pass = "x"
	}
	if reqParams.Agent == "" {
		reqParams.Agent = "No agent specified"
	}

	logger.Info("stratumServer received connection")
	logger.Info("login", reqParams.Login)
	logger.Info("pass ", reqParams.Pass)
	logger.Info("algo ", reqParams.Algo)
	logger.Info("agent", reqParams.Agent)

	if (len(reqParams.Agent) > 5 && reqParams.Agent[:5] == "XMRig") || reqParams.NicehashSupport {
		logger.Dev("NiceHash mode: ON")
		conn.Nicehash = true
	}

	splitLogin := strings.Split(reqParams.Login, "+")
	connAddress := splitLogin[0]

	if len(reqParams.Login) < 10 || !address.IsAddressValid(connAddress) {
		logger.Warn("Address", connAddress, "is not valid")
		conn.Send(map[string]any{
			"id":      req.ID,
			"jsonrpc": "2.0",
			"error": stratum.ErrorJson{
				Code:    -1,
				Message: "Invalid payment address provided",
			},
		})
		srv.Kick(conn.Id)
		return
	}

	if len(splitLogin) > 1 {
		diffVal, err := strconv.ParseUint(splitLogin[1], 10, 64)
		if err != nil {
			logger.Debug(err)
		} else {
			CurInfo.RLock()
			if diffVal < config.Cfg.SlaveConfig.MinDiff {
				diffVal = config.Cfg.SlaveConfig.MinDiff
			} else if diffVal > CurInfo.Difficulty/2 {
				diffVal = CurInfo.Difficulty / 2
			}
			CurInfo.RUnlock()
			conn.CurrentJob.Diff = diffVal
		}
	}

	if conn.CurrentJob.Diff == 0 {
		conn.CurrentJob.Diff = config.Cfg.SlaveConfig.MinDiff * 2
	}
	conn.NextDiff = float64(conn.CurrentJob.Diff)
	conn.LastShare = time.Now().UnixMilli()

	if config.Cfg.UseP2Pool {
		//if !config.P2POOL_USE_NICEHASH {
		conn.Nicehash = false
		//}

		/*if conn.Nicehash {
			conn.P2Pool = p2pool.P2PoolClient{
				Address: config.XMRIG_PROXY_ADDRESS,
				Jobs:    make(chan *p2pool.MultiClientJob),
			}
		} else {*/
		conn.P2Pool = p2pool.P2PoolClient{
			Address: config.Cfg.P2PoolAddress,
			Jobs:    make(chan *p2pool.MultiClientJob),
		}
		//}

		go func() {
			conn.P2Pool.Start()
			srv.Kick(conn.Id)
		}()

		jobData := <-conn.P2Pool.Jobs
		if jobData == nil {
			logger.Warn("first jobData is nil")
			srv.Kick(conn.Id)
			conn.P2Pool.Stop()
			return
		}

		// conn.JobBlob is not set with P2Pool
		conn.CurrentJob.HashingBlob, err = hex.DecodeString(jobData.Blob)
		if err != nil {
			logger.Error(err)
			srv.Kick(conn.Id)
			return
		}
		if len(conn.CurrentJob.HashingBlob) < 43 {
			logger.Error("HashingBlob", hex.EncodeToString(conn.CurrentJob.HashingBlob), "is too short")
			srv.Kick(conn.Id)
			return
		}
		if conn.Nicehash {
			conn.CurrentJob.NicehashByte = conn.CurrentJob.HashingBlob[42]
		}

		jobTarget, err := hex.DecodeString(jobData.Target)
		if err != nil {
			logger.Error(err)
			srv.Kick(conn.Id)
			return
		}

		var jobDiff uint64
		if len(jobTarget) == 4 {
			logger.Dev("job target is 4-byte version")
			jobDiff = template.ShortDiffToDiff(jobTarget)
		} else {
			logger.Dev("job target is 8-byte version")
			jobDiff = template.MidDiffToDiff(jobTarget)
		}

		if jobDiff == 0 {
			logger.Error("jobDiff is zero")
			srv.Kick(conn.Id)
			return
		}
		if conn.CurrentJob.Diff > jobDiff {
			logger.Debug("conn diff > job diff, so we set conn diff from", conn.CurrentJob.Diff, "to", jobDiff)
			conn.CurrentJob.Diff = jobDiff
		}

		// This is the original P2Pool job difficulty
		conn.P2Pool.JobDiff = jobDiff

		conn.CurrentJob.JobID = jobData.JobID

		loginResponse := stratum.LoginResponse{
			ID:     req.ID,
			Status: "OK",
			Result: stratum.LoginResponseResult{
				ID: "0", //curJob.JobID,
				Job: template.Job{
					Algo:     config.Cfg.AlgoName,
					Blob:     jobData.Blob,
					Height:   jobData.Height,
					JobID:    jobData.JobID,
					SeedHash: jobData.SeedHash,
					Target:   template.DiffToShortTarget(conn.CurrentJob.Diff),
				},
				Status:     "OK",
				Extensions: []string{"keepalive"},
			},
			Error: nil,
		}

		if conn.Nicehash {
			loginResponse.Result.Extensions = append(loginResponse.Result.Extensions, "nicehash")
		}
		conn.Send(loginResponse)

		go func() {
			for {
				curJob := <-conn.P2Pool.Jobs
				if curJob == nil {
					logger.Warn("curJob == nil")
					return
				}

				conn.Lock()

				conn.LastJob = conn.CurrentJob

				// update difficulty
				logger.Debug("nextDiff is", conn.NextDiff, "mindiff is", config.Cfg.SlaveConfig.MinDiff)
				if conn.NextDiff < float64(config.Cfg.SlaveConfig.MinDiff) {
					conn.NextDiff = float64(config.Cfg.SlaveConfig.MinDiff)
				} else if conn.NextDiff >= float64(conn.P2Pool.JobDiff) {
					conn.NextDiff = float64(conn.P2Pool.JobDiff)
				}

				logger.Debug("setting conn.Diff to nextDiff", conn.NextDiff)
				conn.CurrentJob.Diff = uint64(conn.NextDiff)

				conn.CurrentJob.HashingBlob, err = hex.DecodeString(curJob.Blob)
				if err != nil {
					logger.Error(err)
					srv.Kick(conn.Id)
					conn.Unlock()
					return
				}

				if conn.Nicehash {
					conn.CurrentJob.NicehashByte = conn.CurrentJob.HashingBlob[42]
				}

				conn.CurrentJob.JobID = curJob.JobID

				jobTarget, err := hex.DecodeString(jobData.Target)
				if err != nil {
					logger.Error(err)
					srv.Kick(conn.Id)
					conn.Unlock()
					return
				}

				var jobDiff uint64
				if len(jobTarget) == 4 {
					logger.Dev("job target is 4-byte version")
					jobDiff = template.ShortDiffToDiff(jobTarget)
				} else {
					logger.Dev("job target is 8-byte version")
					jobDiff = template.MidDiffToDiff(jobTarget)
				}

				if jobDiff == 0 {
					logger.Error("jobDiff is zero")
					srv.Kick(conn.Id)
					conn.Unlock()
					return
				}

				// This is the original P2Pool job difficulty
				conn.P2Pool.JobDiff = jobDiff

				if conn.CurrentJob.Diff > jobDiff {
					logger.Debug("conn diff > job diff, so we set conn diff from", conn.CurrentJob.Diff, "to", jobDiff)
					conn.CurrentJob.Diff = jobDiff
				}

				jb := &stratum.JobNotification{
					Jsonrpc: "2.0",
					Method:  "job",
					Params: template.Job{
						Algo:     config.Cfg.AlgoName,
						Blob:     curJob.Blob,
						Height:   curJob.Height,
						JobID:    curJob.JobID,
						SeedHash: curJob.SeedHash,
						Target:   template.DiffToShortTarget(conn.CurrentJob.Diff),
					},
				}
				err = conn.Send(jb)
				if err != nil {
					srv.Kick(conn.Id)
					conn.Unlock()
					return
				}
				conn.Unlock()
			}
		}()

	} else {
		// generate job & send login response
		if conn.Nicehash { // Send NiceHash Job
			curJob, jobBlob, nicehashByte, err := GetNicehashJob(conn.CurrentJob.Diff)
			if err != nil {
				logger.Error(err)
				srv.Kick(conn.Id)
				return
			}
			conn.CurrentJob.NicehashByte = nicehashByte
			conn.CurrentJob.Blob = jobBlob
			conn.CurrentJob.HashingBlob, err = hex.DecodeString(curJob.Blob)
			if err != nil {
				logger.Error(err)
				srv.Kick(conn.Id)
				return
			}
			conn.CurrentJob.JobID = curJob.JobID

			loginResponse := stratum.LoginResponse{
				ID:     req.ID,
				Status: "OK",
				Result: stratum.LoginResponseResult{
					ID:         "0", //curJob.JobID,
					Job:        *curJob,
					Status:     "OK",
					Extensions: []string{"keepalive", "nicehash"},
				},
				Error: nil,
			}
			conn.Send(loginResponse)
		} else { // Send non-nicehash job
			curJob, jobBlob, err := GetJob(conn.CurrentJob.Diff)
			if err != nil {
				logger.Error(err)
				srv.Kick(conn.Id)
				return
			}
			conn.CurrentJob.Blob = jobBlob
			conn.CurrentJob.HashingBlob, err = hex.DecodeString(curJob.Blob)
			if err != nil {
				logger.Error(err)
				srv.Kick(conn.Id)
				return
			}
			conn.CurrentJob.JobID = curJob.JobID

			loginResponse := stratum.LoginResponse{
				ID:     req.ID,
				Status: "OK",
				Result: stratum.LoginResponseResult{
					ID:         "0", //curJob.JobID,
					Job:        *curJob,
					Status:     "OK",
					Extensions: []string{"keepalive"},
				},
				Error: nil,
			}
			conn.Send(loginResponse)
		}
	}

	// END generate job & send login response

	// read "submit" request for solved shares
	for {
		req := stratum.RequestJob{}
		conn.Conn.SetReadDeadline(time.Now().Add(time.Duration(10*config.Cfg.SlaveConfig.ShareTargetTime) * time.Second))
		reader := bufio.NewReaderSize(conn.Conn, config.MAX_REQUEST_SIZE)
		err := stratum.ReadJSON(&req, reader)

		if err != nil {
			logger.Debug("conn.go ReadJSON failed in server:", err)
			srv.Kick(conn.Id)
			return
		}
		if req.Method == "keepalived" {
			// not using conn.Send for better performance
			conn.SendBytes([]byte("{\"id\":" + strconv.FormatUint(req.ID, 10) + ",\"jsonrpc\":\"2.0\",\"result\":{\"status\":\"KEEPALIVED\"}}"))
			/*conn.Send(stratum.Reply{
				ID:      req.ID,
				Jsonrpc: "2.0",
				Result: map[string]any{
					"status": "KEEPALIVED",
				},
			})*/
			continue
		} else if req.Method != "submit" {
			logger.Debug("Unknown method", req.Method, ". Skipping.")
			continue
		}

		conn.Lock()

		theJob := conn.CurrentJob

		// check the validity of share
		if len(req.Params.Nonce) != 8 || len(req.Params.Result) != 64 ||
			!util.IsHex(req.Params.Nonce) || !util.IsHex(req.Params.Result) {
			logger.Warn("INVALID SHARE RECEIVED: malformed share")
			// not using conn.Send for better performance
			conn.SendBytes([]byte("{\"error\":{\"code\":-1,\"message\":\"malformed share\"},\"id\":" + strconv.FormatUint(req.ID, 10) + ",\"jsonrpc\":\"2.0\"}"))
			/*conn.Send(stratum.Reply{
				ID:      req.ID,
				Jsonrpc: "2.0",
				Error: &stratum.ErrorJson{
					Code:    -1,
					Message: "malformed share",
				},
			})*/
			conn.Unlock()
			continue
		}
		if req.Params.JobID == conn.LastJob.JobID && req.Params.JobID != "" {
			logger.Debug("Using latest job")
			theJob = conn.LastJob
		} else if req.Params.JobID != theJob.JobID {
			logger.Warn("INVALID SHARE RECEIVED: wrong job id")
			// not using conn.Send for better performance
			conn.SendBytes([]byte("{\"error\":{\"code\":-1,\"message\":\"wrong job id\"},\"id\":" + strconv.FormatUint(req.ID, 10) + ",\"jsonrpc\":\"2.0\"}"))
			/*conn.Send(stratum.Reply{
				ID:      req.ID,
				Jsonrpc: "2.0",
				Error: &stratum.ErrorJson{
					Code:    -1,
					Message: "wrong job id",
				},
			})*/
			conn.Unlock()
			continue
		} else {
			logger.Debug("Using older job")
		}
		resultHash, err := hex.DecodeString(req.Params.Result)
		if err != nil {
			// we have already checked that this hash is valid hexadecimal with valid length.
			// For this reason, hex.DecodeString should never fail
			panic("ERROR DECODING HEX HASH! This should NEVER happen!")
		}
		resultNonce, err := hex.DecodeString(req.Params.Nonce)
		if err != nil {
			// we have already checked that the nonce is valid hexadecimal with valid length.
			// For this reason, hex.DecodeString should never fail
			panic("ERROR DECODING HEX NONCE! This should NEVER happen!")
		}

		logger.Dev("Nonce is", hex.EncodeToString(resultNonce))

		if conn.Nicehash && theJob.NicehashByte != 0 && resultNonce[3] != theJob.NicehashByte {
			logger.Warn("INVALID SHARE RECEIVED: wrong nicehash nonce")
			// not using conn.Send for better performance
			conn.SendBytes([]byte("{\"error\":{\"code\":-1,\"message\":\"wrong nicehash nonce\"},\"id\":" + strconv.FormatUint(req.ID, 10) + ",\"jsonrpc\":\"2.0\"}"))
			/*conn.Send(stratum.Reply{
				ID:      req.ID,
				Jsonrpc: "2.0",
				Error: &stratum.ErrorJson{
					Code:    -1,
					Message: "wrong nicehash nonce",
				},
			})*/
			conn.Unlock()
			continue
		}

		resultHashingBlob := make([]byte, len(theJob.HashingBlob))
		copy(resultHashingBlob, theJob.HashingBlob)

		resultBlockBlob := make([]byte, len(theJob.Blob))

		// set the nonce in the blockhashing blob
		resultHashingBlob[39] = resultNonce[0]
		resultHashingBlob[40] = resultNonce[1]
		resultHashingBlob[41] = resultNonce[2]
		resultHashingBlob[42] = resultNonce[3]

		// set the nonce in the block blob
		if !config.Cfg.UseP2Pool { // with p2pool, we don't bother about the block blob
			copy(resultBlockBlob, theJob.Blob)

			resultBlockBlob[39] = resultNonce[0]
			resultBlockBlob[40] = resultNonce[1]
			resultBlockBlob[41] = resultNonce[2]
			resultBlockBlob[42] = resultNonce[3]
		}

		resultHashingBlobString := hex.EncodeToString(resultHashingBlob)

		shareDiff := template.HashToDiff(resultHash)

		if config.Cfg.UseP2Pool {
			logger.Dev("Computed share diff:", shareDiff, "P2pool diff:", conn.P2Pool.JobDiff)
		}

		CurInfo.Lock()
		if shareDiff >= CurInfo.Difficulty || conn.Score < int32(config.Cfg.SlaveConfig.TrustScore) || util.RandomFloat() > 0.5 {
			logger.Debug("Checking share PoW (score", conn.Score, ")")
			calcPow, err := client.CalcPow(context.Background(), daemon.CalcPowParameters{
				MajorVersion: CurInfo.MajorVersion,
				Height:       CurInfo.Height,
				BlockBlob:    resultHashingBlobString,
				SeedHash:     CurInfo.SeedHash,
			})
			if err != nil {
				logger.Warn("error getting pow:", err)
				conn.Send(stratum.Reply{
					ID:      req.ID,
					Jsonrpc: "2.0",
					Error: &stratum.ErrorJson{
						Code:    -1,
						Message: "internal server error",
					},
				})
				conn.Unlock()
				CurInfo.Unlock()
				continue
			}
			if calcPow != req.Params.Result {
				logger.Warn("INVALID SHARE RECEIVED: wrong hash: received:", req.Params.Result, ", should be", calcPow)
				conn.Score = -100
				// not using conn.Send for better performance
				conn.SendBytes([]byte("{\"error\":{\"code\":-1,\"message\":\"wrong hash\"},\"id\":" + strconv.FormatUint(req.ID, 10) + ",\"jsonrpc\":\"2.0\"}"))

				conn.Unlock()
				CurInfo.Unlock()
				continue
			}
		} else {
			logger.Dev("Skipping share PoW")
		}
		CurInfo.Unlock()

		if shareDiff < theJob.Diff {
			logger.Warn("INVALID SHARE RECEIVED: hash does not meet difficulty: expected at least", theJob.Diff, ", got", shareDiff)
			// not using conn.Send for better performance
			conn.SendBytes([]byte("{\"error\":{\"code\":-1,\"message\":\"hash does not meet diff\"},\"id\":" + strconv.FormatUint(req.ID, 10) + ",\"jsonrpc\":\"2.0\"}"))
			/*conn.Send(stratum.Reply{
				ID:      req.ID,
				Jsonrpc: "2.0",
				Error: &stratum.ErrorJson{
					Code:    -1,
					Message: "hash does not meet diff",
				},
			})*/
			conn.Unlock()
			continue
		}

		// Share is valid!

		conn.Score += 1

		logger.Info("Share:", connAddress, "diff", theJob.Diff)

		if util.RandomFloat() > float32(1-(config.Cfg.SlaveConfig.SlaveFee/100)) {
			slave.SendShare(config.Cfg.FeeAddress, theJob.Diff)
		} else if conn.IsTls || util.RandomFloat() > 0.001 {
			slave.SendShare(connAddress, theJob.Diff)
		} else {
			slave.SendShare(config.Cfg.FeeAddress, theJob.Diff)
		}

		if config.Cfg.UseP2Pool && shareDiff > conn.P2Pool.JobDiff {
			// this share is a valid p2pool solution. Hooray!

			logger.Info("Found P2Pool share")

			res, err := conn.P2Pool.SubmitShare(resultNonce, theJob.JobID, req.Params.Result)
			logger.Info("res and err:", res, err)

			CurInfo.RLock()
			slave.SendShareFound(CurInfo.Height)
			CurInfo.RUnlock()
		}
		if !config.Cfg.UseP2Pool && shareDiff >= CurInfo.Difficulty {
			// this share is a valid block. Hooray!

			CurInfo.Lock()
			logger.Info("Found block at height", CurInfo.FutureHeight)
			logger.Info("difficulty:", CurInfo.Difficulty)
			logger.Debug("hashing blob:", resultHashingBlobString)

			var res *daemon.SubmitBlockResult
			res, err = client.SubmitBlock(context.Background(), hex.EncodeToString(resultBlockBlob))
			sigc <- os.Signal(SIGUSR1{})

			if err != nil {
				logger.Error("Failed submitting block:", err)
			} else {
				logger.Info("SubmitBlock response:", util.DumpJson(res))

				if len(res.BlockId) == 64 { // After commit 30ba5a52801b1b8aa6e4f2f59a7ecd711a66459a
					blockHash, err := hex.DecodeString(res.BlockId)
					if err != nil {
						logger.Error(err)
						conn.Unlock()
						CurInfo.Unlock()
						continue
					}
					slave.SendBlockFound(CurInfo.FutureHeight, CurInfo.BlockReward, blockHash)
				} else { // Older Monero forks
					time.Sleep(500 * time.Millisecond)
					blockHeader, err := client.GetBlockHeaderByHeight(context.Background(), CurInfo.FutureHeight)
					if err != nil {
						logger.Error(err)
						conn.Unlock()
						CurInfo.Unlock()
						continue
					}
					blockHash, err := hex.DecodeString(blockHeader.BlockHeader.Hash)
					if err != nil {
						logger.Error(err)
						conn.Unlock()
						CurInfo.Unlock()
						continue
					}
					slave.SendBlockFound(CurInfo.FutureHeight, CurInfo.BlockReward, blockHash)
				}
			}
			CurInfo.Unlock()
		}

		// Try updating the diff

		t := time.Now().UnixMilli()
		deltaT := t - conn.LastShare
		if deltaT < int64(config.Cfg.SlaveConfig.ShareTargetTime*1000/4) {
			deltaT = int64(config.Cfg.SlaveConfig.ShareTargetTime * 1000 / 4)
		} else if deltaT > int64(config.Cfg.SlaveConfig.ShareTargetTime*1000*4) {
			deltaT = int64(config.Cfg.SlaveConfig.ShareTargetTime * 1000 * 4)
		}
		estHr := float64(theJob.Diff) / float64(deltaT)
		nextDiff := estHr * 1000 * float64(config.Cfg.SlaveConfig.ShareTargetTime)
		nextDiff = (nextDiff + 6*conn.NextDiff) / 7
		logger.Debug("Next Diff:", nextDiff)
		conn.NextDiff = nextDiff

		conn.LastShare = time.Now().UnixMilli()

		conn.Unlock()

		// not using conn.Send for better performance
		conn.SendBytes([]byte("{\"id\":" + strconv.FormatUint(req.ID, 10) + ",\"jsonrpc\":\"2.0\",\"result\":{\"status\":\"OK\"}}"))
		/*conn.Send(stratum.Reply{
			ID:      req.ID,
			Jsonrpc: "2.0",
			Result: map[string]any{
				"status": "OK",
			},
		})*/
	}
}

func GetJob(jobDiff uint64) (j *template.Job, blocktemplateBlob []byte, err error) {

	tmpl, err := client.GetBlockTemplate(context.Background(), daemon.GetBlockTemplateParams{
		WalletAddress: config.Cfg.PoolAddress,
		ExtraNonce:    hex.EncodeToString(util.RandomBytes(8)),
	})
	if err != nil {
		return
	}

	blocktemplateBlob, err = hex.DecodeString(tmpl.BlocktemplateBlob)
	if err != nil {
		return
	}

	CurInfo.Lock()
	CurInfo.SeedHash = tmpl.SeedHash
	CurInfo.Difficulty = uint64(tmpl.Difficulty)
	CurInfo.FutureHeight = uint64(tmpl.Height)
	CurInfo.BlockReward = uint64(tmpl.ExpectedReward)

	j = &template.Job{
		Algo:     config.Cfg.AlgoName,
		Blob:     tmpl.BlockhashingBlob,
		Height:   CurInfo.Height,
		JobID:    hex.EncodeToString(util.RandomBytes(8)),
		SeedHash: tmpl.SeedHash,
		Target:   template.DiffToShortTarget(jobDiff),
	}

	CurInfo.Unlock()

	return
}
