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
	"time"
)

type PendingBals struct {
	Height uint64            `json:"height"`
	Hash   string            `json:"hash"`
	Bals   map[string]uint64 `json:"bals"`
}

func OnBlockFound(height, rew uint64, hash string) {
	Stats.Lock()
	defer Stats.Unlock()

	Stats.LastBlock = LastBlock{
		Height:    height,
		Timestamp: time.Now().Unix(),
		Reward:    rew,
		Hash:      hash,
	}

	Stats.BlocksFound = append([]FoundInfo{{
		Height: height,
		Hash:   hash,
	}}, Stats.BlocksFound...)

	Stats.NumFound++
	Stats.Cleanup()
}

func OnP2PoolShareFound(height uint64) {
	Stats.Lock()
	defer Stats.Unlock()
	Stats.LastBlock = LastBlock{
		Height:    height,
		Timestamp: time.Now().Unix(),
	}
	Stats.NumFound++
	Stats.Cleanup()
}
