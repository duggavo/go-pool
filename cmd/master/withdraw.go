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
	"go-pool/database"
	"go-pool/logger"
	"go-pool/util"
	"math"
	"time"

	"github.com/duggavo/go-monero/rpc"
	"github.com/duggavo/go-monero/rpc/daemon"
	"github.com/duggavo/go-monero/rpc/wallet"
	bolt "go.etcd.io/bbolt"
)

var WalletRpc *wallet.Client
var DaemonRpc *daemon.Client

func StartWallet() {
	/*httpClient, err := http.NewClient(http.ClientConfig{
		Username: config.WALLET_RPC_USERNAME,
		Password: config.WALLET_RPC_PASSWORD,
	})
	if err != nil {
		panic(err)
	}*/

	client, err := rpc.NewClient(config.Cfg.MasterConfig.WalletRpc) // rpc.WithHTTPClient(httpClient)
	if err != nil {
		panic(err)
	}

	WalletRpc = wallet.NewClient(client)
}

// If it returns false, there is an error (or nothing changed)
func CheckWithdraw() bool {
	logger.Debug("CheckWithdraw()")
	defer logger.Debug("CheckWithdraw() ended")

	balancesChanged := false

	err := DB.Update(func(tx *bolt.Tx) error {
		pendingBuck := tx.Bucket(database.PENDING)
		pendingBin := pendingBuck.Get([]byte("pending"))

		if pendingBin == nil {
			logger.Dev("pendingBin is nil - there are no pending transactions")
		}

		pending := database.PendingBals{}

		err := pending.Deserialize(pendingBin)
		if err != nil {
			logger.Error(err)
			return err
		}

		if len(pending.UnconfirmedTxs) == 0 {
			logger.Dev("len(pending.UnconfirmedTxs) is 0")
			return nil
		}

		MasterInfo.RLock()
		if pending.UnconfirmedTxs[0].UnlockHeight < MasterInfo.Height {
			MasterInfo.RUnlock()
			logger.Info("pending block should have enough confirmations")

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			txns, err := DaemonRpc.GetTransactions(ctx, []string{hex.EncodeToString(pending.UnconfirmedTxs[0].TxnHash[:])})
			cancel()
			if err != nil {
				logger.Warn(err)
				return err
			}

			if len(txns.Txs) < 1 {
				logger.Warn("txns.Txs length is zero! probably found block is orphaned")
				pending.UnconfirmedTxs = pending.UnconfirmedTxs[1:]
				// TODO: save pending balances
				return nil
			}

			if txns.Txs[0].InPool || txns.Txs[0].BlockHeight == 0 {
				logger.Warn("PendingBalancesP2Pool.UnconfirmedTxs[0] is in pool - removing it, as this should not happen! Tx height is:", txns.Txs[0].BlockHeight)
				pending.UnconfirmedTxs = pending.UnconfirmedTxs[1:]
				// TODO: save pending balances
				return nil
			}

			infoBuck := tx.Bucket(database.ADDRESS_INFO)
			for i, v := range pending.UnconfirmedTxs[0].Bals {
				wallInfoBin := infoBuck.Get([]byte(i))

				addrInfo := database.AddrInfo{}

				if wallInfoBin == nil {
					logger.Debug("wallInfoBin is nil")
				} else {
					err := addrInfo.Deserialize(wallInfoBin)
					if err != nil {
						logger.Error(err)
						return err
					}
				}

				addrInfo.Balance += v

				err = infoBuck.Put([]byte(i), addrInfo.Serialize())
				if err != nil {
					return err
				}
			}

			if len(pending.UnconfirmedTxs) > 1 {
				pending.UnconfirmedTxs = pending.UnconfirmedTxs[1:]
			} else {
				pending.UnconfirmedTxs = []database.UnconfTx{}
			}

			// TODO: save the pending balances

			balancesChanged = true
			return pendingBuck.Put([]byte("pending"), pending.Serialize())
		} else {
			MasterInfo.RUnlock()
			logger.Dev("pending.UnconfirmedTxs[0] not confirmed yet")
			return nil
		}
	})
	if err != nil {
		logger.Error(err)
	}

	return balancesChanged
}

const MIN_WITHDRAW_DESTINATIONS = 1
const MAX_WITHDRAW_DESTINATIONS = 8

func Withdraw() {
	DB.Update(func(tx *bolt.Tx) error {
		buck := tx.Bucket(database.ADDRESS_INFO)

		var destinations []wallet.Destination
		var feeRevenue uint64

		curs := buck.Cursor()

		for key, val := curs.First(); key != nil; key, val = curs.Next() {
			address := string(key)
			logger.Dev("Withdraw: iterating over addresses. Current address is", address)

			addrInfo := database.AddrInfo{}

			err := addrInfo.Deserialize(val)
			if err != nil {
				logger.Error(err)
				return err
			}

			logger.Debug("Address has balance", float64(addrInfo.Balance)/math.Pow10(config.Cfg.Atomic))

			if addrInfo.Balance > uint64(config.Cfg.MasterConfig.MinWithdrawal*math.Pow10(config.Cfg.Atomic)) {
				fee := uint64(config.Cfg.MasterConfig.WithdrawalFee * math.Pow10(config.Cfg.Atomic))

				destinations = append(destinations, wallet.Destination{
					Address: address,
					Amount:  addrInfo.Balance - fee,
				})
				feeRevenue += fee

				addrInfo.Paid += addrInfo.Balance

				addrInfo.Balance = 0

				err = buck.Put(key, addrInfo.Serialize())
				if err != nil {
					logger.Error(err)
					return err
				}
			}

			if len(destinations) >= MAX_WITHDRAW_DESTINATIONS {
				break
			}
		}

		if len(destinations) < MIN_WITHDRAW_DESTINATIONS {
			logger.Warn("Not enough destinations for withdrawal")
			return nil
		}

		logger.Info("Transferring to destinations", destinations)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		data, err := WalletRpc.Transfer(ctx, wallet.TransferParameters{
			Destinations:  destinations,
			DoNotRelay:    true,
			GetTxMetadata: true,
		})
		cancel()
		if err != nil {
			logger.Error(err)
			return (err)
		}
		logger.Dev("Transfer result", data)

		Stats.Lock()
		Stats.RecentWithdrawals = append([]Withdrawal{
			{
				Txid:         data.TxHash,
				Timestamp:    util.Time(),
				Destinations: destinations,
			},
		}, Stats.RecentWithdrawals...)
		Stats.Unlock()

		var txnFee uint64 = data.Fee

		logger.Info("Payout txs total fee", float64(txnFee)/math.Pow10(config.Cfg.Atomic))
		logger.Info("Payout revenue fee  ", float64(feeRevenue)/math.Pow10(config.Cfg.Atomic))
		logger.Info("Earned ", float64(feeRevenue-txnFee)/math.Pow10(config.Cfg.Atomic))

		if txnFee >= feeRevenue {
			logger.Warn("Payout txs total fee is bigger than the revenue fee. Consider increasing withdrawal_fee.")
			feeRevenue = 0
		} else {
			feeRevenue -= txnFee
		}

		feeAddrData := buck.Get([]byte(config.Cfg.FeeAddress))
		feeAddr := database.AddrInfo{}

		if feeAddrData != nil {
			err := feeAddr.Deserialize(feeAddrData)
			if err != nil {
				logger.Error(err)
				return err
			}
		}

		feeAddr.Balance += feeRevenue

		result, err := WalletRpc.RelayTx(context.Background(), data.TxMetadata)
		if err != nil {
			logger.Error("Failed to relay tx "+data.TxHash+":", err)
			// return err
		} else {
			logger.Info("Relayed tx with hash " + result.TxHash)
		}

		return nil
	})
}
