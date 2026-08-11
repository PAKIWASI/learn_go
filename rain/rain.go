// Package rain
package rain

import (
	"bufio"
	"fmt"
	"log"
	"math/rand/v2"
	"os"

	"golang.org/x/term"
)

type charset uint8

const (
	charsetLatin charset = iota
	charsetDigits
	charsetBinary
)

const (
	latin  = "abcdefghijklmnopqrstuvwxyz1234567890"
	digits = "1234567890"
	binary = "01"
)

type Column struct {
	dropIdx  uint16 // which drop we are displaying this frame. Index into Rain.drops
	startIdx uint16 // y-index for where the stream starts for this column
	endIdx   uint16 // y-index where this stream ends (can be past the height so trail can finish)
	// the difference b/w the startIdx and endIdx shouldn't be much bigger than the terminal height
	len       uint16 // length of the rain stream, number of chars drawn for this column
	fadeStart uint16 // y-index where fade starts
	numFaded  uint16 // how many chars are to be faded
	cooldown  uint16 // how long to wait after finishing a stream to be considered for next stream
	isActive  bool   // is this column currently "running"
}

type Rain struct {
	// terminal dimentions
	width  uint16
	height uint16

	charset []rune // pool of all available utf codepoints
	columns []Column

	buffer *bufio.Writer
	// saved terminal state to restore later
	oldState *term.State

	isPaused bool
	done     chan struct{}
}

// get width, height of user's actual terminal
func (r *Rain) setTermDims() {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		log.Fatal(err)
	}
	r.width = uint16(width)
	r.height = uint16(height)
}

// enter raw mode, hide cursor etc
func (r *Rain) setupTerm() {
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Fatal(err)
	}
	r.oldState = oldState
	fmt.Fprint(r.buffer, "\x1b[?25l") // hide cursor
	// fmt.Fprint(r.buffer, bgBase)      // set background. persists until cleanupTerm's reset
	fmt.Fprint(r.buffer, "\x1b[2J") // clear screen (fills with the bg color just set)
	fmt.Fprintf(r.buffer, "\x1b[1;1H")
	r.buffer.Flush()
}

// exit raw mode, show curser etc
func (r *Rain) cleanupTerm() {
	term.Restore(int(os.Stdin.Fd()), r.oldState)
	// fmt.Fprint(r.buffer, cReset)      // drop the Dracula bg/fg back to terminal defaults
	fmt.Fprint(r.buffer, "\x1b[?25h") // show
	r.buffer.Flush()
}

func (r *Rain) setCharset(set charset) {
	var selected string
	switch set {
	case charsetDigits:
		selected = digits
	case charsetBinary:
		selected = binary
	default:
		fmt.Println("Warning: unknown charset provided. Using default: Latin")
		fallthrough
	case charsetLatin:
		selected = latin
	}

	r.charset = make([]rune, len(selected))
	for i := range selected {
		r.charset[i] = rune(selected[i])
	}
}

func (r *Rain) init(set charset) {
	r.setTermDims()
	r.setCharset(set)
	r.buffer = bufio.NewWriter(os.Stdout)
	r.setupTerm()
}

func RunRain() {
	var rain Rain
	rain.init(charsetLatin)
	defer rain.cleanupTerm()
}
