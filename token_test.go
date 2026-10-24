package mq

import (
	"context"
	"errors"
	"testing"
)

func TestNewCompletedToken(t *testing.T) {
	wantErr := errors.New("boom")
	for _, err := range []error{nil, wantErr} {
		tok := NewCompletedToken(err)

		select {
		case <-tok.Done():
		default:
			t.Fatal("Done() channel not closed")
		}
		if got := tok.Wait(context.Background()); !errors.Is(got, err) {
			t.Errorf("Wait() = %v, want %v", got, err)
		}
		if got := tok.Error(); !errors.Is(got, err) {
			t.Errorf("Error() = %v, want %v", got, err)
		}
		if tok.ReasonCode() != ReasonCodeSuccess || tok.Dropped() {
			t.Errorf("ReasonCode()=%v Dropped()=%v, want success/false", tok.ReasonCode(), tok.Dropped())
		}
	}
}
