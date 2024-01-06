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
	"fmt"
	"go-pool/config"
	"go-pool/database"
	"go-pool/logger"
	"math"
	"strconv"

	"github.com/gin-gonic/gin"
	bolt "go.etcd.io/bbolt"
)

type UserWithdrawal struct {
	Amount float64 `json:"amount"`
	Txid   string  `json:"txid"`
}

type PubWithdraw struct {
	Txid         string  `json:"txid"`
	Timestamp    uint64  `json:"time"`
	Amount       float64 `json:"amount"`
	Destinations int     `json:"destinations"`
}

var Coin float64

func StartApiServer() {
	Coin = math.Pow10(config.Cfg.Atomic)

	gin.SetMode("release")
	r := gin.Default()
	r.SetTrustedProxies([]string{
		"127.0.0.1",
	})

	r.GET("/ping", func(c *gin.Context) {
		c.String(200, "pong")
	})

	r.GET("/stats", func(c *gin.Context) {
		c.Header("Cache-Control", "max-age=10")

		MasterInfo.RLock()
		defer MasterInfo.RUnlock()

		Stats.RLock()
		defer Stats.RUnlock()

		var ws = make([]PubWithdraw, 0, len(Stats.RecentWithdrawals))

		for _, v := range Stats.RecentWithdrawals {
			var x uint64
			for _, v2 := range v.Destinations {
				x += v2.Amount
			}

			ws = append(ws, PubWithdraw{
				Txid:         v.Txid,
				Timestamp:    v.Timestamp,
				Amount:       Round6(float64(x) / math.Pow10(config.Cfg.Atomic)),
				Destinations: len(v.Destinations),
			})
		}

		c.JSON(200, gin.H{
			"pool_hr":             Stats.PoolHashrate,
			"net_hr":              Stats.NetHashrate,
			"connected_addresses": len(Stats.KnownAddresses),
			"connected_workers":   Stats.Workers,
			"chart": gin.H{
				"hashrate":  Stats.PoolHashrateChart,
				"workers":   Stats.WorkersChart,
				"addresses": Stats.AddressesChart,
			},
			"num_blocks_found":    Stats.NumFound,
			"recent_blocks_found": Stats.BlocksFound,
			"height":              MasterInfo.Height,
			"last_block":          Stats.LastBlock,

			"reward": Round3(float64(MasterInfo.BlockReward) / math.Pow10(config.Cfg.Atomic)),

			"pplns_window_seconds": GetPplnsWindow(),
			"withdrawals":          ws,

			// stats that do not change

			"pool_fee_percent": config.Cfg.MasterConfig.FeePercent,
			// "stratums":          config.Cfg.MasterConfig.Stratums,
			"payment_threshold": config.Cfg.MasterConfig.MinWithdrawal,
		})
	})

	r.GET("/stats/:addr", func(c *gin.Context) {
		c.Header("Cache-Control", "max-age=10")

		addr := c.Param("addr")

		if addr == config.Cfg.PoolAddress {
			if c.RemoteIP() != "127.0.0.1" {
				c.JSON(404, gin.H{
					"error": gin.H{
						"code":    1, // address not found
						"message": "address not found",
					},
				})
				return
			}
		}

		addrInfo := database.AddrInfo{}

		err := DB.View(func(tx *bolt.Tx) error {
			buck := tx.Bucket(database.ADDRESS_INFO)

			addrData := buck.Get([]byte(addr))
			if addrData == nil {
				return fmt.Errorf("unknown address %s", addr)
			}

			return addrInfo.Deserialize(addrData)
		})

		if err != nil {
			logger.Debug(err)
		}

		Stats.RLock()
		defer Stats.RUnlock()

		uw := []UserWithdrawal{}

		for _, v := range Stats.RecentWithdrawals {
			for _, v2 := range v.Destinations {
				if v2.Address == addr {
					uw = append(uw, UserWithdrawal{
						Amount: float64(v2.Amount) / Coin,
						Txid:   v.Txid,
					})
				}
			}

		}

		c.JSON(200, gin.H{
			"hashrate_5m":     NotNan(Round0(Get5mHashrate(addr))),
			"hashrate_10m":    NotNan(Round0(Get15mHashrate(addr))),
			"hashrate_15m":    NotNan(Round0(Get15mHashrate(addr))),
			"balance":         NotNan(Round6(float64(addrInfo.Balance) / Coin)),
			"balance_pending": NotNan(Round6(float64(addrInfo.BalancePending) / Coin)),
			"paid":            NotNan(Round6(float64(addrInfo.Paid) / Coin)),
			"est_pending":     NotNan(Round6(GetEstPendingBalance(addr))),
			"hr_chart":        Stats.HashrateCharts[addr],
			"withdrawals":     uw,
		})
	})

	r.GET("/info", func(c *gin.Context) {
		c.Header("Cache-Control", "max-age=3600")
		c.JSON(200, gin.H{
			"pool_fee_percent":  config.Cfg.MasterConfig.FeePercent,
			"stratums":          config.Cfg.MasterConfig.Stratums,
			"payment_threshold": config.Cfg.MasterConfig.MinWithdrawal,
		})
	})

	err := r.Run("0.0.0.0:" + strconv.FormatInt(int64(config.Cfg.MasterConfig.ApiPort), 10))
	if err != nil {
		panic(err)
	}
}

func NotNan(n float64) float64 {
	if math.IsNaN(n) {
		return 0
	}
	return n
}

func Round0(n float64) float64 {
	return math.Round(n)
}
func Round3(n float64) float64 {
	return math.Round(n*1000) / 1000
}
func Round6(n float64) float64 {
	return math.Round(n*1000000) / 1000000
}
