package lg

import (
	"testing"
)

func TestLg(t *testing.T) {
	InitLogger(true)
	L(nil).Info("test message.")
}
