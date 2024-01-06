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
	"crypto/rand"
	"fmt"
	"go-pool/config"

	"golang.org/x/crypto/chacha20poly1305"
)

func Encrypt(msg []byte) []byte {
	aead, err := chacha20poly1305.NewX(config.MasterPass[:])
	if err != nil {
		panic(err)
	}

	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(msg)+aead.Overhead())
	rand.Read(nonce)

	// Encrypt the message and append the ciphertext to the nonce.
	return aead.Seal(nonce, nonce, msg, nil)
}
func Decrypt(msg []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(config.MasterPass[:])
	if err != nil {
		panic(err)
	}

	if len(msg) < aead.NonceSize() {
		return []byte{}, fmt.Errorf("cyphertext too short")
	}

	// Split nonce and ciphertext.
	nonce, ciphertext := msg[:aead.NonceSize()], msg[aead.NonceSize():]

	// Decrypt the message and check it wasn't tampered with.
	decrypted, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return []byte{}, err
	}

	return decrypted, nil
}
