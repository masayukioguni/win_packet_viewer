package main

import (
	"fmt"
	"github.com/go-martini/martini"
	"github.com/gorilla/websocket"
	"github.com/martini-contrib/render"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type WinFormatBson struct {
	Id          bson.ObjectId `bson:"_id,omitempty"`
	Sequence    uint8         `bson:"Sequence"`
	SubSequence uint8         `bson:"SubSequence"`
	A0          byte          `bson:"A0"`
	Length      uint16        `bson:"Length"`
	Datetime    int64         `bson:"Datetime"`
	Channel     uint16        `bson:"Channel"`
	Rate        uint16        `bson:"Rate"`
	Size        uint16        `bson:"Size"`
	FirstSample int32         `bson:"FirstSample"`
	Sampling    []int32       `bson:"Sampling"`
}

// https://github.com/patcito/martini-gorilla-websocket-chat-example/blob/master/server.go

var ActiveClients = make(map[ClientConn]int)
var ActiveClientsRWMutex sync.RWMutex

type ClientConn struct {
	websocket *websocket.Conn
	clientIP  net.Addr
}

func addClient(cc ClientConn) {
	ActiveClientsRWMutex.Lock()
	ActiveClients[cc] = 0
	ActiveClientsRWMutex.Unlock()
}

func deleteClient(cc ClientConn) {
	ActiveClientsRWMutex.Lock()
	delete(ActiveClients, cc)
	ActiveClientsRWMutex.Unlock()
}

func broadcastMessage(data WinFormatBson) {
	ActiveClientsRWMutex.RLock()
	defer ActiveClientsRWMutex.RUnlock()

	for client, _ := range ActiveClients {
		if err := client.websocket.WriteJSON(data); err != nil {
			return
		}
	}
}

func main() {
	m := martini.Classic()
	m.Use(render.Renderer())
	m.Use(martini.Static("assets"))

	m.Get("/", Index)
	m.Get("/ws", WebSocket)
	m.Post("/packet", Packet)

	m.Run()
}
func Packet(res render.Render, req *http.Request) {
	body, _ := ioutil.ReadAll(req.Body)
	log.Println(string(body))
	res.JSON(200, req)
}
func Index(r render.Render) {
	r.HTML(200, "index", "")
}

func WebSocket(w http.ResponseWriter, r *http.Request) {
	log.Println("WebSocket:", r)
	ws, err := websocket.Upgrade(w, r, nil, 1024, 1024)
	if _, ok := err.(websocket.HandshakeError); ok {
		http.Error(w, "Not a websocket handshae", 400)
		return
	} else if err != nil {
		log.Println(err)
		return
	}

	client := ws.RemoteAddr()
	clientConn := ClientConn{ws, client}
	addClient(clientConn)

	session, err := mgo.Dial("localhost")
	if err != nil {
		panic(err)
	}
	defer session.Close()

	// Optional. Switch the session to a monotonic behavior.
	session.SetMode(mgo.Monotonic, true)

	c := session.DB("fluent").C("debug.format")
	result := WinFormatBson{}

	_ = c.Find(bson.M{"Channel": 1}).Sort("-_id").One(&result)
	fmt.Println(result)
	broadcastMessage(result)
	iter := c.Find(bson.M{"_id": bson.M{"$gt": result.Id}, "Channel": 1}).Tail(1 * time.Second)

	for {
		var lastId bson.ObjectId
		for iter.Next(&result) {
			//fmt.Println(result)
			lastId = result.Id
			broadcastMessage(result)
		}
		if iter.Err() != nil {
			fmt.Println(iter.Err())
			iter.Close()
			return
		}
		if iter.Timeout() {
			continue
		}
		query := c.Find(bson.M{"_id": bson.M{"$gt": lastId}, "Channel": 1})
		iter = query.Sort("$natural").Tail(1 * time.Second)
	}
	iter.Close()

}
