package main

import (
	"encoding/json"
	"errors"
	"github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"
)

type Session struct {
	mx    sync.Mutex
	conn  *websocket.Conn
	mqtt  mqtt.Client
	ready bool
}

func (s *Session) WriteJSON(v interface{}) error {
	s.mx.Lock()
	defer s.mx.Unlock()
	if !s.ready {
		return errors.New("WS not ready")
	}
	data, err := json.Marshal(v)
	if err != nil {
		log.Println(err)

	} else {
		log.Printf("%s", data)
	}
	return s.conn.WriteJSON(v)
}
func (s *Session) Ready(conn *websocket.Conn) {
	s.mx.Lock()
	defer s.mx.Unlock()
	s.conn = conn
	s.ready = true

}

func (s *Session) NotReady() {
	s.mx.Lock()
	defer s.mx.Unlock()
	s.ready = false

}

var session *Session

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func ServeWS(w http.ResponseWriter, r *http.Request) {
	log.Printf("ws Start")
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	defer c.Close()
	session.Ready(c)
	defer session.NotReady()

	opts := mqtt.NewClientOptions().AddBroker("tcp://localhost:1883").SetClientID("gotrivial")
	opts.SetKeepAlive(15 * time.Second)
	// opts.SetDefaultPublishHandler(f)
	opts.SetPingTimeout(1 * time.Second)

	session.mqtt = mqtt.NewClient(opts)
	defer session.mqtt.Disconnect(500)

	if token := session.mqtt.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	if token := session.mqtt.Subscribe("$aws/things/+/shadow/update/accepted", 0, f); token.Wait() && token.Error() != nil {
		log.Println(token.Error())
		return
		os.Exit(1)
	}

	if token := session.mqtt.Subscribe("$aws/things/+/shadow/get/accepted", 0, f); token.Wait() && token.Error() != nil {
		log.Println(token.Error())
		return
		os.Exit(1)
	}

	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		log.Println("Message: %s", message)

		var data map[string]interface{}

		err = json.Unmarshal(message, &data)
		if err != nil {
			log.Println(err)
			continue
		}

		if value, ok := data["id"]; ok {
			if action, ok := data["action"]; ok {
				switch action {
				case "get":
					if token := session.mqtt.Publish("$aws/things/"+value.(string)+"/shadow/get", 0, false, ""); token.Wait() && token.Error() != nil {
						log.Println(token.Error())
						return
						os.Exit(1)
					}
				case "update":
					if payload, ok := data["payload"]; ok {
						jsonpayload, err := json.Marshal(payload)
						if err != nil {
							log.Println(err)
							return
						}
						log.Printf("UPDATING: %s", jsonpayload)
						if token := session.mqtt.Publish("$aws/things/"+value.(string)+"/shadow/update", 0, false, string(jsonpayload)); token.Wait() && token.Error() != nil {
							log.Println(token.Error())
							return
							os.Exit(1)
						}
					}
				}
			}
		}
	}
}

var f mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	log.Printf("TOPIC: %s\n", msg.Topic())
	log.Printf("MSG: %s\n", msg.Payload())

	data := map[string]string{
		"topic":   msg.Topic(),
		"payload": string(msg.Payload()),
	}

	err := session.WriteJSON(data)
	if err != nil {
		log.Println(err)
	}
}

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		log.Println("Ending")
		os.Exit(0)
	}()
	log.Println("Starting gpio API")

	session = &Session{}

	//mqtt.DEBUG = log.New(os.Stdout, "", 0)
	mqtt.ERROR = log.New(os.Stdout, "", 0)

	r := mux.NewRouter()
	r.PathPrefix("/ws").
		HandlerFunc(ServeWS)

	r.PathPrefix("/").
		Methods("GET").
		Handler(http.FileServer(http.Dir("./")))

	loggedRouter := handlers.LoggingHandler(os.Stdout, r)

	srv := &http.Server{
		Handler:      loggedRouter,
		Addr:         ":4430",
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Printf("IP: %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
