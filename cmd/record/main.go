// Command record captures a real card session into a trace file.
//
// Run it once against a reader and a physical Thai ID card. The resulting
// trace becomes the ground truth that pkg/transport.FakeCard replays in tests,
// so the card logic can be exercised without hardware from then on.
//
//	go run ./cmd/record -out testdata/trace-real.json -reader "Identive CLOUD 2700 R Smart Card Reader"
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

func main() {
	var (
		out        = flag.String("out", "testdata/trace-real.json", "where to write the trace")
		readerName = flag.String("reader", "", "reader to use; empty means the first attached reader")
		name       = flag.String("name", "thai-id", "name recorded in the trace")
		note       = flag.String("note", "", "note recorded in the trace")
		faceImage  = flag.Bool("image", true, "read the face image, which makes the trace large")
		nhso       = flag.Bool("nhso", false, "read the NHSO applet")
		laser      = flag.Bool("laser", true, "read the laser code")
		listOnly   = flag.Bool("list", false, "list the readers PC/SC can see and exit")
	)
	flag.Parse()

	if *listOnly {
		if err := listReaders(); err != nil {
			log.Fatalf("record: %v", err)
		}
		return
	}

	if err := run(*out, *name, *note, *readerName, *faceImage, *nhso, *laser); err != nil {
		log.Fatalf("record: %v", err)
	}
}

// listReaders reports what the library can see, which is the first thing to
// check when a capture fails.
func listReaders() error {
	t, err := smc.NewTransport()
	if err != nil {
		return err
	}
	defer t.Close()

	readers, err := t.ListReaders()
	if err != nil {
		return err
	}
	if len(readers) == 0 {
		log.Println("PC/SC is available but reports no readers.")
		log.Println("On macOS that usually means Apple's own CCID driver is in use instead of")
		log.Println("the IFD CCID driver. See the macOS section of the README.")
		log.Println("The reader must show up here before a trace can be recorded.")
		return nil
	}
	for i, reader := range readers {
		log.Printf("[%d] %s", i, reader)
	}
	return nil
}

func run(out, name, note, readerName string, faceImage, nhso, laser bool) error {
	t, err := smc.NewTransport()
	if err != nil {
		return err
	}
	defer t.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return capture(ctx, t, out, name, note, readerName, faceImage, nhso, laser)
}

func capture(ctx context.Context, t transport.Transport, out, name, note, readerName string, faceImage, nhso, laser bool) error {
	readers, err := t.ListReaders()
	if err != nil {
		return err
	}
	if len(readers) == 0 {
		return fmt.Errorf("no reader attached")
	}

	reader := readerName
	if reader == "" {
		reader = readers[0]
	} else if !contains(readers, reader) {
		return fmt.Errorf("reader %q not attached; available: %s", reader, strings.Join(readers, ", "))
	}
	log.Printf("Using reader: %s", reader)
	log.Println("Waiting for an inserted card; Ctrl+C cancels.")
	for {
		_, err := t.WaitCardPresent(ctx, []string{reader})
		if errors.Is(err, transport.ErrCardTimeout) {
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	card, err := t.Connect(reader)
	if err != nil {
		return err
	}

	defer func() {
		if err := card.Disconnect(); err != nil {
			log.Printf("disconnect: %v", err)
		}
	}()
	rec := transport.Record(card, name)
	status, err := rec.Status()
	if err != nil {
		return err
	}
	response := util.GetResponseCommand(status.Atr)
	rec.Trace().Note = note

	// Read through the normal card logic so the captured trace matches the
	// sequence the library actually issues.
	smcReader := smc.NewPersonalReader(rec, response)
	if err := smcReader.Select(); err != nil {
		return fmt.Errorf("select personal applet: %w", err)
	}
	smcReader.Read(faceImage)

	if laser {
		cardReader := smc.NewCardReader(rec, response)
		if err := cardReader.Select(); err != nil {
			return fmt.Errorf("select card applet: %w", err)
		}
		cardReader.ReadLaserId()
	}

	if nhso {
		nhsoReader := smc.NewNhsoReader(rec, response)
		if err := nhsoReader.Select(); err != nil {
			return fmt.Errorf("select nhso applet: %w", err)
		}
		nhsoReader.Read()
	}

	trace := rec.Trace()
	if len(trace.Exchanges) == 0 {
		return fmt.Errorf("no exchanges recorded")
	}
	if err := trace.Save(out); err != nil {
		return err
	}
	report(trace, out)
	return nil
}

func report(trace *transport.Trace, out string) {
	b, _ := json.MarshalIndent(map[string]any{
		"file":      out,
		"name":      trace.Name,
		"atr":       trace.Atr,
		"exchanges": len(trace.Exchanges),
	}, "", "  ")
	fmt.Println(string(b))
	log.Printf("Wrote %d exchanges to %s", len(trace.Exchanges), out)
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
