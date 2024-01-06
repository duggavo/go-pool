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
	"go-pool/template"
	"go-pool/util"
	"sync"

	"github.com/duggavo/go-monero/rpc/daemon"
)

type NicehashJob struct {
	HashingBlob  []byte // the blockhashing blob
	TemplateBlob []byte // the blocktemplate blob
	Height       uint64 // only used in RandomX jobs
	SeedHash     string // only used in RandomX jobs
	LastNonce    byte
}

var CurrentNicehashJob NicehashJob
var NicehashMutex sync.RWMutex

func GetNicehashJob(jobDiff uint64) (j *template.Job, blocktemplateBlob []byte, nicehashByte byte, err error) {
	NicehashMutex.Lock()
	defer NicehashMutex.Unlock()

	if CurrentNicehashJob.LastNonce == 255 || CurrentNicehashJob.LastNonce == 0 {
		logger.Debug("Generating new Nicehash Job")

		var tmpl *daemon.GetBlockTemplateResult
		tmpl, err = client.GetBlockTemplate(context.Background(), daemon.GetBlockTemplateParams{
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
		CurInfo.Unlock()

		var blobBin []byte
		blobBin, err = hex.DecodeString(tmpl.BlockhashingBlob)
		if err != nil {
			return
		}

		nhJob := NicehashJob{
			HashingBlob:  blobBin,
			TemplateBlob: blocktemplateBlob,
			Height:       tmpl.Height,
			SeedHash:     tmpl.SeedHash,
			LastNonce:    1,
		}
		CurrentNicehashJob = nhJob

		blobBinNonce := blobBin

		blobBinNonce[42] = 1
		nicehashByte = 1

		j = &template.Job{
			Algo:     config.Cfg.AlgoName,
			Blob:     hex.EncodeToString(blobBinNonce),
			Height:   tmpl.Height,
			JobID:    hex.EncodeToString(util.RandomBytes(8)),
			SeedHash: tmpl.SeedHash,
			Target:   template.DiffToShortTarget(jobDiff),
		}
	} else {
		logger.Debug("Reusing Nicehash Job")

		CurrentNicehashJob.LastNonce += 1

		blobBinNonce := CurrentNicehashJob.HashingBlob

		blobBinNonce[42] = CurrentNicehashJob.LastNonce
		nicehashByte = CurrentNicehashJob.LastNonce
		blocktemplateBlob = CurrentNicehashJob.TemplateBlob

		j = &template.Job{
			Algo:     config.Cfg.AlgoName,
			Blob:     hex.EncodeToString(blobBinNonce),
			Height:   CurrentNicehashJob.Height,
			JobID:    hex.EncodeToString(util.RandomBytes(8)),
			SeedHash: CurrentNicehashJob.SeedHash,
			Target:   template.DiffToShortTarget(jobDiff),
		}
	}

	return
}
func ClearNicehashJob() {
	NicehashMutex.Lock()
	defer NicehashMutex.Unlock()

	CurrentNicehashJob.LastNonce = 0
}
