// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/valhallacoin/vhcd/peer"
	"github.com/valhallacoin/vhcd/wire"
)

const (
	// defaultAddressTimeout defines the duration to wait
	// for new addresses.
	defaultAddressTimeout = time.Minute * 10

	// defaultNodeTimeout defines the timeout time waiting for
	// a response from a node.
	defaultNodeTimeout = time.Second * 3
)

var (
	amgr *Manager
	wg   sync.WaitGroup
)

func creep() {
	defer wg.Done()

	onaddr := make(chan struct{})
	verack := make(chan struct{})
	config := peer.Config{
		UserAgentName:    "vhcpeersniffer",
		UserAgentVersion: "0.0.1",
		ChainParams:      activeNetParams,
		DisableRelayTx:   true,

		Listeners: peer.MessageListeners{
			OnAddr: func(p *peer.Peer, msg *wire.MsgAddr) {
				n := make([]net.IP, 0, len(msg.AddrList))
				for _, addr := range msg.AddrList {
					n = append(n, addr.IP)
				}
				added := amgr.AddAddresses(n)
				log.Printf("Peer %v sent %v addresses, %d new",
					p.Addr(), len(msg.AddrList), added)
				onaddr <- struct{}{}
			},
			OnVerAck: func(p *peer.Peer, msg *wire.MsgVerAck) {
				log.Printf("Adding peer %v with services %v",
					p.NA().IP.String(), p.Services())

				verack <- struct{}{}
			},
		},
	}

	var wg sync.WaitGroup
	for {
		ips := amgr.Addresses()
		if len(ips) == 0 {
			log.Printf("No stale addresses -- sleeping for %v",
				defaultAddressTimeout)
			time.Sleep(defaultAddressTimeout)
			continue
		}

		wg.Add(len(ips))

		for _, ip := range ips {
			go func(ip net.IP) {
				defer wg.Done()

				host := net.JoinHostPort(ip.String(),
					activeNetParams.DefaultPort)
				p, err := peer.NewOutboundPeer(&config, host)
				if err != nil {
					log.Printf("NewOutboundPeer on %v: %v",
						host, err)
					return
				}
				amgr.Attempt(ip)
				conn, err := net.DialTimeout("tcp", p.Addr(),
					defaultNodeTimeout)
				if err != nil {
					return
				}
				p.AssociateConnection(conn)

				// Wait for the verack message or timeout in case of
				// failure.
				select {
				case <-verack:
					// Mark this peer as a good node.
					amgr.Good(p.NA().IP, p.Services())

					// Ask peer for some addresses.
					p.QueueMessage(wire.NewMsgGetAddr(), nil)

				case <-time.After(defaultNodeTimeout):
					log.Printf("verack timeout on peer %v",
						p.Addr())
					p.Disconnect()
					return
				}

				select {
				case <-onaddr:
				case <-time.After(defaultNodeTimeout):
					log.Printf("getaddr timeout on peer %v",
						p.Addr())
					p.Disconnect()
					return
				}
				p.Disconnect()
			}(ip)
		}
		wg.Wait()
	}
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "loadConfig: %v\n", err)
		os.Exit(1)
	}
	amgr, err = NewManager(filepath.Join(defaultHomeDir,
		activeNetParams.Name))
	if err != nil {
		fmt.Fprintf(os.Stderr, "NewManager: %v\n", err)
		os.Exit(1)
	}

	amgr.AddAddresses([]net.IP{net.ParseIP(cfg.Seeder)})

	wg.Add(1)
	go creep()

	dnsServer := NewDNSServer(cfg.Host, cfg.Nameserver, cfg.Listen)
	go dnsServer.Start()

	wg.Wait()
}
