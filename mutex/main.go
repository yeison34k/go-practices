package main

//mutex resuelve el problema de coliciones relacionado al acceso de memoria

import (
	"fmt"
	"sync"
	"time"
)

var contador = 50
var mutex sync.Mutex
var user = make(map[string]any)

// recurso https://www.youtube.com/watch?v=Ad5hyNiW6z0
func main() {
	executeUser()
}

func executeUser() {
	go insert("v1", 1)
	go insert("v2", 2)
	go insert("v3", 3)
	go insert("v4", 4)
	go read()
	go insert("v5", 5)
	go insert("v6", 6)
	go insert("v7", "yeison")
	time.Sleep(time.Second)
}

func insert(key string, value any) {
	mutex.Lock()
	defer mutex.Unlock()
	user[key] = value
}

func read() {
	mutex.Lock()
	value, ok := user["v7"]
	mutex.Unlock()
	if ok {
		fmt.Println(value)
	}
	fmt.Println(ok)
}

func executeRestart() {
	go restart("g1")
	go restart("g2")
	go restart("g3")
	go restart("g4")
	go restart("g5")

	time.Sleep(time.Second)
	fmt.Println("contador: ", contador)
}

func restart(id string) {
	for {
		mutex.Lock() //protege la funcion y solo una go rutine puede acceder al tiempo la memoria
		if contador <= 0 {
			return
		}

		fmt.Println(id, "=>", contador)
		contador = contador - 1
		mutex.Unlock()
	}
}
