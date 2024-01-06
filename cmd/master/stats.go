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
	"encoding/json"
	"go-pool/logger"
	"go-pool/util"
	"math"
	"os"
	"sync"
	"time"

	"github.com/duggavo/go-monero/rpc/wallet"
)

const STATS_INTERVAL = 15 // Minutes
const NUM_CHART_DATA = (60 * 24 / STATS_INTERVAL)

type LastBlock struct {
	Height    uint64 `json:"height"`
	Timestamp int64  `json:"timestamp"`
	Reward    uint64 `json:"reward"`
	Hash      string `json:"hash"`
}

type StatsShare struct {
	Count  uint32 `json:"count"`
	Wallet string `json:"wall"`
	Diff   uint64 `json:"diff"`
	Time   uint64 `json:"time"`
}

type Statistics struct {
	LastUpdate int64

	PoolHashrate      float64
	PoolHashrateChart []Hr
	HashrateCharts    map[string][]Hr

	Shares []StatsShare

	LastBlock LastBlock

	BlocksFound []FoundInfo
	NumFound    int32

	NetHashrate float64

	KnownAddresses map[string]uint64

	RecentWithdrawals []Withdrawal

	Workers        uint32 // the current number of miners
	WorkersChart   []uint32
	AddressesChart []uint32

	sync.RWMutex
}

type Withdrawal struct {
	Txid         string `json:"txid"`
	Timestamp    uint64 `json:"time"`
	Destinations []wallet.Destination
}

type Hr struct {
	Time     int64   `json:"t"`
	Hashrate float64 `json:"h"`
}

type FoundInfo struct {
	Height uint64 `json:"height"`
	Hash   string `json:"hash"`
}

var Stats = Statistics{
	HashrateCharts: make(map[string][]Hr),
	KnownAddresses: make(map[string]uint64, NUM_CHART_DATA),
}

func StatsServer() {
	statsData, err := os.ReadFile("stats.json")
	if err != nil {
		logger.Warn(err)
	} else {
		Stats.Lock()

		err := json.Unmarshal(statsData, &Stats)
		if err != nil {
			logger.Warn(err)
		}

		Stats.Unlock()
	}

	for {
		time.Sleep(100 * time.Millisecond)

		func() {
			Stats.Lock()
			defer Stats.Unlock()

			if time.Now().Unix()-Stats.LastUpdate < STATS_INTERVAL*60 {
				return
			} else if time.Now().Unix()-Stats.LastUpdate > (STATS_INTERVAL*60)*10 {
				Stats.LastUpdate = time.Now().Unix() - STATS_INTERVAL*60
				return
			}

			logger.Info("Updating stats")

			Stats.Cleanup()

			Stats.LastUpdate += STATS_INTERVAL * 60

			var totHr float64 = 0

			if Stats.HashrateCharts == nil {
				Stats.HashrateCharts = make(map[string][]Hr, 20)
			}

			for i := range Stats.KnownAddresses {
				hr := Get15mHashrate(i)
				totHr += hr

				Stats.HashrateCharts[i] = append(Stats.HashrateCharts[i], Hr{
					Time:     Stats.LastUpdate,
					Hashrate: hr,
				})

				for len(Stats.HashrateCharts[i]) > NUM_CHART_DATA {
					Stats.HashrateCharts[i] = Stats.HashrateCharts[i][1:]
				}

				didFind := false
				for _, v := range Stats.HashrateCharts[i] {
					if v.Hashrate != 0 {
						didFind = true
					}
				}
				if !didFind {
					delete(Stats.KnownAddresses, i)
					delete(Stats.HashrateCharts, i)
				}
			}

			Stats.WorkersChart = append(Stats.WorkersChart, Stats.Workers)
			Stats.AddressesChart = append(Stats.AddressesChart, uint32(len(Stats.KnownAddresses)))
			Stats.PoolHashrateChart = append(Stats.PoolHashrateChart, Hr{
				Time:     Stats.LastUpdate,
				Hashrate: Round0(totHr),
			})
			for len(Stats.PoolHashrateChart) > NUM_CHART_DATA {
				Stats.PoolHashrateChart = Stats.PoolHashrateChart[1:]
			}
			for len(Stats.WorkersChart) > NUM_CHART_DATA {
				Stats.WorkersChart = Stats.WorkersChart[1:]
			}
			for len(Stats.AddressesChart) > NUM_CHART_DATA {
				Stats.AddressesChart = Stats.AddressesChart[1:]
			}
		}()
	}
}

// Stats MUST be at least RLocked
func Get5mHashrate(wallet string) float64 {
	var numHashes float64
	for _, v := range Stats.Shares {
		deltaT := util.Time() - v.Time
		if v.Wallet == wallet && deltaT <= 5*60 {
			numHashes += float64(v.Diff)
		}
	}
	return math.Round(numHashes / (5 * 60))
}

// Stats MUST be at least RLocked
func Get15mHashrate(wallet string) float64 {
	var numHashes float64
	for _, v := range Stats.Shares {
		deltaT := util.Time() - v.Time
		if v.Wallet == wallet && deltaT <= 15*60 {
			numHashes += float64(v.Diff)
		}
	}
	return math.Round(numHashes / (15 * 60))
}

// Removes shares older than 15 minutes. Also updates the Pool Hashrate in stats, and saves the stats.
// Stats must be locked.
func (s *Statistics) Cleanup() {
	shares2 := make([]StatsShare, 0, len(s.Shares))
	var totalHashes float64 = 0

	for _, v := range s.Shares {
		// share isn't outdated
		if v.Time+(15*60) >= util.Time() {
			shares2 = append(shares2, v)
			totalHashes += float64(v.Diff)
		}

		s.KnownAddresses[v.Wallet] = v.Time
	}

	kaddr := make(map[string]uint64, len(s.KnownAddresses))

	// clean up known addresses
	for i, v := range s.KnownAddresses {
		if v+3600*24 > util.Time() { // address is not out of date
			kaddr[i] = v
		}
	}

	s.KnownAddresses = kaddr

	s.Shares = shares2
	s.PoolHashrate = math.Round(totalHashes / (15 * 60))

	data, err := json.Marshal(s)
	if err != nil {
		logger.Error(err)
		return
	}

	// only keep the last 40 blocks found
	for len(s.BlocksFound) > 40 {
		s.BlocksFound = s.BlocksFound[:len(s.BlocksFound)-2]
	}

	// only keep the last 40 withdrawal transactions
	for len(s.RecentWithdrawals) > 40 {
		s.RecentWithdrawals = s.RecentWithdrawals[:len(s.RecentWithdrawals)-2]
	}

	os.WriteFile("stats.json", data, 0o600)
}
