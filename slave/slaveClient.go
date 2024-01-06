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

package slave

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"go-pool/config"
	"go-pool/logger"
	"go-pool/serializer"
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

var conn net.Conn
var connMut sync.RWMutex

const Overhead = 40

func StartSlaveClient() {
out:
	for {
		logger.Info("Connecting to master server:", config.Cfg.SlaveConfig.MasterAddress)

		var err error
		conn, err = net.Dial("tcp", config.Cfg.SlaveConfig.MasterAddress)

		if err != nil {
			logger.Error(err)
			time.Sleep(time.Second)
			continue out
		}

		for {
			lenBuf := make([]byte, 2+Overhead)
			_, err := io.ReadFull(conn, lenBuf)
			if err != nil {
				logger.Warn(err)
				conn.Close()
				time.Sleep(time.Second)
				continue out
			}
			lenBuf, err = Decrypt(lenBuf)
			if err != nil {
				logger.Warn(err)
				conn.Close()
				time.Sleep(time.Second)
				continue out
			}
			len := int(lenBuf[0]) | (int(lenBuf[1]) << 8)

			// read the actual message

			buf := make([]byte, len+Overhead)
			_, err = io.ReadFull(conn, buf)
			if err != nil {
				logger.Warn(err)
				conn.Close()
				time.Sleep(time.Second)
				continue out
			}
			buf, err = Decrypt(buf)
			if err != nil {
				logger.Warn(err)
				conn.Close()
				time.Sleep(time.Second)
				continue out
			}
			logger.Net("Received message:", hex.EncodeToString(buf))
			OnMessage(buf)
		}

	}
}
func OnMessage(b []byte) {
	d := serializer.Deserializer{
		Data: b,
	}

	packet := d.ReadUint8()

	switch packet {

	}
}

func SendShare(wallet string, diff uint64) {
	connMut.Lock()
	defer connMut.Unlock()

	cacheShare(wallet, diff)

	/*if conn == nil {
		cacheShare(wallet, diff)

		return
	}

	s := serializer.Serializer{
		Data: []byte{0},
	}

	s.AddUvarint(1)
	s.AddString(wallet)
	s.AddUvarint(diff)

	sendToConn(s.Data)*/
}

func SendBlockFound(height, reward uint64, hash []byte) {
	s := serializer.Serializer{
		Data: []byte{1},
	}

	s.AddUvarint(height)
	s.AddUvarint(reward)
	s.AddFixedByteArray(hash, 32)

	sendToConn(s.Data)
}
func SendStats(nrMiners int) {
	s := serializer.Serializer{
		Data: []byte{2},
	}
	s.AddUvarint(uint64(nrMiners))

	sendToConn(s.Data)
}
func SendShareFound(height uint64) {
	s := serializer.Serializer{
		Data: []byte{3},
	}

	s.AddUvarint(height)

	sendToConn(s.Data)
}
func sendToConn(data []byte) {
	if conn == nil {
		logger.Error("SendToConn: Connection is nil")
		return
	}
	var dataLenBin = make([]byte, 0, 2)
	dataLenBin = binary.LittleEndian.AppendUint16(dataLenBin, uint16(len(data)))
	conn.Write(Encrypt(dataLenBin))
	conn.Write(Encrypt(data))
}

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
		panic("ciphertext too short")
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
