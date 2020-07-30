package websocket

import (
	"errors"
	"github.com/gorilla/websocket"
	"net/http"
	"sync"
)

var wsUpgrader = websocket.Upgrader{
	// 允许所有CORS跨域请求
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// websocket消息
type WsMessage struct {
	MessageType int
	Data []byte
}

// 封装websocket连接
type WsConnection struct {
	wsSocket *websocket.Conn // 底层websocket
	inChan chan *WsMessage   // 读取队列
	outChan chan *WsMessage  // 发送队列

	closeOnce sync.Once // 避免重复关闭管道
	closeChan chan byte  // 关闭通知
}

// 读取协程
func (wsConn *WsConnection) wsReadLoop() {
	var (
		msgType int
		data []byte
		msg *WsMessage
		err error
	)
	for {
		// 读一个message
		if msgType, data, err = wsConn.wsSocket.ReadMessage(); err != nil {
			goto ERROR
		}
		msg = &WsMessage{
			msgType,
			data,
		}
		// 放入请求队列
		select {
		case wsConn.inChan <- msg:
		case <- wsConn.closeChan:
			goto CLOSED
		}
	}
ERROR:
	wsConn.Close()
CLOSED:
}

// 发送协程
func (wsConn *WsConnection) wsWriteLoop() {
	var (
		msg *WsMessage
		err error
	)
	for {
		select {
		// 取一个应答
		case msg = <- wsConn.outChan:
			// 写给websocket
			if err = wsConn.wsSocket.WriteMessage(msg.MessageType, msg.Data); err != nil {
				goto ERROR
			}
		case <- wsConn.closeChan:
			goto CLOSED
		}
	}
ERROR:
	wsConn.Close()
CLOSED:
}

/************** 并发安全 API **************/

func InitWebsocket(resp http.ResponseWriter, req *http.Request) (wsConn *WsConnection, err error) {
	var (
		wsSocket *websocket.Conn
	)
	// 应答客户端告知升级连接为websocket
	if wsSocket, err = wsUpgrader.Upgrade(resp, req, nil); err != nil {
		return
	}
	wsConn = &WsConnection{
		wsSocket: wsSocket,
		inChan: make(chan *WsMessage, 1000),
		outChan: make(chan *WsMessage, 1000),
		closeChan: make(chan byte),
	}

	// 读协程
	go wsConn.wsReadLoop()
	// 写协程
	go wsConn.wsWriteLoop()

	return
}

// 发送消息
func (wsConn *WsConnection) Write(messageType int, data []byte) (err error) {
	select {
	case wsConn.outChan <- &WsMessage{messageType, data,}:
	case <- wsConn.closeChan:
		err = errors.New("websocket closed")
	}
	return
}

// 读取消息
func (wsConn *WsConnection) Read() (msg *WsMessage, err error) {
	select {
	case msg = <- wsConn.inChan:
		return
	case <- wsConn.closeChan:
		err = errors.New("websocket closed")
	}
	return
}

// 关闭连接
func (wsConn *WsConnection) Close() {
	wsConn.closeOnce.Do(func() {
		wsConn.wsSocket.Close()
		close(wsConn.closeChan)
	})
}