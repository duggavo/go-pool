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
	"math/big"
)

//const base58 = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

var base58Lookup = map[byte]int{
	'1': 0, '2': 1, '3': 2, '4': 3, '5': 4, '6': 5, '7': 6, '8': 7, '9': 8,
	'A': 9, 'B': 10, 'C': 11, 'D': 12, 'E': 13, 'F': 14, 'G': 15, 'H': 16,
	'J': 17, 'K': 18, 'L': 19, 'M': 20, 'N': 21, 'P': 22, 'Q': 23, 'R': 24,
	'S': 25, 'T': 26, 'U': 27, 'V': 28, 'W': 29, 'X': 30, 'Y': 31, 'Z': 32,
	'a': 33, 'b': 34, 'c': 35, 'd': 36, 'e': 37, 'f': 38, 'g': 39, 'h': 40,
	'i': 41, 'j': 42, 'k': 43, 'm': 44, 'n': 45, 'o': 46, 'p': 47, 'q': 48,
	'r': 49, 's': 50, 't': 51, 'u': 52, 'v': 53, 'w': 54, 'x': 55, 'y': 56, 'z': 57,
}
var base58big = big.NewInt(58)

var encodedBlockSizes = []int{0, 2, 3, 5, 6, 7, 9, 10, 11}

/*// encode 8 byte chunk with necessary padding
func encodeBlock(raw []byte) (result string) {
	remainder := new(big.Int)
	remainder.SetBytes(raw)
	bigZero := new(big.Int)
	for remainder.Cmp(bigZero) > 0 {
		current := new(big.Int)
		remainder.DivMod(remainder, base58big, current)
		result = string(base58[current.Int64()]) + result
	}

	for i := range encodedBlockSizes {
		if i == len(raw) {
			if len(result) < encodedBlockSizes[i] {
				result = strings.Repeat("1", (encodedBlockSizes[i]-len(result))) + result
			}
			return result
		}
	}

	return // we never reach here, if inputs are well-formed <= 8 bytes
}*/

func decodeBlock(encoded string) (result []byte) {
	bigResult := big.NewInt(0)
	currentMultiplier := big.NewInt(1)
	tmp := new(big.Int)
	for i := len(encoded) - 1; i >= 0; i-- {
		// Make sure that the character is actually base58
		if encoded[i] != '1' && base58Lookup[encoded[i]] == 0 {
			return
		}
		tmp.SetInt64(int64(base58Lookup[encoded[i]]))
		tmp.Mul(currentMultiplier, tmp)
		bigResult.Add(bigResult, tmp)
		currentMultiplier.Mul(currentMultiplier, base58big)
	}

	for i := range encodedBlockSizes {
		if encodedBlockSizes[i] == len(encoded) {
			result = append([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0}, bigResult.Bytes()...)
			return result[len(result)-i:] // return necessary bytes, initial zero appended  as per mapping
		}
	}

	return // we never reach here, if inputs are well-formed <= 11 chars
}

/*func encodeBase58(data ...[]byte) (result string) {
	var combined []byte
	for _, item := range data {
		combined = append(combined, item...)
	}

	fullblocks := len(combined) / 8
	for i := 0; i < fullblocks; i++ {
		result += encodeBlock(combined[i*8 : (i+1)*8])
	}
	if len(combined)%8 > 0 {
		result += encodeBlock(combined[fullblocks*8:])
	}
	return
}*/

func decodeBase58(data string) (result []byte) {
	fullblocks := len(data) / 11
	for i := 0; i < fullblocks; i++ {
		result = append(result, decodeBlock(data[i*11:(i+1)*11])...)
	}
	if len(data)%11 > 0 {
		result = append(result, decodeBlock(data[fullblocks*11:])...)
	}
	return
}
