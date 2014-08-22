package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/bobappleyard/readline"
	// "github.com/funkygao/golib/profile"
)

func main() {
	// defer profile.Start(profile.CPUProfile).Stop()

	log.SetFlags(log.Lshortfile)

	rand.Seed(time.Now().UnixNano())

	if len(os.Args) < 2 {
		log.Fatal("usage: ./example /path/to/download/to")
	}

	config := ClientConfig{
		Port: uint16(20000 + rand.Intn(1000)),
	}

	copy(config.PeerId[:], fmt.Sprintf("-gtr-%x", rand.Int63()))

	client := NewClient(config)

	for {
		line, err := readline.String("> ")
		if err != nil {
			log.Fatal(err)
		}

		if len(line) == 0 {
			continue
		}

		if line[0:4] == "add " {
			if session, err := client.Add(line[4:]); err != nil {
				log.Printf("error: %s", err.Error())
			} else {
				log.Printf("added: %x", session.InfoHash())
			}
		}
	}
}
