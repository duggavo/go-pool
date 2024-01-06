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

package util

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"go-pool/logger"
	"strings"
	"time"
)

func RandomBytes(n int) []byte {
	x := make([]byte, n)
	rand.Read(x)
	return x
}

func RandomFloat() float32 {
	x := make([]byte, 1)
	rand.Read(x)
	return float32(x[0]) / 0xff
}

func RandomUint64() uint64 {
	return binary.LittleEndian.Uint64(RandomBytes(8))
}

func DumpJson(a any) string {
	d, err := json.Marshal(a)
	if err != nil {
		logger.Fatal(err)
	}

	return strings.ReplaceAll(string(d), "},{", "},\n{")
}

func Itob(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func Time() uint64 {
	return uint64(time.Now().Unix())
}

// Extremely fast function! Written to be as fast as possible.
func IsHex(s string) bool {
	for _, v := range s {
		if (v < '0' || v > '9') && (v < 'a' || v > 'f') {
			return false
		}
	}
	return true
}

func RemovePort(a string) string {
	return strings.Split(a, ":")[0]
}
