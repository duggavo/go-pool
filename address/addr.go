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

package address

import (
	"bytes"

	"go-pool/config"
	"go-pool/logger"

	"golang.org/x/crypto/sha3"
)

func IsAddressValid(addr string) bool {
	decoded := decodeBase58(addr)
	if len(decoded) < 64+4+1 {
		return false
	}

	data := decoded[:len(decoded)-4]
	checksum := decoded[len(decoded)-4:]

	if !bytes.Equal(getChecksum(data), checksum) {
		logger.Debug("Address not valid: checksum doesn't match")
		return false
	}

	prefix := data[:len(data)-(64)]

	if bytes.Equal(data[:len(config.Cfg.AddrPrefix)], config.Cfg.AddrPrefix) {
		return len(data) == len(config.Cfg.AddrPrefix)+64
	} else if bytes.Equal(data[:len(config.Cfg.SubaddrPrefix)], config.Cfg.SubaddrPrefix) {
		return len(data) == len(config.Cfg.SubaddrPrefix)+64
	} else {
		logger.Debug("Address not valid: invalid prefix", prefix)
		return false
	}
}

func getChecksum(data []byte) []byte {
	k := sha3.NewLegacyKeccak256()

	k.Write(data)
	x := make([]byte, 0, 32)
	x = k.Sum(x)

	return x[:4]
}
