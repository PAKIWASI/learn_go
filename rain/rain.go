// Package rain
package rain

import (
	"bufio"
	"log"
	"os"

	"golang.org/x/term"
)

// type Drop struct {
// 	char uint32 // utf code point
// }

type Drop uint32

type Column struct {
	dropIdx   uint16 // which drop we are displaying this frame. Index into Rain.drops
	startIdx  uint16 // y-index for where the stream starts for this column
	endIdx    uint16 // y-index where this stream ends (can be past the height so trail can finish)
	len       uint16 // length of the rain stream, number of chars drawn for this column
	fadeStart uint16 // y-index where fade starts
	numFaded  uint16 // how many chars are to be faded
	isActive  bool   // is this column currently "running"
}

type Rain struct {
	// terminal dimentions
	width  uint16
	height uint16

	drops   []Drop // pool of all available utf codepoints
	columns []Column

	buffer *bufio.Writer
	// saved terminal state to restore later
	oldState *term.State

	isPaused bool
}


// get width, height of user's actual terminal
func (r *Rain) setTermDims() {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		log.Fatal(err)
	}
	// reserve space for borders
	r.width = uint16(width)
	r.height = uint16(height)
}

func (r *Rain) init() {
	r.setTermDims()
}




