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
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

type Conn struct {
	timeout      time.Duration
	client       *net.UDPAddr
	upstreamConn *net.UDPConn
	lock         sync.RWMutex

	nextUpstream *net.UDPAddr
}

func (c *Conn) shouldUpdate() bool {
	return c.NextUpstream() != nil && (c.Upstream() == nil || c.Upstream().String() != c.NextUpstream().String())
}

func (c *Conn) Client() *net.UDPAddr {
	return c.client
}

func (c *Conn) Conn() *net.UDPConn {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.upstreamConn
}

func (c *Conn) Proxy() *net.UDPAddr {
	if conn := c.Conn(); conn != nil {
		return conn.LocalAddr().(*net.UDPAddr)
	} else {
		return nil
	}
}

func (c *Conn) Upstream() *net.UDPAddr {
	if conn := c.Conn(); conn != nil {
		return conn.RemoteAddr().(*net.UDPAddr)
	} else {
		return nil
	}
}

func (c *Conn) NextUpstream() *net.UDPAddr {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.nextUpstream
}

func (c *Conn) SetNextUpstream(upstream *net.UDPAddr) {
	c.lock.Lock()
	c.nextUpstream = upstream
	c.lock.Unlock()
}

func (c *Conn) Stop() error {
	c.lock.RLock()
	conn := c.upstreamConn
	c.lock.RUnlock()

	if conn != nil {
		c.lock.Lock()
		c.upstreamConn = nil
		c.lock.Unlock()
		return conn.Close()
	} else {
		return nil
	}
}

func (c *Conn) Read() ([]byte, error) {
	data := make([]byte, 1500)

	conn := c.Conn()

	if conn == nil {
		return nil, fmt.Errorf("connection is not established")
	}

	conn.SetReadDeadline(time.Now().Add(c.timeout))
	n, err := conn.Read(data)

	if err != nil {
		return nil, err
	}

	return data[:n], nil
}

func (c *Conn) Write(data []byte) error {
	conn := c.Conn()

	if conn == nil {
		return fmt.Errorf("connection is not established")
	}

	conn.SetWriteDeadline(time.Now().Add(c.timeout))
	_, err := conn.Write(data)

	if err != nil {
		return err
	}

	return nil
}

func (c *Conn) applyNextUpstream() error {
	log.Printf("change upstream from %v to %v", c.Upstream(), c.NextUpstream())
	newConn, err := net.DialUDP("udp", nil, c.NextUpstream())
	if err != nil {
		log.Printf("error dial %v", err)
		return err
	}

	c.lock.Lock()
	c.upstreamConn = newConn
	c.nextUpstream = nil
	c.lock.Unlock()

	return nil
}

func (c *Conn) RunForward(conn *net.UDPConn) error {
	for {
		if c.Upstream() != nil {
			data, err := c.Read()
			if err != nil {
				c.Stop()
				if c.NextUpstream() != nil {
					if err := c.applyNextUpstream(); err != nil {
						return err
					}
					continue
				} else {
					return fmt.Errorf("read from upstream error: %v", err)
				}
			}

			conn.SetWriteDeadline(time.Now().Add(c.timeout))
			if _, err := conn.WriteToUDP(data, c.client); err != nil {
				c.Stop()
				return fmt.Errorf("write to client from upstream error: %v", err)
			}
		} else {
			return fmt.Errorf("no upstream candidates available")
		}
	}
}
