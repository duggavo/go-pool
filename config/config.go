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

package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

const MAX_REQUEST_SIZE = 5 * 1024 // 5 MiB

var Cfg Config

func init() {
	fd, err := os.ReadFile("config.json")
	if err != nil {
		fmt.Println(err)

		fd, err = os.ReadFile("../config.json")
		if err != nil {
			blankCfg, err := json.MarshalIndent(Config{}, "", "\t")

			if err != nil {
				panic(err)
			}

			os.WriteFile("config.json", blankCfg, 0o666)

			panic(fmt.Errorf("could not open config: %s. blank configuration created", err))
		}
	}

	err = json.Unmarshal(fd, &Cfg)
	if err != nil {
		panic(err)
	}

	// master password is hashed with sha256 to make it fixed-length (32 bytes long)
	MasterPass = sha256.Sum256([]byte(Cfg.MasterPass))
	fmt.Println("Master password is", hex.EncodeToString(MasterPass[:]))

}

var MasterPass [32]byte
var BlockTime uint64

type Config struct {
	LogLevel uint8 `json:"log_level"`

	DaemonRpc string `json:"daemon_rpc"`

	Atomic   int    `json:"atomic"`
	MinConfs uint64 `json:"min_confs"`

	AddrPrefix    []byte `json:"addr_prefix"`
	SubaddrPrefix []byte `json:"subaddr_prefix"`

	PoolAddress string `json:"pool_address"`
	FeeAddress  string `json:"fee_address"`

	UseP2Pool     bool   `json:"use_p2pool"`
	P2PoolAddress string `json:"p2pool_address"`

	MasterPass string `json:"master_pass"`

	AlgoName string `json:"algo_name"`

	MasterConfig MasterConfig `json:"master_config"`
	SlaveConfig  SlaveConfig  `json:"slave_config"`
}

type MasterConfig struct {
	ListenAddress    string        `json:"listen_address"`
	WalletRpc        string        `json:"wallet_rpc"`
	FeePercent       float64       `json:"fee_percent"`
	ApiPort          uint16        `json:"api_port"`
	WithdrawalFee    float64       `json:"withdrawal_fee"`
	MinWithdrawal    float64       `json:"min_withdrawal"`
	WithdrawInterval int64         `json:"withdrawal_interval_minutes"`
	Stratums         []StratumAddr `json:"stratums"`
}
type SlaveConfig struct {
	MasterAddress string `json:"master_address"`

	MinDiff         uint64 `json:"min_diff"`
	ShareTargetTime uint64 `json:"share_target_time"`
	TrustScore      uint64 `json:"trust_score"`

	PoolPort    uint16 `json:"pool_port"`
	PoolPortTls uint16 `json:"pool_port_tls"`

	TemplateTimeout int     `json:"template_timeout"`
	SlaveFee        float64 `json:"slave_fee"`
}

type StratumAddr struct {
	Addr string `json:"addr"`
	Desc string `json:"desc"`
	Tls  bool   `json:"tls"`
}
