package main

import (
	"log"

	uuid "github.com/satori/go.uuid"
)

// hub maintains the set of active clients and broadcasts messages to the
// clients.
type hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the clients.
	broadcast chan []byte

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	checkOrigin bool
}

type JoinInfo struct {
	GameID string
	Client *Client
}

func newHub(checkOrigin bool) *hub {
	return &hub{
		broadcast:   make(chan []byte),
		register:    make(chan *Client),
		unregister:  make(chan *Client),
		clients:     make(map[*Client]bool),
		checkOrigin: checkOrigin,
	}
}

func (h *hub) run() {
	for {
		select {
		case client := <-h.register:
			log.Println("WS Client Connected")
			h.clients[client] = true

			uuid := uuid.NewV4()
			client.clientID = uuid.String()

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcast:
			//log.Printf("Broadcasting Message: %+v", message)
			for client := range h.clients {
				client.send <- message
			}
		}
	}
}

func (h *hub) Write(p []byte) (n int, err error) {
	h.broadcast <- p
	return len(p), nil
}
