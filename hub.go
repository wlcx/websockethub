package hub

import (
	"net"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// Message is an incoming message from a client
type Message struct {
	Data       []byte
	RemoteAddr net.Addr
}

// Hub is a websocket connection pool
type Hub struct {
	clients          map[*Client]bool
	Broadcast        chan []byte
	register         chan *Client
	unregister       chan *Client
	incoming         chan *Message
	incomingHandler  func(*Message)
	onConnectHandler func(*Client)
}

// NewHub initialises a new hub
func NewHub() *Hub {
	return &Hub{
		Broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		incoming:   make(chan *Message),
	}
}

// SetIncomingHandler sets a handler function to be called on an incoming message, rather than it being discarded.
func (h *Hub) SetIncomingHandler(handler func(*Message)) {
	h.incomingHandler = handler
}

// SetOnConnectHandler sets a handler function to be called on a new client connection
func (h *Hub) SetOnConnectHandler(handler func(*Client)) {
	h.onConnectHandler = handler
}

// Run loops forever, processing incoming and outgoing messages, and registering and unregistering clients.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			if h.onConnectHandler != nil {
				h.onConnectHandler(client)
			}
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
		case message := <-h.Broadcast:
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.clients, client)
				}
			}
		case msg := <-h.incoming:
			if h.incomingHandler != nil {
				h.incomingHandler(msg)
			}
		}
	}
}

// HandleRequest upgrades an incoming http request to a websocket connection, and registers the client
func (h *Hub) HandleRequest(w http.ResponseWriter, r *http.Request) error {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	client := &Client{hub: h, Conn: conn, Send: make(chan []byte, 256)}
	client.hub.register <- client
	go client.writePump()
	client.readPump()

	return nil
}
