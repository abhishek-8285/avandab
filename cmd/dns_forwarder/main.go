package main

import (
	"log"
	"net"
	"sync"
)

func runProxy(addr string) {
	laddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		log.Printf("resolve %s: %v", addr, err)
		return
	}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		log.Printf("listen %s: %v", addr, err)
		return
	}
	defer conn.Close()
	log.Printf("DNS Proxy listening on %s", addr)

	upstream, _ := net.ResolveUDPAddr("udp", "8.8.8.8:53")
	buf := make([]byte, 4096)
	for {
		n, cAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		data := make([]byte, n)
		copy(data, buf[:n])
		go func(q []byte, client *net.UDPAddr) {
			up, err := net.DialUDP("udp", nil, upstream)
			if err != nil {
				return
			}
			defer up.Close()
			_, _ = up.Write(q)
			resp := make([]byte, 4096)
			rn, err := up.Read(resp)
			if err != nil {
				return
			}
			_, _ = conn.WriteToUDP(resp[:rn], client)
		}(data, cAddr)
	}
}

func main() {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		runProxy("127.0.0.1:53")
	}()
	go func() {
		defer wg.Done()
		runProxy("[::1]:53")
	}()
	wg.Wait()
}
