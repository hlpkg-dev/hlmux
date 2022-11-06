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

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/hlpkg-dev/hlmux"
)

type Upstream struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
	Address string `json:"address"`
}

type Config struct {
	Bind      string      `json:"bind"`
	API       string      `json:"api"`
	TTL       int         `json:"ttl"`
	Upstreams []*Upstream `json:"upstreams"`
}

type Set struct {
	Proxy    string `json:"proxy"`
	Upstream string `json:"upstream"`
}

var flagConfig = flag.String("config", "config.json", "Configuration file")

func readConfig() (*Config, error) {
	var config Config

	data, err := os.ReadFile(*flagConfig)
	if err != nil {
		return nil, err
	}

	if json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func main() {
	config, err := readConfig()
	if err != nil {
		log.Fatalf("cannot read config: %v", err)
	}

	if len(config.Upstreams) == 0 {
		log.Fatalf("cannot find valid upstreams")
	}

	upstreams := make(map[string]*net.UDPAddr)
	var defaultUpstream *net.UDPAddr
	for _, upstream := range config.Upstreams {
		addr, err := net.ResolveUDPAddr("udp", upstream.Address)
		if err != nil {
			log.Fatalf("cannot resolve \"%s\": %v", upstream.Address, err)
		}
		upstreams[upstream.Name] = addr

		if upstream.Default {
			defaultUpstream = addr
		}
	}

	if defaultUpstream == nil {
		defaultUpstream = upstreams[config.Upstreams[0].Name]
	}

	log.Printf("default upstream: %v", defaultUpstream)

	mux := hlmux.NewMux(defaultUpstream)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			fmt.Print("> ")
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			tokens := strings.FieldsFunc(line, func(c rune) bool {
				return c == ' ' || c == '\n'
			})
			if len(tokens) > 0 {
				switch tokens[0] {
				case "ls":
					conns := mux.Connections()
					if len(conns) == 0 {
						fmt.Println("no connections")
					}
					for i, conn := range conns {
						fmt.Printf(`
	[%d] Client: %v Proxy: %v Upstream: %v Next: %v
`, i, conn.Client(), conn.Proxy(), conn.Upstream(), conn.NextUpstream())
					}
				case "update":
					clientAddr, err := net.ResolveUDPAddr("udp", tokens[1])
					if err != nil {
						fmt.Printf("invalid client: %v\n", err)
					}
					if conn := mux.FindConnectionByClient(clientAddr); conn != nil {
						addr, err := net.ResolveUDPAddr("udp", tokens[2])
						if err != nil {
							fmt.Printf("invalid upstream: %v\n", err)
						} else {
							conn.SetNextUpstream(addr)
						}
					} else {
						fmt.Printf("connection not found")
					}
				}
			}
		}
	}()
	go func() {
		// recommended to use more powerful libraries like `gin`

		http.HandleFunc("/api/v1/set", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				w.WriteHeader(http.StatusNotImplemented)
				return
			}
			var s Set
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			if upstream, ok := upstreams[s.Upstream]; ok {
				found := false
				for _, conn := range mux.Connections() {
					if conn.Proxy().String() == s.Proxy {
						found = true
						log.Printf("set client %v next upstream to %v", conn.Client(), upstream)
						conn.SetNextUpstream(upstream)
						break
					}
				}

				if found {
					w.WriteHeader(http.StatusOK)
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		})

		if err := http.ListenAndServe(config.API, nil); err != nil {
			log.Printf("cannot server http: %v", err)
		}
	}()
	if err := mux.Run(config.Bind); err != nil {
		log.Fatalf("cannot init mux: %v", err)
	}
}
