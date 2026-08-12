// Package rain
package rain

import (
	"bufio"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"
)

type charset uint8

const (
	charsetLatin charset = iota
	charsetDigits
	charsetBinary
)

const (
	binary = "01"
	digits = "1234567890"
	latin  = "abcdefghijklmnopqrstuvwxyz1234567890"
)

type Column struct {
	charIdx  uint16 // which drop we are displaying this frame. Index into Rain.drops
	startIdx uint16 // y-index for where the stream starts for this column
	endIdx   uint16 // y-index where this stream ends (can be past the height so trail can finish)
	// the difference b/w the startIdx and endIdx shouldn't be much bigger than the terminal height
	len      uint16 // length of the rain stream, number of chars drawn for this column
	numFaded uint16 // how many chars are to be faded (starting at startIdx)
	cooldown uint16 // how long to wait after finishing a stream to be considered for next stream
	isActive bool   // is this column currently "running"
}

type Cursor struct {
	x, y uint16
}
type Event struct {
	move Cursor
	char rune
}

type Rain struct {
	// terminal dimentions
	width  uint16
	height uint16

	charset []rune // pool of all available utf codepoints
	columns []Column

	buffer *bufio.Writer
	writer chan Event

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
	r.charset = []rune(selected)
}

func (r *Rain) initColumn() {
	for {
		colIdx := rand.N(r.width)
		col := &r.columns[colIdx] // BUG: local copy
		if col.isActive {
			continue
		}

		col.charIdx = uint16(rand.N(len(r.charset)))
		col.endIdx = rand.N(r.height)     // TODO: don't allow small values
		col.startIdx = rand.N(col.endIdx) // dont allow values very close to endIdx
		col.len = 0
		col.numFaded = rand.N(col.endIdx - col.startIdx)
		col.cooldown = 0
		col.isActive = true
		break
	}
}

func (r *Rain) init(set charset) {
	r.setTermDims()
	r.setCharset(set)
	r.columns = make([]Column, r.width)
	r.buffer = bufio.NewWriter(os.Stdout)
	r.setupTerm() // term cleanup defered in main

	// init some lines
	for range 5 {
		r.initColumn()
	}
}

func (r *Rain) handleInput(k byte) {
	switch k {
	case 'q':
		r.done <- struct{}{}
	case 'p':
		r.isPaused = true
	}
}

func (r *Rain) update() {

}

// TODO: maybe do an initial full draw like normal?
// we only write changes to stdout, not the full buffer. this runs as a seperate goroutine
func (r *Rain) draw() {
	for i, c := range r.columns {
		if c.isActive {
			r.writer <- Event{move: Cursor{x: uint16(i), y: c.startIdx + c.len}, char: r.charset[c.charIdx]}
		}
	}
}

func RunRain() {
	r := Rain{}
	r.init(charsetLatin)
	defer r.cleanupTerm()

	// Catch SIGTERM (kill command)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		r.done <- struct{}{}
	}()

	// reading keyboard input as a seperate goroutine
	keyCh := make(chan byte)
	buf := make([]byte, 1)
	go func() {
		for {
			// TODO: how to stop this goroutine when we are done
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				continue
			}
			keyCh <- buf[0]
		}
	}()

	// FPS stuff
	const fps = 30
	frameDuration := time.Second / time.Duration(fps)
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	// MAIN LOOP
	for {
		select {
		case <-r.done:
			close(r.done)
			return
		case k := <-keyCh:
			r.handleInput(k)
		case <-ticker.C:
			if !r.isPaused {
				r.update()
				r.draw()
			}
		}
	}
}
