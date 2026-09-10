package wal_test

import (
	"fmt"
	"os"

	"wal"
)

func Example() {
	file, err := os.CreateTemp("", "wal-example-*.log")
	if err != nil {
		panic(err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		panic(err)
	}
	defer os.Remove(path)

	log, err := wal.Open(path)
	if err != nil {
		panic(err)
	}
	defer log.Close()

	sequence, err := log.Append([]byte(`{"type":"user.created","id":42}`))
	if err != nil {
		panic(err)
	}
	fmt.Println(sequence)

	entry, err := log.Read(sequence)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(entry.Data))

	// Output:
	// 1
	// {"type":"user.created","id":42}
}
