package smc

import (
	"fmt"
	"strings"
	"time"

	"github.com/ebfe/scard"
	"github.com/varokas/tis620"
)

type Options struct {
	ShowFaceImage bool
	ShowNhsoData  bool
}

func Connect(options *Options) {
	if options == nil {
		options = &Options{
			ShowFaceImage: false,
			ShowNhsoData:  false,
		}
	}
	// Establish a context
	ctx, err := scard.EstablishContext()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer ctx.Release()

	// List available readers
	readers, err := ctx.ListReaders()
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("Found %d readers:\n", len(readers))
	for i, reader := range readers {
		fmt.Printf("[%d] %s\n", i, reader)
	}

	if len(readers) > 0 {
		rs := make([]scard.ReaderState, len(readers))
		for i := range rs {
			rs[i].Reader = readers[i]
			rs[i].CurrentState = scard.StateUnaware
		}
		for {
			fmt.Println("Waiting for a Card Inserted")
			index, err := waitUntilCardPresent(ctx, rs)
			if err != nil {
				fmt.Println(err)
				continue
			}

			// Connect to card
			fmt.Println("Connecting to card in ", readers[index])
			card, err := ctx.Connect(readers[index], scard.ShareExclusive, scard.ProtocolAny)
			if err != nil {
				fmt.Println(err)
				card.Disconnect(scard.UnpowerCard)
				continue
			}
			// defer func() {
			// 	card.Disconnect(scard.ResetCard)
			// 	index, err = waitUntilCardPresent(ctx, readers)
			// 	if err != nil {
			// 		handleError(err)
			// 	}
			// }()

			fmt.Println("Card status:")
			status, err := card.Status()
			if err != nil {
				fmt.Println(err)
				card.Disconnect(scard.UnpowerCard)
				continue
			}

			fmt.Printf("\treader: %s\n\tstate: %x\n\tactive protocol: %x\n\tatr: % x\n",
				status.Reader, status.State, status.ActiveProtocol, status.Atr)

			var cmd = []byte{0x00, 0xa4, 0x00, 0x0c, 0x02, 0x3f, 0x00} // SELECT MF

			fmt.Println("Transmit:")
			fmt.Printf("\tc-apdu: % x\n", cmd)
			rsp, err := card.Transmit(cmd)
			if err != nil {
				fmt.Println(err)
				card.Disconnect(scard.UnpowerCard)
				continue
			}
			fmt.Printf("\tr-apdu: % x\n", rsp)
			card.Disconnect(scard.UnpowerCard)

			// err = ctx.GetStatusChange(rs, -1)
			// if err != nil {
			// 	return -1, err
			// }
			// fmt.Println("StatusChanged")
			time.Sleep(3 * time.Second)
		}

	}
}

func readCard() {

}

func getResponseCommand(atr []byte) []byte {
	if atr[0] == 0x3B && atr[1] == 0x67 {
		return []byte{0x00, 0xc0, 0x00, 0x01}
	}
	return []byte{0x00, 0xc0, 0x00, 0x00}
}

func waitUntilCardPresent(ctx *scard.Context, rs []scard.ReaderState) (int, error) {

	fmt.Println(
		scard.StateUnaware,
		scard.StateIgnore,
		scard.StateChanged,
		scard.StateUnknown,
		scard.StateUnavailable,
		scard.StateEmpty,
		scard.StatePresent,
		scard.StateAtrmatch,
		scard.StateExclusive,
		scard.StateInuse,
		scard.StateMute,
		scard.StateUnpowered,
	)
	for {
		err := ctx.GetStatusChange(rs, -1)
		if err != nil {
			return -1, err
		}
		fmt.Println("StatusChanged")
		for i := range rs {
			fmt.Println(
				rs[i].EventState&scard.StateUnaware,
				rs[i].EventState&scard.StateIgnore,
				rs[i].EventState&scard.StateChanged,
				rs[i].EventState&scard.StateUnknown,
				rs[i].EventState&scard.StateUnavailable,
				rs[i].EventState&scard.StateEmpty,
				rs[i].EventState&scard.StatePresent,
				rs[i].EventState&scard.StateAtrmatch,
				rs[i].EventState&scard.StateExclusive,
				rs[i].EventState&scard.StateInuse,
				rs[i].EventState&scard.StateMute,
				rs[i].EventState&scard.StateUnpowered,
			)
			fmt.Println(
				rs[i].Reader,
				rs[i].CurrentState,
				rs[i].EventState,
				scard.StatePresent,
				rs[i].EventState&scard.StatePresent)

			rs[i].CurrentState = rs[i].EventState
			if rs[i].EventState&scard.StateUnpowered != 0 {
				continue
			}
			if rs[i].EventState&scard.StatePresent != 0 {
				return i, nil
			}
		}

	}
}

func readData(card *scard.Card, cmd []byte, cmdGetResponse []byte) (string, error) {
	return readDataToString(card, cmd, cmdGetResponse, false)
}

func readDataThai(card *scard.Card, cmd []byte, cmdGetResponse []byte) (string, error) {
	return readDataToString(card, cmd, cmdGetResponse, true)
}

func readDataToString(card *scard.Card, cmd []byte, cmdGetResponse []byte, isTIS620 bool) (string, error) {
	// Send command APDU
	_, err := card.Transmit(cmd)
	if err != nil {
		fmt.Println("Error Transmit:", err)
		return "", err
	}
	// fmt.Println(rsp)

	// get respond command
	cmd_respond := append(cmdGetResponse[:], cmd[len(cmd)-1])
	rsp, err := card.Transmit(cmd_respond)
	if err != nil {
		fmt.Println("Error Transmit:", err)
		return "", err
	}
	// fmt.Println(rsp)

	if isTIS620 {
		rsp = tis620.ToUTF8(rsp)
	}

	// for i := 0; i < len(rsp)-2; i++ {
	// 	cid += fmt.Sprintf("%c", rsp[i])
	// }
	return strings.TrimSpace(string(rsp[:len(rsp)-2])), nil
}
