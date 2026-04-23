package ws

import (
	"log"

	"github.com/gorilla/websocket"
)

type Client struct {
	clientManager *ClientManager
	conn          *websocket.Conn
	writeChan     chan []byte
	readChan      chan []byte
	handler       Handler
}

func NewClient(clientManager *ClientManager, conn *websocket.Conn) *Client {
	return &Client{
		clientManager: clientManager,
		conn:          conn,
		writeChan:     make(chan []byte),
		readChan:      make(chan []byte),
	}
}

func (c *Client) ReadPump() {
	defer c.SendCloseSignal()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Fatalln(err.Error())
			return
		}
		log.Println(string(message))
		c.writeChan <- message
	}
}

func (c *Client) WritePump() {
	for {
		write := <-c.writeChan
		err := c.conn.WriteMessage(websocket.TextMessage, write)
		if err != nil {
			c.SendCloseSignal()
		}
	}
}

func (c *Client) SendCloseSignal() {
	c.clientManager.removeClientChan <- c
}
