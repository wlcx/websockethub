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
type Hub[T Byteable] struct {
	clients             map[*Client[T]]bool
	Broadcast           chan T
	register            chan *Client[T]
	unregister          chan *Client[T]
	incoming            chan *Message
	incomingHandler     func(*Message)
	onConnectHandler    func(*Client[T])
	onDisconnectHandler func(*Client[T])
	onClientSendHandler func(*Client[T], T) bool
}

// NewHub initialises a new hub
func NewHub[T Byteable]() *Hub[T] {
	return &Hub[T]{
		Broadcast:  make(chan T),
		register:   make(chan *Client[T]),
		unregister: make(chan *Client[T]),
		clients:    make(map[*Client[T]]bool),
		incoming:   make(chan *Message),
	}
}

// SetIncomingHandler sets a handler function to be called on an incoming message, rather than it being discarded.
func (h *Hub[T]) SetIncomingHandler(handler func(*Message)) {
	h.incomingHandler = handler
}

// SetOnConnectHandler sets a handler function to be called on a new client connection
func (h *Hub[T]) SetOnConnectHandler(handler func(*Client[T])) {
	h.onConnectHandler = handler
}

// SetOnDisconnectHandler sets a handler function to be called on a client disconnection
func (h *Hub[T]) SetOnDisconnectHandler(handler func(*Client[T])) {
	h.onDisconnectHandler = handler
}

// SetClientSendHandler sets a handler function to be called before sending a message to
// a client. The function returns a bool for whether the message should be sent to the
// client or not. This allows you to implement e.g. filtering logic using data sent in
// the client's original request.
func (h *Hub[T]) SetOnClientSendHandler(handler func(*Client[T], T) bool) {
	h.onClientSendHandler = handler
}

// Run loops forever, processing incoming and outgoing messages, and registering and unregistering clients.
func (h *Hub[T]) Run(ctx context.Context) {
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
				if h.onClientSendHandler != nil && !h.onClientSendHandler(client, message) {
					log.Debug("websockethub: onClientSendHandler returned false, skipping sending message")
					continue
				}
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
func (h *Hub[T]) HandleRequest(w http.ResponseWriter, r *http.Request) error {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	client := &Client[T]{
		hub:     h,
		Conn:    conn,
		Send:    make(chan T, 256),
		Request: r,
	}
	client.hub.register <- client
	go client.writePump()
	client.readPump()

	return nil
}
