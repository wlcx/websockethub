package hub

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

// Something that can be turned into []byte.
// This is the interface over which websockethub is generic.
type Byteable interface {
	ToBytes() ([]byte, error)
}

// Client is a websocket Client
type Client[T Byteable] struct {
	hub     *Hub[T]
	Conn    *websocket.Conn
	Send    chan T
	Request *http.Request
}

func (c *Client[T]) readPump() {
	log := log.WithField("client", c.Conn.RemoteAddr())
	log.Debug("websockethub: readpump starting")
	defer func() {
		c.hub.unregister <- c
		c.Conn.Close()
	}()
	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error { c.Conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	for {
		_, msg, err := c.Conn.ReadMessage()
		if err != nil {
			log.Debugf("websockethub: error reading message: %v", err)
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Warnf("websockethub: closed unexpectedly: %v", err)
			}
			break
		}
		c.hub.incoming <- &Message{msg, c.Conn.RemoteAddr()}
	}
	log.Debug("websockethub: readpump done")

}

func (c *Client[T]) writePump() {
	log := log.WithField("client", c.Conn.RemoteAddr())
	log.Debug("websockethub: writepump starting")
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub Closed channel
				log.Debug("websockethub: disconnecting client")
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				log.Debugf("websockethub: error getting writer for conn: %v", err)
				return
			}
			encoded, err := message.ToBytes()
			if err != nil {
				log.Errorf("websockethub: error encoding message to bytes: %s", err)
				return
			}
			_, err = w.Write(encoded)
			if err != nil {
				log.Debugf("websockethub: error writing to client: %v", err)
				return
			}

			// Add queued messages too
			for i := 0; i < len(c.Send); i++ {
				encoded, err := (<-c.Send).ToBytes()
				if err != nil {
					log.Errorf("websockethub: error encoding message to bytes: %s", err)
					continue
				}
				_, err = w.Write(encoded)
				if err != nil {
					log.Debugf("websockethub: error writing to client: %v", err)
					return
				}
			}

			if err := w.Close(); err != nil {
				if err != nil {
					log.Debugf("websockethub: error closing writer: %v", err)
					return
				}
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				log.Errorf("websockethub: error writing ping to client: %v", err)
				return
			}
		}
	}
}
