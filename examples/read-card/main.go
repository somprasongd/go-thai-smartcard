package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (err error) {
	transport, err := smc.NewTransport()
	if err != nil {
		return fmt.Errorf("open PC/SC transport: %w", err)
	}
	reader := smc.NewSmartCardWith(transport)
	defer func() {
		if closeErr := reader.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close PC/SC transport: %w", closeErr)
		}
	}()

	// nil watches all attached readers; pass &name to select a specific reader.
	data, err := reader.Read(nil, &smc.Options{ShowFaceImage: true})
	if err != nil {
		return fmt.Errorf("read card: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(data)
}
