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
	"fmt"
	"go-pool/config"
	"go-pool/database"
	"go-pool/logger"
	"go-pool/util"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/duggavo/go-monero/rpc/wallet"
	bolt "go.etcd.io/bbolt"
)

func Updater() {
	go func() {
		for {
			go Withdraw()

			time.Sleep(time.Duration(config.Cfg.MasterConfig.WithdrawInterval) * time.Minute)
		}
	}()

	for {
		time.Sleep(5 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		info, err := DaemonRpc.GetInfo(ctx)
		cancel()
		if err != nil {
			logger.Warn(err)
			continue
		}

		MasterInfo.Lock()
		if info.Height != MasterInfo.Height {
			logger.Info("New height", MasterInfo.Height, "->", info.Height)
			MasterInfo.Height = info.Height
			Stats.NetHashrate = float64(info.Difficulty) / float64(config.BlockTime)
			config.BlockTime = info.Target

			MasterInfo.Unlock()

			go func() {
				updated := CheckWithdraw()
				if updated {
					logger.Info("CheckWithdraw(): some balances have been updated")
				} else {
					logger.Debug("CheckWithdraw(): no balances have been updated")
				}
			}()
			go UpdatePendingBals()
		} else {
			MasterInfo.Unlock()
		}

		go UpdateReward()
	}
}

func UpdateReward() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	info, err := DaemonRpc.GetLastBlockHeader(ctx)
	cancel()
	if err != nil {
		logger.Warn(err)
		return
	}

	MasterInfo.Lock()
	defer MasterInfo.Unlock()
	MasterInfo.BlockReward = info.BlockHeader.Reward
}

var minHeight uint64
var minHeightMut sync.RWMutex

func UpdatePendingBals() {
	logger.Debug("Updating user balances")

	curAddy := config.Cfg.PoolAddress

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	indices, err := WalletRpc.GetAddressIndex(ctx, curAddy)
	if err != nil {
		logger.Warn(err)
		cancel()
		return
	}
	minHeightMut.Lock()
	MasterInfo.Lock()
	if minHeight == 0 && MasterInfo.Height > 720 {
		minHeight = MasterInfo.Height - 720
	}
	MasterInfo.Unlock()
	transfers, err := WalletRpc.GetTransfers(ctx, wallet.GetTransfersParams{
		In: true,

		AccountIndex:   indices.Index.Major,
		SubaddrIndices: []uint{indices.Index.Minor},

		FilterByHeight: true,
		MinHeight:      minHeight,
	})
	minHeightMut.Unlock()
	cancel()
	if err != nil {
		logger.Warn(err)
		return
	}

	// Sort transfers by ascending height
	sort.SliceStable(transfers.In, func(i, j int) bool {
		return transfers.In[i].Height < transfers.In[j].Height
	})

	logger.Dev("sorted transfers", util.DumpJson(transfers.In))

	err = DB.Update(func(tx *bolt.Tx) error {
		pendingBuck := tx.Bucket(database.PENDING)
		pendingData := pendingBuck.Get([]byte("pending"))

		pending := database.PendingBals{
			UnconfirmedTxs: make([]database.UnconfTx, 0, 10),
		}

		if pendingData == nil {
			logger.Debug("pending is nil")
		} else {
			err := pending.Deserialize(pendingData)
			if err != nil {
				logger.Error(err)
				return err
			}
		}

		minHeightMut.Lock()
		if pending.LastHeight > 720 {
			minHeight = pending.LastHeight - 720
		}
		minHeightMut.Unlock()

		var totalPendings = make(map[string]uint64)

		nextHeight := pending.LastHeight

		for _, vt := range transfers.In {
			if vt.Height > pending.LastHeight {
				logger.Dev("transfer is fine! adding unconfirmed balance to it")

				rewardNoFee := float64(vt.Amount)
				logger.Debug("reward before fee is", rewardNoFee)
				reward := rewardNoFee * (100 - config.Cfg.MasterConfig.FeePercent) / 100
				logger.Debug("reward after fee is", reward)

				var totHashes float64
				var minersTotalHashes = make(map[string]float64, 10)

				buck := tx.Bucket(database.SHARES)

				c := buck.Cursor()

				for key, v := c.First(); key != nil; key, v = c.Next() {
					sh := database.Share{}

					err := sh.Deserialize(v)

					if err != nil {
						logger.Error("error reading share:", err)
						continue
					}

					Stats.RLock()
					// delete outdated shares
					if sh.Time+GetPplnsWindow() < util.Time() {
						Stats.RUnlock()
						buck.Delete(key)
						continue
					}
					Stats.RUnlock()

					totHashes += float64(sh.Diff)
					minersTotalHashes[sh.Wallet] += float64(sh.Diff)

					logger.Dev("block payout: adding share", sh.Wallet, ",", sh.Diff)

					fmt.Printf("key=%x, value=%x\n", key, v)
				}

				txHashBin, err := hex.DecodeString(vt.Txid)
				if err != nil {
					logger.Error(err)
					return err
				}
				if len(txHashBin) != 32 {
					logger.Error("transaction hash length is not 32 bytes!", vt.Txid)
					return fmt.Errorf("tx hash length is not 32 bytes")
				}

				pendBals := database.UnconfTx{
					UnlockHeight: vt.Height + config.Cfg.MinConfs + 1,
					Bals:         make(map[string]uint64),
					TxnHash:      [32]byte(txHashBin),
				}

				var minersBalances = make(map[string]float64, len(minersTotalHashes))
				var totalRewarded uint64 // totalReward is slightly smaller than rewardNoFee because of rounding errors
				for i, v := range minersTotalHashes {
					minersBalances[i] = v * float64(reward) / totHashes

					x := uint64(v * float64(reward) / totHashes)
					pendBals.Bals[i] = x

					totalPendings[i] += x

					totalRewarded += x
				}
				pendBals.Bals[config.Cfg.FeeAddress] += uint64(rewardNoFee) - totalRewarded

				totalPendings[config.Cfg.FeeAddress] += pendBals.Bals[config.Cfg.FeeAddress]

				logger.Debug("Fee wallet has earned", (rewardNoFee-float64(totalRewarded))/math.Pow10(config.Cfg.Atomic))

				logger.Dev("total hashes", util.DumpJson(minersTotalHashes))
				logger.Dev("balances", util.DumpJson(minersBalances))

				if pending.UnconfirmedTxs == nil {
					pending.UnconfirmedTxs = make([]database.UnconfTx, 0, 10)
				}

				pending.UnconfirmedTxs = append(pending.UnconfirmedTxs, pendBals)

				if vt.Height > nextHeight {
					MasterInfo.RLock()
					nextHeight = MasterInfo.Height
					MasterInfo.RUnlock()
				}

				logger.Dev("Adding transfer DONE")
			} else {
				logger.Dev("transfer is too old - gotta ignore it - pending.LastHeight is", pending.LastHeight, "txn Height is", vt.Height)
			}
		}

		pending.LastHeight = nextHeight

		infoBuck := tx.Bucket(database.ADDRESS_INFO)

		for i, v := range totalPendings {
			addrInfoBin := infoBuck.Get([]byte(i))

			addrInfo := database.AddrInfo{}

			if addrInfoBin == nil {
				logger.Dev("addrInfo is nil")
			} else {
				err = addrInfo.Deserialize(addrInfoBin)
				if err != nil {
					logger.Warn(err)
					continue
				}
			}

			addrInfo.BalancePending = v

			infoBuck.Put([]byte(i), addrInfo.Serialize())

		}

		return pendingBuck.Put([]byte("pending"), pending.Serialize())

	})

	if err != nil {
		logger.Error(err)
	}

}
