package main

import (
	"log"
	"net"
	"time"
)

type Service struct {
	chStatus    chan int
	name        string
	key         string
	serverAddr  string
	serviceAddr string
	poolSize    int
}

func newService() *Service {
	p := &Service{}
	p.chStatus = make(chan int, 5)

	return p
}

func (this *Service) loop() {
	var totalConn = 0
	var idleConn = 0

	for {
		addr, err := net.ResolveTCPAddr("tcp", this.serverAddr)
		if err != nil {
			log.Printf("resolve %s fail! %v\n", this.serverAddr, err)
			if isQuitting(time.Minute) {
				break
			}
			time.Sleep(time.Minute)
			continue
		}
		cn, err := net.DialTCP("tcp", nil, addr)
		if err != nil {
			log.Printf("connect to %s fail! %v\n", this.serverAddr, err)
			if isQuitting(time.Minute) {
				break
			}
			time.Sleep(time.Minute)
			continue
		}
		log.Printf("connected to server %s.\n", this.serverAddr)
		go this.createSession(cn)
		totalConn++
		idleConn++
		//log.Printf("idleConn=%d,totalConn=%d", idleConn, totalConn)
		for idleConn >= this.poolSize {
			select {
			case <-g_chClose:
				goto end
			case status := <-this.chStatus:
				//log.Printf("status=%d,idle=%d,total=%d\n", status, idleConn, totalConn)
				if status == 1 {
					//状态由空闲变为使用
					if idleConn > 0 {
						idleConn--
					}
				} else if status == 2 {
					//状态由使用变为关闭
					if totalConn > 0 {
						totalConn--
					}
				} else if status == 3 {
					//状态由空闲变为关闭
					if idleConn > 0 {
						idleConn--
					}

					if totalConn > 0 {
						totalConn--
					}
				}
			}
		}
	}

end:
}

func (this *Service) createSession(cn *net.TCPConn) {
	p := newSession()
	p.chStatus = this.chStatus
	p.key = this.key
	p.serverAddr = this.serverAddr
	p.serviceAddr = this.serviceAddr
	p.cnServer = cn

	p.loop()
}
