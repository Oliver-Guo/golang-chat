package ws

import (
	"encoding/json"
	"sync"
	"time"
)

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	mutex sync.RWMutex
	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the clients.
	broadcast chan Msg

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	roomsClients map[int][]*Client
}

func NewHub() *Hub {
	return &Hub{
		broadcast:    make(chan Msg),
		register:     make(chan *Client),
		unregister:   make(chan *Client),
		clients:      make(map[*Client]bool),
		roomsClients: make(map[int][]*Client),
	}
}

func (h *Hub) ClientRemoveRoom(client *Client, index int) {
	h.mutex.Lock()
	h.roomsClients[client.RoomId] = append(h.roomsClients[client.RoomId][:index], h.roomsClients[client.RoomId][index+1:]...)
	//fmt.Println("<-h.unregister END", client.ClientName)
	h.mutex.Unlock()
}

func (h *Hub) ClientToRoom(client *Client) {
	h.mutex.Lock()
	h.roomsClients[client.RoomId] = append(h.roomsClients[client.RoomId], client)
	//fmt.Println("<-h.register", h.roomsClients[client.RoomId])
	h.mutex.Unlock()
}

func (h *Hub) GetRoomClients(roomId int) []*Client {
	h.mutex.RLock()
	roomClients := h.roomsClients[roomId]
	h.mutex.RUnlock()

	return roomClients
}
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			//fmt.Println("<-h.register INIT", client.ClientName)
			roomClients := h.GetRoomClients(client.RoomId)

			clientList := make(map[string]string)

			msg := Msg{
				Type:       "login",
				Time:       time.Now().Unix(),
				RoomId:     client.RoomId,
				ClientId:   client.ClientId,
				ClientName: client.ClientName,
			}

			sendToRoom, _ := json.Marshal(msg)

			for _, roomClient := range roomClients {

				clientList[roomClient.ClientId] = roomClient.ClientName

				roomClient.send <- sendToRoom

			}

			h.clients[client] = true
			h.ClientToRoom(client)

			msg.ClientList = clientList
			sendToCurrentClient, _ := json.Marshal(msg)

			client.send <- sendToCurrentClient

		case client := <-h.unregister:
			//fmt.Println("<-h.unregister INIT", client.ClientName)
			if _, ok := h.clients[client]; ok {

				roomClients := h.GetRoomClients(client.RoomId)

				removeIndex := -1

				for index, roomClient := range roomClients {

					if client.ClientId == roomClient.ClientId {

						removeIndex = index

					} else {

						msg := Msg{
							Type:           "logout",
							Time:           time.Now().Unix(),
							RoomId:         client.RoomId,
							FromClientId:   client.ClientId,
							FromClientName: client.ClientName,
						}

						sendToRoom, _ := json.Marshal(msg)
						//fmt.Println("<-h.unregister SAY", roomClient.ClientName)
						roomClient.send <- sendToRoom
					}
				}

				if removeIndex > -1 {
					h.ClientRemoveRoom(client, removeIndex)
				}

				delete(h.clients, client)

				close(client.send)
			}
		case msg := <-h.broadcast:

			roomClients := h.GetRoomClients(msg.RoomId)

			var message []byte

			origContent := msg.Content

			for index, roomClient := range roomClients {

				if msg.ToClientId != "all" {

					if msg.ToClientId == roomClient.ClientId {

						msg.Content = "<b>對你说: </b>" + origContent

					} else if msg.ClientId == roomClient.ClientId {

						msg.Content = "<b>你對" + msg.ToClientName + "说: </b>" + origContent

					} else {

						continue

					}

				}

				message, _ = json.Marshal(msg)

				select {
				case roomClient.send <- message:
				default:

					if msg.ClientId == roomClient.ClientId {

						h.ClientRemoveRoom(roomClient, index)

					}

					close(roomClient.send)

					delete(h.clients, roomClient)
				}
			}

		}
	}
}
