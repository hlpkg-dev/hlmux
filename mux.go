// HLMUX
//
// Copyright (C) 2022 hlpkg-dev
//
// This program is free software: you can redistribute it and/or modify it under
// the terms of the GNU General Public License as published by the Free Software
// Foundation, either version 3 of the License, or (at your option) any later
// version.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS
// FOR A PARTICULAR PURPOSE. See the GNU General Public License for more
// details.
//
// You should have received a copy of the GNU General Public License along with
// this program. If not, see <https://www.gnu.org/licenses/>.

package hlmux

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"
)

func makeUDPAddrKey(addr *net.UDPAddr) uint64 {
	key := make([]byte, 8)
	copy(key, addr.IP.To4())
	binary.BigEndian.PutUint16(key[4:], uint16(addr.Port))
	return binary.BigEndian.Uint64(key)
}

func tokenize(data string) []string {
	return strings.Split(data, " ")
}

type HandlerFunc func(*Conn)
type HandlersChain []HandlerFunc

type Mux struct {
	timeout time.Duration

	defaultUpstream *net.UDPAddr

	conns       map[uint64]*Conn
	connsRWLock sync.RWMutex

	stop chan bool

	getchallengeChains HandlersChain
	logger             *log.Logger

	workers int
}

func NewMux(upstream *net.UDPAddr) *Mux {
	return &Mux{
		timeout:            30 * time.Second,
		defaultUpstream:    upstream,
		conns:              make(map[uint64]*Conn),
		getchallengeChains: make(HandlersChain, 0),
		workers:            runtime.NumCPU(),
	}
}

func (mux *Mux) SetWorkers(n int) {
	mux.workers = n
}

func (mux *Mux) SetTimeout(timeout time.Duration) {
	mux.timeout = timeout
}

func (mux *Mux) SetDefaultUpstream(upstream *net.UDPAddr) {
	mux.defaultUpstream = upstream
}

func (mux *Mux) FindConnectionByClient(client *net.UDPAddr) *Conn {
	mux.connsRWLock.RLock()
	defer mux.connsRWLock.RUnlock()
	if conn, ok := mux.conns[makeUDPAddrKey(client)]; ok {
		return conn
	}

	return nil
}

func (mux *Mux) Connections() []*Conn {
	ret := make([]*Conn, 0)

	mux.connsRWLock.RLock()
	defer mux.connsRWLock.RUnlock()
	for _, conn := range mux.conns {
		ret = append(ret, conn)
	}

	return ret
}

func (mux *Mux) OnGetChallenge(handlers ...HandlerFunc) {
	mux.getchallengeChains = append(mux.getchallengeChains, handlers...)
}

func (mux *Mux) delete(client *net.UDPAddr) {
	mux.connsRWLock.Lock()
	delete(mux.conns, makeUDPAddrKey(client))
	mux.connsRWLock.Unlock()

	log.Printf("delete client %v's connection", client)
}

func (mux *Mux) setConnection(client *net.UDPAddr, conn *Conn) {
	mux.connsRWLock.Lock()
	mux.conns[makeUDPAddrKey(client)] = conn
	mux.connsRWLock.Unlock()
}

func (mux *Mux) SetLogger(logger *log.Logger) {
	mux.logger = logger
}

func (mux *Mux) process(listener *net.UDPConn) error {
	data := make([]byte, 1500)
	n, client, err := listener.ReadFromUDP(data)
	if err != nil {
		return err
	}

	r := NewReader(data[:n])
	peeked, err := r.PeekUint32()
	if err != nil {
		return err
	}

	conn := mux.FindConnectionByClient(client)

	if conn == nil {
		conn = &Conn{
			timeout:      mux.timeout,
			client:       client,
			nextUpstream: mux.defaultUpstream,
		}
		mux.setConnection(client, conn)
		if err := conn.applyNextUpstream(); err != nil {
			return fmt.Errorf("cannot connect to upstream: %v", err)
		}

		go func() {
			if err := conn.RunForward(listener); err != nil {
				mux.delete(client)
			}
		}()
	}

	connectionless := false
	if peeked == 0xffffffff {
		connectionless = true
		// connectionless packets

		// eats 0xffffffff at the very beginning
		r.ReadUint32()

		// tokenize the incoming line (argv[0], argv[1], ...)
		line, _ := r.ReadString('\n')
		tokens := tokenize(line)

		switch tokens[0] {
		case "getchallenge":
			for _, handler := range mux.getchallengeChains {
				handler(conn)
			}
		default:
			// do nothing
		}
	} else {
		seq, _ := r.ReadUint32()
		ack, _ := r.ReadUint32()

		seqReliable := seq>>31 != 0
		ackReliable := ack>>31 != 0
		fragmented := seq&(1<<30) != 0

		// TODO: ...
		_ = seqReliable
		_ = ackReliable
		_ = fragmented

		// r.Unmunge2(seq & 0xff)
		// data, _ := io.ReadAll(r)
	}

	if connectionless && conn.shouldUpdate() {
		conn.Stop()
	}

	if conn != nil {
		if connectionless {
			log.Printf("client %v write to upstream %v: %v", client, conn.Upstream(), string(data[:n]))
		}
		if err := conn.Write(data[:n]); err != nil {
			conn.Stop()
		}
	}

	return nil
}

func (mux *Mux) Run(address string) error {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return fmt.Errorf("cannot resolve addr: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen: %v", err)
	}
	defer conn.Close()

	wg := &sync.WaitGroup{}
	ctx, cancel := context.WithCancel(context.Background())

	wg.Add(mux.workers)
	for i := 0; i < mux.workers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case <-mux.stop:
					cancel()
					return
				default:
					err := mux.process(conn)
					if err != nil {
						log.Printf("process error: %v", err)
					}
				}
			}
		}()
	}

	wg.Wait()
	cancel()

	return nil
}

func (mux *Mux) Stop() {
	mux.stop <- true
}
