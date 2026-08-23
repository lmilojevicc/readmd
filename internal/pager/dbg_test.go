package pager

import (
	"fmt"
	"testing"
)

func TestZZDbg(t *testing.T) {
	src := "$$\n\\alpha\n\\beta\n$$\nx\n"
	fmt.Printf("sub=%q\n", substituteMath(src))
}
