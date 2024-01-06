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

package stratum

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"go-pool/config"
	"go-pool/logger"
	"go-pool/p2pool"
	"go-pool/util"
	"net"
	"strconv"
	"sync"
	"time"
)

type Server struct {
	Connections []*Connection
	ConnsMut    sync.RWMutex

	NewConnections chan *Connection

	sync.Mutex
}

type Connection struct {
	Conn net.Conn
	Id   uint64

	IsTls bool

	CurrentJob ConnJob
	LastJob    ConnJob

	NextDiff  float64
	LastShare int64 // in unix milliseconds
	Score     int32
	Nicehash  bool
	P2Pool    p2pool.P2PoolClient

	sync.RWMutex
}

type ConnJob struct {
	Blob        []byte // Blocktemplate blob
	HashingBlob []byte // Blockhashing blob

	Diff  uint64
	JobID string

	NicehashByte byte
}

func (c *Connection) Send(a any) error {
	data, err := json.Marshal(a)
	if err != nil {
		panic(err)
	}
	c.SendBytes(data)
	return nil
}
func (c *Connection) SendBytes(data []byte) error {
	logger.Net(">>>", string(data))
	c.Conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
	_, err := c.Conn.Write(append(data, '\n'))
	if err != nil {
		return err
	}
	return nil
}

func randomUint64() uint64 {
	b := make([]byte, 8)
	rand.Read(b)

	return binary.BigEndian.Uint64(b)
}

func (s *Server) Start(port uint16, port_tls uint16) {
	s.NewConnections = make(chan *Connection, 1)

	go func() {
		cert, err := tls.LoadX509KeyPair("cert.pem", "key.pem")
		if err != nil {
			logger.Error("Invalid TLS certificate:", err, "generating a new one")

			certPem, keyPem, err := GenCertificate()
			if err != nil {
				logger.Fatal(err)
			}

			cert, err = tls.X509KeyPair(certPem, keyPem)
			if err != nil {
				logger.Fatal(err)
			}

		}
		listener_tls, err := tls.Listen("tcp", "0.0.0.0:"+strconv.FormatUint(uint64(port_tls), 10), &tls.Config{
			Certificates: []tls.Certificate{
				cert,
			},
		})
		if err != nil {
			panic(err)
		}
		logger.Info("Stratum TLS server listening on port", port_tls)
		for {
			c, err := listener_tls.Accept()
			if err != nil {
				logger.Error(err)
				continue
			}

			conn := &Connection{
				Conn:  c,
				IsTls: true,
				Id:    randomUint64(),
			}
			go s.handleConnection(conn)
		}
	}()

	listener, err := net.Listen("tcp", "0.0.0.0:"+strconv.FormatUint(uint64(port), 10))
	if err != nil {
		panic(err)
	}

	logger.Info("Stratum server listening on port", port)

	for {
		c, err := listener.Accept()
		if err != nil {
			logger.Error(err)
			continue
		}
		minerIp := util.RemovePort(c.RemoteAddr().String())

		logger.Debug("new incoming connection with IP", minerIp)

		conn := &Connection{
			IsTls: false,
			Conn:  c,
			Id:    randomUint64(),
		}
		go s.handleConnection(conn)
	}
}

// Note: srv.ConnsMut must be locked before calling this
func (s *Server) Kick(id uint64) {
	var connectionsNew = make([]*Connection, 0, len(s.Connections))

	for _, v := range s.Connections {
		if v.Id == id {
			v.Conn.Close()

			if config.Cfg.UseP2Pool && v.P2Pool.Jobs != nil {
				// terminate the p2pool connection
				v.P2Pool.Stop()
			}
		} else {
			connectionsNew = append(connectionsNew, v)
		}
	}
	s.Connections = connectionsNew
}

func (srv *Server) handleConnection(conn *Connection) {
	srv.ConnsMut.Lock()
	srv.Connections = append(srv.Connections, conn)
	srv.ConnsMut.Unlock()
	logger.Debug("handling connection")

	srv.NewConnections <- conn
}

func ReadJSON(response interface{}, reader *bufio.Reader) error {
	data, isPrefix, err := reader.ReadLine()
	if isPrefix {
		logger.Warn("oversized request")
		return errors.New("oversize request")
	} else if err != nil {
		logger.Debug("Stratum server: error reading:", err)
		return err
	}
	logger.Net("<<< " + string(data))
	err = json.Unmarshal(data, response)
	if err != nil {
		logger.Warn("failed to unmarshal json:", err)
		return err
	}
	return nil
}
