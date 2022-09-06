package util

import (
	"errors"
	"log"
	"strings"

	"github.com/ebfe/scard"
	"github.com/varokas/tis620"
)

func EstablishContext() (*scard.Context, error) {
	return scard.EstablishContext()
}

func ReleaseContext(ctx *scard.Context) {
	ctx.Release()
}

func ListReaders(ctx *scard.Context) ([]string, error) {
	return ctx.ListReaders()
}

func InitReaderStates(readers []string) []scard.ReaderState {
	rs := make([]scard.ReaderState, len(readers))
	for i := range rs {
		rs[i].Reader = readers[i]
		rs[i].CurrentState = scard.StateUnaware
	}
	return rs
}

func WaitUntilCardPresent(ctx *scard.Context, rs []scard.ReaderState) (int, error) {
	// log.Println(
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
		// log.Println("StatusChanged")
		for i := range rs {
			// log.Println(
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
			// log.Println(
			// 	rs[i].Reader,
			// 	rs[i].CurrentState,
			// 	rs[i].EventState,
			// 	scard.StatePresent,
			// 	rs[i].EventState&scard.StatePresent)

			rs[i].CurrentState = rs[i].EventState
			// if rs[i].EventState&scard.StateUnpowered != 0 {
			// 	log.Println("Card removed")
			// 	continue
			// }
			if rs[i].EventState&scard.StatePresent != 0 {
				log.Println("Card inserted")
				return i, nil
			}
		}
	}
}

func WaitUntilCardRemove(ctx *scard.Context, rs []scard.ReaderState) (int, error) {
	// log.Println(
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
		// log.Println("StatusChanged")
		for i := range rs {
			// log.Println(
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
			// log.Println(
			// 	rs[i].Reader,
			// 	rs[i].CurrentState,
			// 	rs[i].EventState,
			// 	scard.StateEmpty,
			// 	rs[i].EventState&scard.StateEmpty)

			rs[i].CurrentState = rs[i].EventState
			if rs[i].EventState&scard.StateEmpty != 0 {
				log.Println("Card removed")
				return i, nil
			}
			// if rs[i].EventState&scard.StatePresent != 0 {
			// 	log.Println("Card inserted")
			// 	continue
			// }
		}
	}
}

func ConnectCard(ctx *scard.Context, reader string) (*scard.Card, error) {
	return ctx.Connect(reader, scard.ShareExclusive, scard.ProtocolAny)
}

func DisconnectCard(card *scard.Card) error {
	if card == nil {
		return errors.New("card is nil")
	}
	return card.Disconnect(scard.UnpowerCard)
}

func GetResponseCommand(atr []byte) []byte {
	if atr[0] == 0x3B && atr[1] == 0x67 {
		return []byte{0x00, 0xc0, 0x00, 0x01}
	}
	return []byte{0x00, 0xc0, 0x00, 0x00}
}

func ReadData(card *scard.Card, cmd []byte, cmdGetResponse []byte) (string, error) {
	return readDataToString(card, cmd, cmdGetResponse, false)
}

func ReadDataThai(card *scard.Card, cmd []byte, cmdGetResponse []byte) (string, error) {
	return readDataToString(card, cmd, cmdGetResponse, true)
}

func readDataToString(card *scard.Card, cmd []byte, cmdGetResponse []byte, isTIS620 bool) (string, error) {
	_, err := card.Status()
	if err != nil {
		return "", err
	}
	// Send command APDU
	_, err = card.Transmit(cmd)
	if err != nil {
		// log.Println("Error Transmit:", err)
		return "", err
	}
	// log.Println(rsp)

	// get respond command
	cmd_respond := append(cmdGetResponse[:], cmd[len(cmd)-1])
	rsp, err := card.Transmit(cmd_respond)
	if err != nil {
		// log.Println("Error Transmit:", err)
		return "", err
	}
	// log.Println(rsp)

	if isTIS620 {
		rsp = tis620.ToUTF8(rsp)
	}

	// for i := 0; i < len(rsp)-2; i++ {
	// 	cid += fmt.Sprintf("%c", rsp[i])
	// }
	return strings.TrimSpace(string(rsp[:len(rsp)-2])), nil
}

func ReadLaserData(card *scard.Card, cmd []byte, cmdGetResponse []byte) (string, error) {
	_, err := card.Status()
	if err != nil {
		return "", err
	}
	// Send command APDU
	_, err = card.Transmit(cmd)
	if err != nil {
		return "", err
	}

	// get respond command
	cmd_respond := append(cmdGetResponse[:], 12)
	rsp, err := card.Transmit(cmd_respond)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(rsp[:len(rsp)-2])), nil
}
