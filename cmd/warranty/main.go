package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"example.com/warranty-repair-parts/internal/warranty"
)

func main() {
	stateDir := flag.String("state", "", "directory for persisted state")
	flag.Parse()

	store, err := warranty.OpenStore(*stateDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	enc := json.NewEncoder(writer)

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			dispatchLine(store, line, enc, writer)
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func dispatchLine(store *warranty.Store, line []byte, enc *json.Encoder, w *bufio.Writer) {
	var req warranty.Request
	if err := json.Unmarshal(line, &req); err != nil {
		writeError(enc, w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if err := enc.Encode(store.Dispatch(req)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	w.Flush()
}

func writeError(enc *json.Encoder, w *bufio.Writer, msg string) {
	_ = enc.Encode(warranty.Response{OK: false, Error: msg})
	w.Flush()
}
