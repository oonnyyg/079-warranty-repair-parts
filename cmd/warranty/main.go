package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"example.com/warranty-repair-parts/internal/warranty"
)

func main() {
	stateDir := flag.String("state", "", "directory holding the persistent state file")
	flag.Parse()
	if *stateDir == "" {
		fmt.Fprintln(os.Stderr, "usage: warranty --state <state-dir>")
		os.Exit(2)
	}
	store, err := warranty.Load(*stateDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load state:", err)
		os.Exit(1)
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		writer.Write(store.HandleLine(line))
		writer.WriteByte('\n')
		writer.Flush()
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "read stdin:", err)
		os.Exit(1)
	}
}
