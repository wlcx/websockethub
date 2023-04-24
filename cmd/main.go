package main

import (
	"context"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/wlcx/websockethub"
)

type Message string 

func (m Message) ToBytes() ([]byte, error) {
	return []byte(m), nil
}

func main() {
	h := hub.NewHub[Message]()
	h.SetOnConnectHandler(func (c *hub.Client[Message]) {
		log.Infof("Connected: %s", c.Conn.RemoteAddr())
	})
	h.SetOnDisconnectHandler(func (c *hub.Client[Message]) {
		log.Infof("Disconnected: %s", c.Conn.RemoteAddr())
	})
	h.SetOnClientSendHandler(func(c *hub.Client[Message], msg Message) bool {
		if filter := c.Request.URL.Query().Get("filter"); filter != "" {
			return string(msg) == filter
		}
		return true
	})
	go h.Run(context.Background())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		err := h.HandleRequest(w, r)
		if err != nil {
			log.Errorf("Error handling websocket conn: %v", err)
		}
	})

	go func() {
		for {
			h.Broadcast <- "hello"
			time.Sleep(500*time.Millisecond)
			h.Broadcast <- "goodbye"
			time.Sleep(500*time.Millisecond)
		}
	}()

	listenAddr := ":5555"
	log.Infof("Listening on %s", listenAddr)
	http.ListenAndServe(listenAddr, nil)
}
