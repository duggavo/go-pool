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
	"go-pool/config"
)

// returns the current PPLNS window duration in seconds
// Stats MUST be Rlocked before calling this
func GetPplnsWindow() uint64 {
	blockFoundInterval := Stats.NetHashrate / Stats.PoolHashrate * float64(config.BlockTime)

	if blockFoundInterval == 0 {
		return 2 * 3600 * 24
	}

	// PPLNS window is double of average pool block found time
	blockFoundInterval *= 2

	// PPLNS window is at most 2 days
	if blockFoundInterval > 2*3600*24 {
		return 2 * 3600 * 24
	}

	// PPLNS window is at most 2 times the block time
	if blockFoundInterval < float64(config.BlockTime)*2 {
		return config.BlockTime * 2
	}

	return uint64(blockFoundInterval)
}
