package hub

import (
	"context"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
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
	clients             map[*Client]bool
	Broadcast           chan []byte
	register            chan *Client
	unregister          chan *Client
	incoming            chan *Message
	incomingHandler     func(*Message)
	onConnectHandler    func(*Client)
	onDisconnectHandler func(*Client)
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

// SetOnDisconnectHandler sets a handler function to be called on a client disconnection
func (h *Hub) SetOnDisconnectHandler(handler func(*Client)) {
	h.onDisconnectHandler = handler
}

// Run loops forever, processing incoming and outgoing messages, and registering and unregistering clients.
func (h *Hub) Run(ctx context.Context) {
	log.Debug("websockethub: starting")
mainloop:
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			if h.onConnectHandler != nil {
				h.onConnectHandler(client)
			}
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				log.Debug("websockethub: unregistering client")
				if h.onDisconnectHandler != nil {
					h.onDisconnectHandler(client)
				}
				delete(h.clients, client)
				close(client.Send)
			} else {
				log.Debug("websockethub: got unregister for non existent client")
			}
		case message := <-h.Broadcast:
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					log.Debug("websockethub: client send failed, unregistering...")
					h.unregister <- client
				}
			}
		case msg := <-h.incoming:
			if h.incomingHandler != nil {
				h.incomingHandler(msg)
			}
		case <-ctx.Done():
			log.Debugf("websockethub stopping, disconnecting %d clients", len(h.clients))
			for client := range h.clients {
				close(client.Send)
				delete(h.clients, client)
			}
			break mainloop
		}
	}
	log.Debug("websockethub done")
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
