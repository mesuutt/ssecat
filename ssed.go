package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// Example SSE server in Golang.
//     $ go run sse.go

type Broker struct {

	// Events are pushed to this channel by the main events-gathering routine
	Notifier chan []byte

	// New client connections
	newClients chan chan []byte

	// Closed client connections
	closingClients chan chan []byte

	// Client connections registry
	clients map[chan []byte]bool
}

func NewServer() (broker *Broker) {
	// Instantiate a broker
	broker = &Broker{
		Notifier:       make(chan []byte, 1),
		newClients:     make(chan chan []byte),
		closingClients: make(chan chan []byte),
		clients:        make(map[chan []byte]bool),
	}

	// Set it running - listening and broadcasting events
	go broker.listen()

	return
}

func (broker *Broker) ServeHTTP(rw http.ResponseWriter, req *http.Request) {

	// Make sure that the writer supports flushing.
	//
	flusher, ok := rw.(http.Flusher)

	if !ok {
		http.Error(rw, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("Access-Control-Allow-Origin", "*")

	// Each connection registers its own message channel with the Broker's connections registry
	messageChan := make(chan []byte)

	// Signal the broker that we have a new connection
	broker.newClients <- messageChan

	// Remove this client from the map of connected clients
	// when this handler exits.
	defer func() {
		broker.closingClients <- messageChan
	}()

	// Listen to connection close and un-register messageChan
	notify := rw.(http.CloseNotifier).CloseNotify()

	go func() {
		<-notify
		broker.closingClients <- messageChan
	}()

	for {

		// Write to the ResponseWriter
		// Server Sent Events compatible
		fmt.Fprintf(rw, "data: %s\n\n", <-messageChan)

		// Flush the data immediatly instead of buffering it for later.
		flusher.Flush()
	}

}

func (broker *Broker) listen() {
	for {
		select {
		case s := <-broker.newClients:

			// A new client has connected.
			// Register their message channel
			broker.clients[s] = true
		case s := <-broker.closingClients:

			// A client has dettached and we want to
			// stop sending them messages.
			delete(broker.clients, s)
		case event := <-broker.Notifier:

			// We got a new event from the outside!
			// Send event to all connected clients
			for clientMessageChan, _ := range broker.clients {
				clientMessageChan <- event
			}
		}
	}

}

func PromptHandler(broker *Broker) {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("(%d clients)-> ", len(broker.clients))
		line, _, err := reader.ReadLine()
		if err != nil {
			fmt.Println(err)
		}

		if len(line) > 0 {
			fmt.Printf("Sent message: %s\n", string(line))
			broker.Notifier <- []byte(line)
		}
	}
}

func main() {

	promptPtr := flag.Bool("p", false, "Show prompt for message which send to clients")
	addrPtr := flag.String("l", "localhost:3000", "Listening address and port")
	verbosePtr := flag.Bool("v", false, "Verbose debug messages")
	flag.CommandLine.Parse(os.Args[1:])

	if *verbosePtr {
		fmt.Println("Verbose mode on")
	}

	broker := NewServer()

	if *promptPtr {
		go PromptHandler(broker)
	} else {
		fmt.Println("Reading from STDIN")
		go func() {
			scan := bufio.NewScanner(os.Stdin)
			for scan.Scan() {
				broker.Notifier <- scan.Bytes()

				if *verbosePtr {
					currentTime := time.Now().Local()
					fmt.Printf("[%s] %d clients: %s\n", currentTime.Format("2006-01-02 15:04:05"), len(broker.clients), scan.Bytes())
				}
			}
		}()
	}

	fmt.Println("Listening on ", *addrPtr)
	log.Fatal("HTTP server error: ", http.ListenAndServe(*addrPtr, broker))

}
