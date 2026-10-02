package main

import (
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
	"testing"
)

func TestCommandAcceptedBeforeTerminalAndOnlyOnce(t *testing.T) {
	queue := make(chan smc.Control, 1)
	var got []string
	c := &optionsController{control: queue}
	cmd := model.Command{Action: "get-status", RequestID: "synthetic", Reply: &model.CommandReply{Send: func(status, code string) { got = append(got, status) }}}
	c.handle(cmd)
	ctl := <-queue
	ctl.Complete.Finish(nil)
	ctl.Complete.Finish(nil)
	if len(got) != 2 || got[0] != "accepted" || got[1] != "completed" {
		t.Fatalf("results %v", got)
	}
}
func TestReaderQueueFullReportsBusy(t *testing.T) {
	c := &optionsController{control: make(chan smc.Control)}
	var got string
	c.handle(model.Command{Action: "read-now", Reply: &model.CommandReply{Send: func(status, code string) { got = status + ":" + code }}})
	if got != "busy:reader_queue_full" {
		t.Fatal(got)
	}
}
