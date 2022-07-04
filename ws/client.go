package ws

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 5 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 30 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte

	//
	ClientId string

	//
	ClientName string

	//
	RoomId int
}

type Msg struct {
	Type           string            `json:"type"`
	RoomId         int               `json:"room_id"`
	Time           int64             `json:"time"`
	Content        string            `json:"content"`
	ClientId       string            `json:"client_id"`
	ClientName     string            `json:"client_name"`
	FromClientId   string            `json:"from_client_id"`
	FromClientName string            `json:"from_client_name"`
	ToClientId     string            `json:"to_client_id"`
	ToClientName   string            `json:"to_client_name"`
	ClientList     map[string]string `json:"client_list"`
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		//fmt.Println("ReadMessage1", string(message))
		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		//fmt.Println("ReadMessage2", string(message))

		// fmt.Println("c.conn.RemoteAddr().Network():", c.conn.RemoteAddr().Network())
		// fmt.Println("c.conn.RemoteAddr().String():", c.conn.RemoteAddr().String())

		var msg Msg
		json.Unmarshal(message, &msg)

		switch msg.Type {
		case "login":

			msg.ClientId = uuid.New().String()

			c.ClientId = msg.ClientId
			c.ClientName = msg.ClientName
			c.RoomId = msg.RoomId

			c.hub.register <- c

		case "say":

			msg.Time = time.Now().Unix()
			msg.FromClientId = c.ClientId
			msg.FromClientName = c.ClientName
			msg.ClientId = c.ClientId
			msg.ClientName = c.ClientName
			msg.RoomId = c.RoomId

			c.hub.broadcast <- msg

		}
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				fmt.Println("NextWriter", err)
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message.
			n := len(c.send)

			// if n >= 1 {
			// 	fmt.Println("message", string(message))
			// 	fmt.Println("n", n)
			// }

			for i := 0; i < n; i++ {
				w.Write(newline)
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			//fmt.Println("<-ticker.C", websocket.PingMessage)
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// serveWs handles websocket requests from the peer.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256)}
	// fmt.Println("conn.RemoteAddr().Network():", conn.RemoteAddr().Network())
	// fmt.Println("conn.RemoteAddr().String():", conn.RemoteAddr().String())
	// fmt.Println("conn.LocalAddr().Network():", conn.LocalAddr().Network())
	// fmt.Println("conn.LocalAddr().String():", conn.LocalAddr().String())

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}
