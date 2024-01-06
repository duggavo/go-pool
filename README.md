# Go-Pool
An Open-Source, Extremely High Performance Monero and Cryptonote Mining Pool

(c) 2024 Duggavo. All rights reserved.

## Features

- High performance
	The same job is reused with several miners if they support the Nicehash mode
- Horizontally scalable
	You can run as many slaves as you desire, on different servers.
	The slaves connect to the same master server.
- PPLNS payment scheme
	Fair payment scheme, resistant to hopping attacks unlike PROP, safe for the pool unlike PPS

## Architecture

### Master
The pool master server. It stores shares, handles payments, saves stats, and runs API server.

### Slave
The slave server runs a stratum server, verifies shares, and, if they are valid, broadcasts them to
the master server. Slave server uses the daemon RPC call `calc_pow`, which requires you to run a local daemon.

You can run many slaves in the same pool, potentially at different locations.

Traffic between the Slave and the Master is encrypted using a common password, `master_pass`.

Make sure that the `master_pass` is secure, otherwise attackers could pretend to be a slave server
and submit fake shares - in practice, steal reward from your miners.

## Optimizing your pool

### Reduce latency
Pool latency harms the profit, since it increases the chance of orphaned blocks.
This pool can have extremely low latencies, but the daemon needs to be set up to allow this.
All you need to do is start the daemon using this flag: `--block-notify '/usr/bin/pkill -USR1 slave'`.

This way, the slave server will be notified when a new blocks is added to the daemon.

Your pool's profits will immediately start getting benefit from this.

### Reduce CPU usage
For RandomX coins (such as Monero, Zephyr, Arqma, Nevocoin, Wownero, Sispop), you can speed up share
verification by using the full RandomX scratchpad (requires 2 GB extra RAM).

You can do this by running the daemon with the
`MONERO_RANDOMX_FULL_MEM` environment variable, for example:
```bash
env MONERO_RANDOMX_FULL_MEM='1' ./yourcoind
```

## Custom Diff
You can add +DIFF to your miner user to change the starting diff. For example:
8A1b4qgyA1516hba+15000
will set the miner's initial difficulty to 15000.

Note that the difficulty will be automatically changed to the best difficulty for the miner's hashrate.
There is no need to have super-low difficulty window: payment scheme is PPLNS.

## Pools running go-pool
Create an issue or a Pull Request to list your pool here.

# Donation
If you want to support the go-pool development, consider donating any amount.
This will help me keeping this up to date.

Monero: `83HMPHuYEf78cwjWTgDSRe4sAXiwsMb5XhRoMhERD74h3o9rZxF2GqNfG86erFhaHEDPLSK2VNjQ1X5aWcvS63dz45c8fxL`