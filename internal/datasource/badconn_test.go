package datasource

import (
	"database/sql/driver"
	"errors"
	"io"
	"testing"
)

func TestIsBadConnErr(t *testing.T) {
	bad := []error{
		driver.ErrBadConn,
		io.EOF,
		errors.New("invalid connection"),
		errors.New("packets.go:58 unexpected EOF"),
		errors.New("write: broken pipe"),
		errors.New("read: connection reset by peer"),
		errors.New("use of closed network connection"),
	}
	for _, e := range bad {
		if !isBadConnErr(e) {
			t.Errorf("expected bad-conn for %q", e)
		}
	}
	good := []error{
		nil,
		errors.New("Error 1054: Unknown column 'x'"),
		errors.New("syntax error near 'FROM'"),
	}
	for _, e := range good {
		if isBadConnErr(e) {
			t.Errorf("expected NOT bad-conn for %v", e)
		}
	}
}
