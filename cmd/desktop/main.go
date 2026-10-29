package main

import (
	"log"

	"myanime/internal/desktop"
)

func main() {
	if err := desktop.Run(); err != nil {
		log.Fatal(err)
	}
}
