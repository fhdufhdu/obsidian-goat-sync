package ws

type Hub struct {
	clients          map[*Client]bool
	addClientChan    chan *Client
	removeClientChan chan *Client
}

func NewHub() *Hub {
	return &Hub{
		clients:          make(map[*Client]bool),
		addClientChan:    make(chan *Client),
		removeClientChan: make(chan *Client),
	}
}

func (h *Hub) ManageClient() {
	for {
		select {
		case client := <-h.addClientChan:
			h.clients[client] = true
		case client := <-h.removeClientChan:
			delete(h.clients, client)
			client.conn.Close()
		}
	}
}

func (h *Hub) AddClinet(client *Client) {
	h.addClientChan <- client
}

func (h *Hub) RemoveClient(client *Client) {
	h.removeClientChan <- client
}
