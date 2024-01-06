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
	"encoding/binary"
	"encoding/hex"
	"go-pool/config"
	"go-pool/logger"
	"go-pool/serializer"
	"go-pool/util"
	"io"
	"math"
	"net"
)

const Overhead = 40

// numConns is locked by the mutex of Stats
var numConns = make(map[uint64]uint32)

func HandleSlave(conn net.Conn) {
	var connId uint64 = util.RandomUint64()

	for {
		lenBuf := make([]byte, 2+Overhead)
		_, err := io.ReadFull(conn, lenBuf)
		if err != nil {
			logger.Warn(err)
			conn.Close()
			Stats.Lock()
			delete(numConns, connId)
			Stats.Unlock()
			return
		}
		lenBuf, err = Decrypt(lenBuf)
		if err != nil {
			logger.Warn(err)
			conn.Close()
			Stats.Lock()
			delete(numConns, connId)
			Stats.Unlock()
			return
		}
		len := int(lenBuf[0]) | (int(lenBuf[1]) << 8)

		// read the actual message

		buf := make([]byte, len+Overhead)
		_, err = io.ReadFull(conn, buf)
		if err != nil {
			logger.Warn(err)
			conn.Close()
			Stats.Lock()
			delete(numConns, connId)
			Stats.Unlock()
			return
		}
		buf, err = Decrypt(buf)
		if err != nil {
			logger.Warn(err)
			conn.Close()
			Stats.Lock()
			delete(numConns, connId)
			Stats.Unlock()
			return
		}
		logger.NetDev("Received message:", hex.EncodeToString(buf))
		OnMessage(buf, connId)
	}
}

func SendToConn(conn net.Conn, data []byte) {
	var dataLenBin = make([]byte, 0, 2)
	dataLenBin = binary.LittleEndian.AppendUint16(dataLenBin, uint16(len(data)))
	conn.Write(Encrypt(dataLenBin))
	conn.Write(Encrypt(data))
}

func OnMessage(msg []byte, connId uint64) {
	d := serializer.Deserializer{
		Data: msg,
	}
	if d.Error != nil {
		logger.Error(d.Error)
		return
	}

	packet := d.ReadUint8()

	switch packet {
	case 0: // Share Found packet
		numShares := uint32(d.ReadUvarint())
		wallet := d.ReadString()
		diff := d.ReadUvarint()

		if d.Error != nil {
			logger.Error(d.Error)
			return
		}

		OnShareFound(wallet, diff, numShares)
	case 1: // Block Found packet
		if config.Cfg.UseP2Pool {
			logger.Error("received Block Found packet; is using P2Pool")
			return
		}

		height := d.ReadUvarint()
		reward := d.ReadUvarint()
		hash := hex.EncodeToString(d.ReadFixedByteArray(32))

		if d.Error != nil {
			logger.Error(d.Error)
			return
		}

		/*BlocksMut.Lock()
		BlocksFound = append(BlocksFound, hash)
		SaveBlocks()
		BlocksMut.Unlock()*/

		logger.Info("Found block height", height, "reward", float64(reward)/math.Pow10(config.Cfg.Atomic), "hash", hash)
		OnBlockFound(height, reward, hash)
	case 2: // Stats packet
		conns := uint32(d.ReadUvarint())

		if d.Error != nil {
			logger.Error(d.Error)
			return
		}

		func() {
			Stats.Lock()
			defer Stats.Unlock()

			numConns[connId] = conns

			Stats.Workers = 0
			for _, v := range numConns {
				Stats.Workers += v
			}
		}()
	case 3: // P2Pool Share Found
		if !config.Cfg.UseP2Pool {
			logger.Error("received P2Pool Share Found packet; is not using P2Pool")
			return
		}

		height := d.ReadUvarint()

		OnP2PoolShareFound(height)

	default:
		logger.Error("unknown packet type", packet)
		return
	}

}
