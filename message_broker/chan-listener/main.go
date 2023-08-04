package main

import (
	"chanlistener/messanger"
	"fmt"
	"log"
	"math/rand"
	"time"
)

type Person struct {
	ID   int
	Name string
}

type Animal struct {
	ID   int
	Name string
	Type string
}

func handler1(msg any) error {
	log.Println("msg recive person", msg)
	return nil
}

func handler2(msg any) error {
	log.Println("msg recive animals", msg)
	return nil
}

func main() {
	m := messanger.New()
	m.AddHandler(handler1, "person")
	m.AddHandler(handler2, "animal")

	go func() {
		for {
			rand.NewSource(time.Now().UnixNano())
			num := rand.Intn(100)
			if num%2 == 0 {
				m.SendMessage(messanger.Message{
					Topic: "person",
					Data: Person{
						ID:   num,
						Name: "Person: " + fmt.Sprint(num),
					},
				})
			} else {
				m.SendMessage(messanger.Message{
					Topic: "animal",
					Data: Animal{
						ID:   num,
						Name: "Animal: " + fmt.Sprint(num),
						Type: "dog",
					},
				})
			}
		}
	}()

	go m.Start()
	time.Sleep(time.Second)
}
