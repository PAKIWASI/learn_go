package main

import (
	"bufio"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"
)


// Dracula palette as 24-bit ANSI SGR codes.
const (
	cReset   = "\x1b[0m"
	bgBase   = "\x1b[48;2;40;42;54m"    // #282a36 background
	bgLine   = "\x1b[48;2;68;71;90m"    // #44475a current-line (highlight bar)
	fgFg     = "\x1b[38;2;248;248;242m" // #f8f8f2 foreground
	fgComment = "\x1b[38;2;98;114;164m" // #6272a4 comment (dim text)
	fgPurple = "\x1b[38;2;189;147;249m" // #bd93f9
	fgPink   = "\x1b[38;2;255;121;198m" // #ff79c6
	fgGreen  = "\x1b[38;2;80;250;123m"  // #50fa7b
	fgCyan   = "\x1b[38;2;139;233;253m" // #8be9fd
)

const (
	dead byte = ' '
	alive byte = 'o'
)

type GoL struct {
	// terminal dimentions
	width  int
	height int
	// we need 2 matrices as we need to calculate
	// the next state of the grid from the previous WHOLE state
	// we can't update a cell in the same grid to next state as it will effect the state
	// of it's neighboring cell and game of life's rules will be broken
	matrix1 [][]bool
	matrix2 [][]bool
	// we do buffered writes, we write to a buffer then flush to screen in one call
	buffer *bufio.Writer
	// saved terminal state to restore later
	oldState *term.State
	
	isPaused bool
	//signal game is finished
	done     chan struct{}
	doneOnce sync.Once //guarantees g.done is closed exactly once
	// even if 'q' and Ctrl+C/SIGTERM race each other
}



// get width, height of user's actual terminal
func (g *GoL) setTermDims() {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		log.Fatal(err)
	}
	g.width = width
	g.height = height
}

// enter raw mode, hide cursor etc
func (g *GoL) setupTerm() {
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Fatal(err)
	}
	g.oldState = oldState
	fmt.Fprint(g.buffer, "\x1b[?25l") // hide cursor
	fmt.Fprint(g.buffer, bgBase)      // set background. persists until cleanupTerm's reset
	fmt.Fprint(g.buffer, "\x1b[2J")   // clear screen (fills with the bg color just set)
	fmt.Fprintf(g.buffer, "\x1b[1;1H")
	g.buffer.Flush()
}

// exit raw mode, show curser etc
func (g *GoL) cleanupTerm() {
	term.Restore(int(os.Stdin.Fd()), g.oldState)
	fmt.Fprint(g.buffer, cReset)       // drop the Dracula bg/fg back to terminal defaults
	fmt.Fprint(g.buffer, "\x1b[?25h") // show
	g.buffer.Flush()
}

// shutdown signals every goroutine watching g.done to stop.
// safe to call multiple times / from multiple goroutines concurrently.
func (g *GoL) shutdown() {
	g.doneOnce.Do(func() { close(g.done) })
}

// init the grid with some random alive cells
func (g *GoL) initRandom() {
	// switch on 30% of the cells
	toCreate := int32(float32(g.width) * float32(g.height) * 0.3)

	for range toCreate {
		for {
			i, j := rand.IntN(g.height), rand.IntN(g.width)
			if g.matrix1[i][j] {
				continue
			}
			g.matrix1[i][j] = true
			break
		}
	}
}

// Clears both matrices and reseeds a fresh random pattern
func (g *GoL) restart() {
	for i := range g.matrix1 {
		for j := range g.matrix1[i] {
			g.matrix1[i][j] = false
			g.matrix2[i][j] = false
		}
	}
	g.initRandom()
}

func (g *GoL) init() {

	g.done = make(chan struct{})

	// buffered writes for less flicker
	g.buffer = bufio.NewWriter(os.Stdout)

	// set width, height according to terminal size
	g.setTermDims()

	// have a contingious array (width*heigh) and treat it as
	// a 2D matrix, way better cache locality
	buf1 := make([]bool, g.width*g.height)
	buf2 := make([]bool, g.width*g.height)
	g.matrix1 = make([][]bool, g.height)
	g.matrix2 = make([][]bool, g.height)
	for i := range g.matrix1 {
		g.matrix1[i] = buf1[i*g.width : (i+1)*g.width]
		g.matrix2[i] = buf2[i*g.width : (i+1)*g.width]
	}

	// setup term raw mode (cleanup defered in main())
	g.setupTerm()

	// init some cells
	g.initRandom()
}

// given a cell (i, j) count how many of its neighbours are alive
func (g *GoL) countNeighbours(i, j int) int {
	count := 0
	// dy and dx get all 8 neighbours
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			// skip the cell itself
			if dy == 0 && dx == 0 {
				continue
			}
			// wraps around like a torus (edges connect to opposite edges)
			I := ((i+dy)%g.height + g.height) % g.height
			J := ((j+dx)%g.width + g.width) % g.width
			if g.matrix1[I][J] {
				count++
			}
		}
	}
	return count
}

// update loop
func (g *GoL) update() {

	for i := range g.height {
		for j := range g.width {
			alive := g.matrix1[i][j] // read from matrix1
			count := g.countNeighbours(i, j)

			// alive next gen iff exactly 3 neighbours, or currently alive with exactly 2
			g.matrix2[i][j] = count == 3 || (alive && count == 2)
		}
	}

	// swap matrices: we wrote the next state to matrix2, while in draw() we draw matrix1
	// we switch matrices here so new state is drawn. then in next frame, we will write next
	// state to the swapped matrix2
	temp := g.matrix1
	g.matrix1 = g.matrix2
	g.matrix2 = temp
}

func (g *GoL) draw() {
	fmt.Fprint(g.buffer, "\x1b[1;1H")	// bring cursor home
 
	// set green foreground ONCE for the whole frame
	fmt.Fprint(g.buffer, fgGreen)
 
	for i := range g.height {
		for j := range g.width {
			if !g.matrix1[i][j] {
				g.buffer.WriteByte(dead)
			} else {
				g.buffer.WriteByte(alive)
			}
		}
	}
 
	g.buffer.Flush()	// write to stdiot
}

// claude be like:
// ascii block font — 5 rows tall, covers just the glyphs "GAME OF LIFE" needs
var asciiFont = map[rune][5]string{
	'G': {" ### ", "#    ", "#  ##", "#   #", " ### "},
	'A': {" ### ", "#   #", "#####", "#   #", "#   #"},
	'M': {"#   #", "## ##", "# # #", "#   #", "#   #"},
	'E': {"#####", "#    ", "#### ", "#    ", "#####"},
	'O': {" ### ", "#   #", "#   #", "#   #", " ### "},
	'F': {"#####", "#    ", "#### ", "#    ", "#    "},
	'L': {"#    ", "#    ", "#    ", "#    ", "#####"},
	'I': {"###", " # ", " # ", " # ", "###"},
	' ': {"   ", "   ", "   ", "   ", "   "},
}

// bigText renders s as 5 lines of ascii-art, glyphs joined left to right
func bigText(s string) []string {
	lines := make([]string, 5)
	for _, r := range s {
		glyph, ok := asciiFont[r]
		if !ok {
			continue
		}
		for row := range 5 {
			lines[row] += glyph[row] + " "
		}
	}
	return lines
}

// writeCentered writes s centered horizontally at terminal row (1-indexed)
func (g *GoL) writeCentered(row int, color, s string) {
	col := max((g.width-len([]rune(s)))/2, 0)
	fmt.Fprintf(g.buffer, "\x1b[%d;%dH%s%s", row, col+1, color, s)
}
 
func (g *GoL) drawWelcome() {
	fmt.Fprint(g.buffer, "\x1b[2J") // clear screen (Dracula bg from setupTerm still active)
 
	title := bigText("GAME OF LIFE")
	startRow := g.height/2 - len(title)/2 - 3 // room for controls below
 
	for i, line := range title {
		g.writeCentered(startRow+i, fgPurple, line)
	}
	g.writeCentered(startRow+len(title)+2, fgComment, "p pause    r restart    q quit")
	g.writeCentered(startRow+len(title)+4, fgGreen, "press any key to start")
 
	g.buffer.Flush()
}
 
// drawPaused overlays a message over the current grid, without clearing it,
// so the last generation stays visible underneath. Uses the current-line
// highlight color as a background bar so the message reads clearly against
// whatever cells happen to be behind it
func (g *GoL) drawPaused() {
	msg := " PAUSED — p resume   r restart   q quit "
	col := max((g.width-len([]rune(msg)))/2, 0)
	row := g.height / 2
	fmt.Fprintf(g.buffer, "\x1b[%d;%dH%s%s%s%s", row, col+1, bgLine, fgPink, msg, bgBase)
	g.buffer.Flush()
}

func (g *GoL) handleInput(k byte) {
	switch k {
	case 'q', 3: //3 = Ctrl+C is disabled in raw mode. it arrives as
		// this raw byte over stdin instead, and must be handled like any other key.
		g.shutdown()
	case 'p':
		g.isPaused = !g.isPaused
		if g.isPaused {
			g.drawPaused()
		}
	case 'r':
		g.restart()
	}
}

func main() {
	g := GoL{}
	g.init()
	defer g.cleanupTerm()


	// Catch SIGTERM (kill command)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		g.shutdown()
	}()


	// reading keyboard input
	keyCh := make(chan byte)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				continue
			}
			keyCh <- buf[0]
		}
	}()


	// welcome screen: draw ONCE, then block until any key or a shutdown signal
	// this must happen before the ticker starts, so frame timing begins fresh
	// once the game actually starts, not from whenever the program launched
	g.drawWelcome()
	select {
	case k := <-keyCh:
		g.handleInput(k) // if this was 'q'/Ctrl+C, g.done is now closed
		// the main loop below will catch it on its very first iteration
	case <-g.done:
		return
	}


	// FPS stuff
	const fps = 10
	frameDuration := time.Second / time.Duration(fps)
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()


	// MAIN LOOP

	for {
		// select waits on whichever channel is ready first. a keypress or the next tick
		// so input feels responsive while the frame still advances on schedule even if the user isn't typing.
		select {
		// if we have a keyboard input we handle it first, then go to main update/draw stuff
		case k := <-keyCh:
			g.handleInput(k)
		// channel receives a value every time sleep duration elapses. ranging over it blocks until the next tick
		// so we naturally get proper frame pacing
		case <-ticker.C:
			if !g.isPaused {
				g.update()
				g.draw()
			}
		case <-g.done: // game finished
			return
		}
	}

}
