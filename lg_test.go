package lg

import (
	"testing"
)

func TestLg(t *testing.T) {
	InitLogger("debug")
	L(nil).Info("test message.")
}
