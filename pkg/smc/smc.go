package smc

import (
	"errors"
	"log"

	"github.com/ebfe/scard"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

type Options struct {
	ShowFaceImage bool
	ShowNhsoData  bool
}

type smartCard struct {
	Options
}

func NewSmartCard(opt *Options) *smartCard {
	if opt == nil {
		opt = &Options{
			ShowFaceImage: false,
			ShowNhsoData:  false,
		}
	}
	return &smartCard{
		Options: *opt,
	}
}

func (s *smartCard) StartDemon() error {
	// Establish a context
	ctx, err := scard.EstablishContext()
	if err != nil {
		return err
	}

	// List available readers
	readers, err := ctx.ListReaders()
	if err != nil {
		return err
	}

	log.Printf("Available %d readers:\n", len(readers))
	for i, reader := range readers {
		log.Printf("[%d] %s\n", i, reader)
	}

	if len(readers) > 0 {
		rs := s.initReaderStates(readers)
		for {
			log.Println("Waiting for a Card Inserted")
			index, err := s.waitUntilCardPresent(ctx, rs)
			if err != nil {
				log.Printf("waiting card error %s", err.Error())
				continue
			}

			// Connect to card
			reader := readers[index]
			log.Printf("Connecting to card with %s", reader)
			card, err := ctx.Connect(reader, scard.ShareExclusive, scard.ProtocolAny)
			if err != nil {
				log.Printf("connecting card error %s", err.Error())
				card.Disconnect(scard.UnpowerCard)
				continue
			}

			// Todo - send event card inserted

			status, err := card.Status()
			if err != nil {
				log.Printf("get card status error %s", err.Error())
				card.Disconnect(scard.UnpowerCard)
				continue
			}

			// log.Printf("\treader: %s\n\tstate: %x\n\tactive protocol: %x\n\tatr: % x\n",
			// 	status.Reader, status.State, status.ActiveProtocol, status.Atr)

			cmd := util.GetResponseCommand(status.Atr)

			// respCmd, err := card.Transmit(cmd)
			// if err != nil {
			// 	log.Printf("card transmit cmd error %s", err.Error())
			// 	card.Disconnect(scard.UnpowerCard)
			// 	continue
			// }

			// log.Println(respCmd)

			personalReader := NewPersonalReader(card, cmd)
			personalReader.Check()
			cid := personalReader.ReadCID()
			name := personalReader.ReadName()

			log.Println(cid, name)

			log.Printf("Disconnect card")
			card.Disconnect(scard.UnpowerCard)
		}
	}

	return errors.New("not available readers")
}

func (s smartCard) initReaderStates(readers []string) []scard.ReaderState {
	rs := make([]scard.ReaderState, len(readers))
	for i := range rs {
		rs[i].Reader = readers[i]
		rs[i].CurrentState = scard.StateUnaware
	}
	return rs
}

func readCard() {

}

func getResponseCommand(atr []byte) []byte {
	if atr[0] == 0x3B && atr[1] == 0x67 {
		return []byte{0x00, 0xc0, 0x00, 0x01}
	}
	return []byte{0x00, 0xc0, 0x00, 0x00}
}

func (s smartCard) waitUntilCardPresent(ctx *scard.Context, rs []scard.ReaderState) (int, error) {
	// fmt.Println(
	// 	scard.StateUnaware,
	// 	scard.StateIgnore,
	// 	scard.StateChanged,
	// 	scard.StateUnknown,
	// 	scard.StateUnavailable,
	// 	scard.StateEmpty,
	// 	scard.StatePresent,
	// 	scard.StateAtrmatch,
	// 	scard.StateExclusive,
	// 	scard.StateInuse,
	// 	scard.StateMute,
	// 	scard.StateUnpowered,
	// )
	for {
		err := ctx.GetStatusChange(rs, -1)
		if err != nil {
			return -1, err
		}
		// fmt.Println("StatusChanged")
		for i := range rs {
			// fmt.Println(
			// 	rs[i].EventState&scard.StateUnaware,
			// 	rs[i].EventState&scard.StateIgnore,
			// 	rs[i].EventState&scard.StateChanged,
			// 	rs[i].EventState&scard.StateUnknown,
			// 	rs[i].EventState&scard.StateUnavailable,
			// 	rs[i].EventState&scard.StateEmpty,
			// 	rs[i].EventState&scard.StatePresent,
			// 	rs[i].EventState&scard.StateAtrmatch,
			// 	rs[i].EventState&scard.StateExclusive,
			// 	rs[i].EventState&scard.StateInuse,
			// 	rs[i].EventState&scard.StateMute,
			// 	rs[i].EventState&scard.StateUnpowered,
			// )
			// fmt.Println(
			// 	rs[i].Reader,
			// 	rs[i].CurrentState,
			// 	rs[i].EventState,
			// 	scard.StatePresent,
			// 	rs[i].EventState&scard.StatePresent)

			rs[i].CurrentState = rs[i].EventState
			if rs[i].EventState&scard.StateUnpowered != 0 {
				log.Println("Card removed")
				continue
			}
			if rs[i].EventState&scard.StatePresent != 0 {
				log.Println("Card inserted")
				return i, nil
			}
		}

	}
}

// func Connect(options *Options) {
// 	if options == nil {
// 		options = &Options{
// 			ShowFaceImage: false,
// 			ShowNhsoData:  false,
// 		}
// 	}
// 	// Establish a context
// 	ctx, err := newContext()
// 	if err != nil {
// 		fmt.Println(err)
// 		return
// 	}
// 	defer ctx.Release()

// 	// List available readers
// 	readers, err := ctx.ListReaders()
// 	if err != nil {
// 		fmt.Println(err)
// 		return
// 	}

// 	fmt.Printf("Found %d readers:\n", len(readers))
// 	for i, reader := range readers {
// 		fmt.Printf("[%d] %s\n", i, reader)
// 	}

// 	if len(readers) > 0 {
// 		rs := make([]scard.ReaderState, len(readers))
// 		for i := range rs {
// 			rs[i].Reader = readers[i]
// 			rs[i].CurrentState = scard.StateUnaware
// 		}
// 		for {
// 			fmt.Println("Waiting for a Card Inserted")
// 			index, err := waitUntilCardPresent(ctx, rs)
// 			if err != nil {
// 				fmt.Println("card present error:", err)
// 				continue
// 			}

// 			// Connect to card
// 			fmt.Println("Connecting to card in ", readers[index])
// 			card, err := ctx.Connect(readers[index], scard.ShareExclusive, scard.ProtocolAny)
// 			if err != nil {
// 				fmt.Println(err)
// 				card.Disconnect(scard.UnpowerCard)
// 				continue
// 			}
// 			// defer func() {
// 			// 	card.Disconnect(scard.ResetCard)
// 			// 	index, err = waitUntilCardPresent(ctx, readers)
// 			// 	if err != nil {
// 			// 		handleError(err)
// 			// 	}
// 			// }()

// 			fmt.Println("Card status:")
// 			status, err := card.Status()
// 			if err != nil {
// 				fmt.Println(err)
// 				card.Disconnect(scard.UnpowerCard)
// 				continue
// 			}

// 			fmt.Printf("\treader: %s\n\tstate: %x\n\tactive protocol: %x\n\tatr: % x\n",
// 				status.Reader, status.State, status.ActiveProtocol, status.Atr)

// 			var cmd = []byte{0x00, 0xa4, 0x00, 0x0c, 0x02, 0x3f, 0x00} // SELECT MF

// 			fmt.Println("Transmit:")
// 			fmt.Printf("\tc-apdu: % x\n", cmd)
// 			rsp, err := card.Transmit(cmd)
// 			if err != nil {
// 				fmt.Println(err)
// 				card.Disconnect(scard.UnpowerCard)
// 				continue
// 			}
// 			fmt.Printf("\tr-apdu: % x\n", rsp)
// 			card.Disconnect(scard.UnpowerCard)

// 			// err = ctx.GetStatusChange(rs, -1)
// 			// if err != nil {
// 			// 	return -1, err
// 			// }
// 			// fmt.Println("StatusChanged")
// 			time.Sleep(3 * time.Second)
// 		}

// 	}
// }

func newContext() (*scard.Context, error) {
	return scard.EstablishContext()
}

func releaseContext(ctx *scard.Context) {
	ctx.Release()
}
