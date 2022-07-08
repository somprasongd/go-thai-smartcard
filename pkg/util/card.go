package util

import (
	"fmt"
	"strings"

	"github.com/ebfe/scard"
	"github.com/varokas/tis620"
)

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
