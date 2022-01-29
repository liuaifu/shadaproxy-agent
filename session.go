package main

import (
	"log"
	"net"
	"sync"
	"time"
	"github.com/liuaifu/buffer"
)

type Session struct {
	cnServer    *net.TCPConn //与代理服务器的连接
	cnService   *net.TCPConn //与目标服务的连接
	chStatus    chan int
	chIdle      chan int
	key         string
	serverAddr  string       //代理服务器地址
	serviceAddr string       //目标服务地址
	onceStop    sync.Once    //避免stop重复调用
}

func newSession() *Session {
	p := &Session{}
	p.chIdle = make(chan int, 1)
	p.onceStop = sync.Once{}

	return p
}

func (this *Session) loop() {
	if !this.sendKey() {
		log.Printf("send key eror")
		isQuitting(time.Second)
		this.onceStop.Do(this.stop)
		return
	}
	ok, msgType, result, _ := this.receiveServerMsg()
	if ok {
		if msgType == PKT_REPORT_KEY {	//上报key应答
			if result == int32(0) {
				log.Printf("invalid key: %s", this.key)
				isQuitting(time.Minute)		//延迟，避免过快重连
				this.onceStop.Do(this.stop)
				return
			}
			//到这里说明key是有效的
		} else {
			//如果能到这里一般是协议不对
			log.Printf("invalid message: 0x%08X", msgType)
			isQuitting(time.Minute)
			this.onceStop.Do(this.stop)
			return
		}
	} else {
		log.Printf("receive message error")
		isQuitting(time.Second)
		this.onceStop.Do(this.stop)
		return
	}

	this.serverLoop()
}

/**
* 停止会话
* 提示：保证每个会话只调一次
 */
func (this *Session) stop() {
	if this.cnServer != nil {
		this.cnServer.Close()
		this.cnServer = nil
		log.Printf("server closed.\n")
		if this.cnService != nil {
			//状态由使用变为关闭
			this.chStatus <- 2
		} else {
			//状态由空闲变为关闭
			this.chStatus <- 3
		}
		this.chIdle <- 1
	}

	if this.cnService != nil {
		this.cnService.Close()
		this.cnService = nil
		log.Printf("service closed.\n")
	}
}

/**
* 报告key给服务器
* 如果服务器发现key不存在会关闭会话
 */
func (this *Session) sendKey() bool {
	head := Head{}
	head.pkt_type = PKT_REPORT_KEY

	buf := buffer.New()
	buf.WriteStr(this.key)
	body := buf.Buffer()

	return this.sendToServer(&head, &body)
}

/**
* 定时发心跳，维持与服务器的连接
 */
func (this *Session) sendIdleRoutine() {
	for {
		select {
		case <-g_chClose:
			goto end
		case <-time.After(30 * time.Second):
			//log.Printf("send idle\n")

			head := Head{}
			head.pkt_type = PKT_HEARTBEAT

			if !this.sendToServer(&head, nil) {
				goto end
			}
			break
		case <-this.chIdle:
			log.Printf("exit idle routine")
			goto end
		}
	}
end:
}

func (this *Session) sendToServer(head *Head, body *[]byte) bool {
	if this.cnServer == nil || head == nil {
		this.onceStop.Do(this.stop)
		return false
	}

	buf := buffer.New()
	buf.WriteUint32(head.pkt_type) //类型
	if body == nil {
		buf.WriteUint32(0) //长度
	} else {
		buf.WriteUint32(uint32(len(*body))) //长度
	}
	buf.WriteInt32(head.result) //result
	if body != nil {
		buf.Append(*body)
	}

	data := buf.Buffer()
	leftCount := len(data)

	for leftCount > 0 {
		if this.cnServer == nil {
			this.onceStop.Do(this.stop)
			return false
		}
		n, err := this.cnServer.Write(data)
		if err != nil {
			log.Printf("send to %s fail! %s\n", this.serverAddr, err.Error())
			this.onceStop.Do(this.stop)
			return false
		}
		leftCount -= n
		data = data[n:]
	}

	return true
}

func (this *Session) onReqConnect(msgType uint32, msgLength uint32, result int32, data []byte) bool {
	log.Printf("request connect to %s\n", this.serviceAddr)

	head := &Head{}
	head.pkt_type = msgType
	head.result = int32(0)

	addr, err := net.ResolveTCPAddr("tcp", this.serviceAddr)
	if err != nil {
		log.Printf("net.ResolveTCPAddr: %v\n", err)
		this.sendToServer(head, nil)
		return false
	}
	cn, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		log.Printf("net.DialTCP: %v\n", err)
		this.sendToServer(head, nil)
		return false
	}
	cn.SetKeepAlive(true)
	log.Printf("connect to service(%s) successful\n", this.serviceAddr)
	this.cnService = cn
	this.chStatus <- 1
	go this.serviceLoop()
	head.result = int32(1)
	this.sendToServer(head, nil)

	return true
}

func (this *Session) onCPMsg(msgType uint32, msgLength uint32, result int32, data []byte) bool {
	switch msgType {
	case PKT_HEARTBEAT: //idle
		break
	case PKT_FORWARD_SERVER_DATA: //Server消息
		break
	case PKT_CONNECT: //请求连接指定的服务器
		return this.onReqConnect(msgType, msgLength, result, data)
	case PKT_FORWARD_CLIENT_DATA: //请求转发客户端消息
		return this.sendToService(&data)
	default:
		log.Printf("unknown message type=0x%08X, length=%d, result=%d\n", msgType, msgLength, result)
		return false
	}

	return true
}

func (this *Session) receiveServerMsg() (bool, uint32, int32, []byte) {
	var header = []byte{}
	var msgType, bodyLength uint32
	var result int32
	var body = []byte{}

	for {
		tmpBuf := make([]byte, 12 - len(header))
		n, err := this.cnServer.Read(tmpBuf)
		if err != nil {
			//log.Printf("server(%s) has closed!\n", g_config.ServerAddr)
			return false, 0, 0, nil
		}
		header = append(header, tmpBuf[:n]...)
		if len(header) >= 12 {
			msgType = uint32(header[0])
			msgType |= uint32(header[1]) << 8
			msgType |= uint32(header[2]) << 16
			msgType |= uint32(header[3]) << 24

			bodyLength = uint32(header[4])
			bodyLength |= uint32(header[5]) << 8
			bodyLength |= uint32(header[6]) << 16
			bodyLength |= uint32(header[7]) << 24

			result = int32(header[8])
			result |= int32(header[9]) << 8
			result |= int32(header[10]) << 16
			result |= int32(header[11]) << 24

			break
		}
	}

	if bodyLength > 0 {
		for {
			tmpBuf := make([]byte, bodyLength - uint32(len(body)))
			n, err := this.cnServer.Read(tmpBuf)
			if err != nil {
				//log.Printf("server(%s) has closed!\n", g_config.ServerAddr)
				return false, msgType, result, nil
			}
			body = append(body, tmpBuf[:n]...)
			if uint32(len(body)) >= bodyLength {
				break
			}
		}
	}

	return true, msgType, result, body
}

func (this *Session) serverLoop() {
	go this.sendIdleRoutine()

	buf := []byte{}
	tmpBuf := make([]byte, 2048)
	for {
		n, err := this.cnServer.Read(tmpBuf)
		if err != nil {
			//log.Printf("server(%s) has closed!\n", g_config.ServerAddr)
			break
		}
		buf = append(buf, tmpBuf[:n]...)
		if len(buf) < 12 {
			continue
		}
		for len(buf) >= 12 {
			//检查是否有一条完整的消息
			var msgType, msgLength uint32
			msgType = uint32(buf[0])
			msgType |= uint32(buf[1]) << 8
			msgType |= uint32(buf[2]) << 16
			msgType |= uint32(buf[3]) << 24

			msgLength = uint32(buf[4])
			msgLength |= uint32(buf[5]) << 8
			msgLength |= uint32(buf[6]) << 16
			msgLength |= uint32(buf[7]) << 24

			if uint32(len(buf)) < (12 + msgLength) {
				break
			}

			result := int32(buf[8])
			result |= int32(buf[9]) << 8
			result |= int32(buf[10]) << 16
			result |= int32(buf[11]) << 24
			r := this.onCPMsg(msgType, msgLength, result, buf[12:12+msgLength])
			if !r {
				goto Exit
			}
			buf = buf[12+msgLength:]
		}
	}

Exit:
	this.onceStop.Do(this.stop)
}

func (this *Session) serviceLoop() {
	buf := make([]byte, 2048)
	head := Head{}
	head.pkt_type = PKT_FORWARD_SERVER_DATA

	for {
		if this.cnService == nil {
			break
		}
		n, err := this.cnService.Read(buf)
		if err != nil {
			//log.Printf("service(%s) closed!\n", this.serviceAddr)
			break
		}

		body := buf[:n]
		if !this.sendToServer(&head, &body) {
			break
		}
	}

	this.onceStop.Do(this.stop)
}

func (this *Session) sendToService(data *[]byte) bool {
	if this.cnService == nil {
		return false
	}
	leftCount := len(*data)
	if leftCount == 0 {
		return false
	}

	for leftCount > 0 {
		n, err := this.cnService.Write(*data)
		if err != nil {
			log.Printf("send to %s: %s!\n", this.serviceAddr, err.Error())
			return false
		}
		leftCount -= n
		*data = (*data)[n:]
	}

	return true
}
