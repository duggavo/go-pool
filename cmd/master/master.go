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
	"go-pool/address"
	"go-pool/config"
	"go-pool/database"
	"go-pool/logger"
	"go-pool/util"
	"math"
	"net"
	"sync"

	"github.com/duggavo/go-monero/rpc"
	"github.com/duggavo/go-monero/rpc/daemon"
	bolt "go.etcd.io/bbolt"
)

type Info struct {
	BlockReward uint64
	Height      uint64

	sync.RWMutex
}

var MasterInfo Info

var DB *bolt.DB

func main() {
	if !address.IsAddressValid(config.Cfg.PoolAddress) || !address.IsAddressValid(config.Cfg.FeeAddress) {
		logger.Fatal("Pool or fee address are not valid")
	}

	var err error
	DB, err = bolt.Open("pool.db", 0o600, bolt.DefaultOptions)

	if err != nil {
		logger.Fatal(err)
	}

	err = DB.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(database.ADDRESS_INFO)
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists(database.PENDING)
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists(database.SHARES)

		return err
	})
	if err != nil {
		logger.Fatal(err)
	}

	DatabaseCleanup()

	StartWallet()

	srv, err := net.Listen("tcp", config.Cfg.MasterConfig.ListenAddress)
	if err != nil {
		panic(err)
	}
	logger.Info("Master server listening on", config.Cfg.MasterConfig.ListenAddress)

	rpcClient, err := rpc.NewClient(config.Cfg.DaemonRpc)
	if err != nil {
		panic(err)
	}
	DaemonRpc = daemon.NewClient(rpcClient)
	logger.Info("Using daemon RPC " + config.Cfg.DaemonRpc)

	go StartApiServer()
	go StatsServer()

	go Updater()

	for {
		conn, err := srv.Accept()
		if err != nil {
			logger.Error(err)
		}
		go HandleSlave(conn)
	}
}

func DatabaseCleanup() {
	logger.Info("Starting database cleanup")

	var sharesRemoved = 0
	var sharesKept = 0

	err := DB.Update(func(tx *bolt.Tx) error {
		buck := tx.Bucket(database.SHARES)

		cursor := buck.Cursor()

		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			sh := database.Share{}

			err := sh.Deserialize(v)

			if err != nil {
				logger.Warn("error reading share:", err)
				buck.Delete(k)
				sharesRemoved++
				continue
			}

			// delete outdated shares
			if sh.Time+GetPplnsWindow() < util.Time() {
				buck.Delete(k)
				sharesRemoved++
				continue
			}

			sharesKept++
		}

		return nil
	})
	if err != nil {
		logger.Error(err)
	}

	logger.Info("Database cleanup OK,", sharesRemoved, "outdated shares removed,", sharesKept, "mantained")
}

func OnShareFound(wallet string, diff uint64, numShares uint32) {
	logger.Info("Wallet", wallet, "found", numShares, "shares with diff", float64(diff/100)/10, "k HR:", Get5mHashrate(wallet))

	if !address.IsAddressValid(wallet) {
		logger.Warn("Wallet", wallet, "is not valid. Replacing it with fee address.")
		wallet = config.Cfg.FeeAddress
	}

	Stats.Lock()
	Stats.Shares = append(Stats.Shares, StatsShare{
		Count:  numShares,
		Wallet: wallet,
		Diff:   diff,
		Time:   util.Time(),
	})
	Stats.Cleanup()
	Stats.Unlock()

	DB.Update(func(tx *bolt.Tx) error {
		buck := tx.Bucket(database.SHARES)

		shareId, _ := buck.NextSequence()

		shareData := database.Share{
			Wallet: wallet,
			Diff:   diff,
			Time:   util.Time(),
		}

		buck.Put(util.Itob(shareId), shareData.Serialize())

		return nil
	})
}

// Important: Stats must be locked
func GetEstPendingBalance(addr string) float64 {

	var totHashes float64
	var minerHashes float64

	/*SharesMut.RLock()
	UpdateShares()

	for _, v := range Shares {
		totHashes += float64(v.Diff)
		if v.Wallet == addr {
			minerHashes += float64(v.Diff)
		}
	}
	SharesMut.RUnlock()*/

	MasterInfo.RLock()
	reward := float64(MasterInfo.BlockReward) / math.Pow10(config.Cfg.Atomic)
	MasterInfo.RUnlock()

	if config.Cfg.UseP2Pool {
		reward = 0
	}

	balance := minerHashes / totHashes * reward

	return balance
}
