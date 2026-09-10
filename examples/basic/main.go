package main

import (
	"fmt"
	"log"
	"os"

	"wal"
)

func main() {
	path := "events.wal"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	journal, err := wal.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer journal.Close()

	sequence, err := journal.Append([]byte("order-created: 1234"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("appended entry %d\n", sequence)

	if err := journal.Replay(func(entry wal.Entry) error {
		fmt.Printf("replay %d: %s\n", entry.Sequence, entry.Data)
		return nil
	}); err != nil {
		log.Fatal(err)
	}
}
