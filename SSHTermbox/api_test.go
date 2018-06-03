package sshtermbox

import (
	"context"
	"io"
	"testing"
	"time"
)

func consume(t *testing.T, reader *io.PipeReader) {
	buffer := make([]byte, 4096)
	var err error
	for err == nil {
		reader.Read(buffer)
	}
	if err != io.EOF {
		t.Errorf("got error while consumeing: %v", err)
	}
}

func TestInit(t *testing.T) {

	inTerm, _ := io.Pipe()
	out, outTerm := io.Pipe()

	go consume(t, out)

	_, err := Init(inTerm, outTerm, "xterm", 0, 0)
	if err != nil {
		t.Errorf("error initializing a sshterm: %v", err)
	}
}

func TestUnhandledTerminalType(t *testing.T) {
	_, err := Init(nil, nil, "", 0, 0)
	if err != errorUnknownTerm {
		t.Errorf("expected error %v, got %v", errorUnknownTerm, err)
	}
}

func TestCancelingEventPoll(t *testing.T) {
	inTerm, _ := io.Pipe()
	out, outTerm := io.Pipe()

	go consume(t, out)

	term, err := Init(inTerm, outTerm, "xterm", 0, 0)
	if err != nil {
		t.Errorf("error initializing a sshterm: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	term.PollEventWithContext(ctx)
}

func TestDeadlineEventPolling(t *testing.T) {
	inTerm, _ := io.Pipe()
	out, outTerm := io.Pipe()

	go consume(t, out)

	term, err := Init(inTerm, outTerm, "xterm", 0, 0)
	if err != nil {
		t.Errorf("error initializing a sshterm: %v", err)
		return
	}

	ctx, _ := context.WithTimeout(context.Background(), 0*time.Second)
	term.PollEventWithContext(ctx)
}
