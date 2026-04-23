package ws

type ClientManager struct {
	clients          map[*Client]bool
	addClientChan    chan *Client
	removeClientChan chan *Client
}

func NewClientManager() *ClientManager {
	return &ClientManager{
		clients:          make(map[*Client]bool),
		addClientChan:    make(chan *Client),
		removeClientChan: make(chan *Client),
	}
}

func (cm *ClientManager) Run() {
	for {
		select {
		case client := <-cm.addClientChan:
			cm.clients[client] = true
		case client := <-cm.removeClientChan:
			delete(cm.clients, client)
			client.conn.Close()
		}
	}
}

func (cm *ClientManager) Add(client *Client) {
	cm.addClientChan <- client
}

func (cm *ClientManager) Remove(client *Client) {
	cm.removeClientChan <- client
}
