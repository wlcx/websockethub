# WebsocketHub

A websocket server hub. Common functionality needed for a couple of projects, does not claim to be high quality or production ready!

Based heavily on https://github.com/gorilla/websocket/tree/master/examples/chat

## Using
```
h := hub.NewHub()
h.SetOnConnectHandler(func(client *hub.Client) {
    fmt.Println("Client connected!")
    client.Send <- []byte("Hello!")
})

go h.Run()
```
