package gameoflife

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
	cReset    = "\x1b[0m"
	bgBase    = "\x1b[48;2;40;42;54m"    // #282a36 background
	bgLine    = "\x1b[48;2;68;71;90m"    // #44475a current-line (highlight bar)
	fgFg      = "\x1b[38;2;248;248;242m" // #f8f8f2 foreground
	fgComment = "\x1b[38;2;98;114;164m"  // #6272a4 comment (dim text)
	fgPurple  = "\x1b[38;2;189;147;249m" // #bd93f9
	fgPink    = "\x1b[38;2;255;121;198m" // #ff79c6
	fgGreen   = "\x1b[38;2;80;250;123m"  // #50fa7b
	fgCyan    = "\x1b[38;2;139;233;253m" // #8be9fd
)

const (
	borderTL = '╭'
	borderTR = '╮'
	borderBL = '╰'
	borderBR = '╯'
	borderH  = '─'
	borderV  = '│'
)

const (
	dead  byte = ' '
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

	// editor cursor position, used only while drawing the starting pattern
	cursorRow int
	cursorCol int

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
	// reserve space for borders
	g.width = width - 2
	g.height = height - 2
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
	fmt.Fprint(g.buffer, cReset)      // drop the Dracula bg/fg back to terminal defaults
	fmt.Fprint(g.buffer, "\x1b[?25h") // show
	g.buffer.Flush()
}

// shutdown signals every goroutine watching g.done to stop.
// safe to call multiple times / from multiple goroutines concurrently.
func (g *GoL) shutdown() {
	g.doneOnce.Do(func() { close(g.done) })
}

// sets every cell dead
func (g *GoL) clearGrid() {
	for i := range g.matrix1 {
		for j := range g.matrix1[i] {
			g.matrix1[i][j] = false
		}
	}
}

// init the grid with some random cells alive
func (g *GoL) initRandom() {
	g.clearGrid()

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
	g.drawBorder()
	g.buffer.Flush()
}

// flips a single cell
func (g *GoL) toggleCell(i, j int) {
	g.matrix1[i][j] = !g.matrix1[i][j]
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
	// set green foreground ONCE for the whole frame
	fmt.Fprint(g.buffer, fgGreen)

	for i := range g.height {
		// reposition to the start of this interior row every time
		// the grid is inset by the border, so we can't just write
		// one long stream of bytes from 1;1H or we'd wrap onto the border
		fmt.Fprintf(g.buffer, "\x1b[%d;2H", i+2)
		for j := range g.width {
			if !g.matrix1[i][j] {
				g.buffer.WriteByte(dead)
			} else {
				g.buffer.WriteByte(alive)
			}
		}
	}

	g.buffer.Flush() // write to stdiot
}

// draws a rounded border around the full terminal once.
// this must be re-called after any full-screen clear (\x1b[2J),
func (g *GoL) drawBorder() {
	fmt.Fprint(g.buffer, fgPurple)

	// top edge
	fmt.Fprintf(g.buffer, "\x1b[1;1H%c", borderTL)
	for range g.width {
		g.buffer.WriteRune(borderH)
	}
	g.buffer.WriteRune(borderTR)

	// side edges
	for i := 0; i < g.height; i++ {
		row := i + 2 // +1 for top border, +1 for 1-indexing
		fmt.Fprintf(g.buffer, "\x1b[%d;1H%c", row, borderV)
		fmt.Fprintf(g.buffer, "\x1b[%d;%dH%c", row, g.width+2, borderV)
	}

	// bottom edge
	fmt.Fprintf(g.buffer, "\x1b[%d;1H%c", g.height+2, borderBL)
	for range g.width {
		g.buffer.WriteRune(borderH)
	}
	g.buffer.WriteRune(borderBR)

	fmt.Fprint(g.buffer, fgGreen) // restore grid's default fg for whatever follows
}

// claude be like:
// ascii block font — 5 rows tall, covers just the glyphs "GAME OF LIFE" needs
var asciiFont = map[rune][5]string{
	'G': {" ⣿⣿⣿ ", "⣿    ", "⣿  ⣿⣿", "⣿   ⣿", " ⣿⣿⣿ "},
	'A': {" ⣿⣿⣿ ", "⣿   ⣿", "⣿⣿⣿⣿⣿", "⣿   ⣿", "⣿   ⣿"},
	'M': {"⣿   ⣿", "⣿⣿ ⣿⣿", "⣿ ⣿ ⣿", "⣿   ⣿", "⣿   ⣿"},
	'E': {"⣿⣿⣿⣿⣿", "⣿    ", "⣿⣿⣿⣿ ", "⣿    ", "⣿⣿⣿⣿⣿"},
	'O': {" ⣿⣿⣿ ", "⣿   ⣿", "⣿   ⣿", "⣿   ⣿", " ⣿⣿⣿ "},
	'F': {"⣿⣿⣿⣿⣿", "⣿    ", "⣿⣿⣿⣿ ", "⣿    ", "⣿    "},
	'L': {"⣿    ", "⣿    ", "⣿    ", "⣿    ", "⣿⣿⣿⣿⣿"},
	'I': {"⣿⣿⣿", " ⣿ ", " ⣿ ", " ⣿ ", "⣿⣿⣿"},
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

// writes s centered horizontally at terminal row (1-indexed)
func (g *GoL) writeCentered(row int, color, s string) {
	col := max((g.width-len([]rune(s)))/2, 0)
	fmt.Fprintf(g.buffer, "\x1b[%d;%dH%s%s", row, col+1, color, s)
}

// shows the title and the three welcome-screen actions
// d = draw your own pattern, r = start with a random pattern, q = quit
func (g *GoL) drawWelcome() {
	fmt.Fprint(g.buffer, "\x1b[2J") // clear screen (Dracula bg from setupTerm still active)
	g.drawBorder()

	title := bigText("GAME OF LIFE")
	startRow := g.height/2 - len(title)/2 - 3 // room for controls below

	for i, line := range title {
		g.writeCentered(startRow+i, fgPurple, line)
	}
	g.writeCentered(startRow+len(title)+2, fgComment, "d draw    r random    q quit")

	g.buffer.Flush()
}

// overlays a message over the current grid, without clearing it
func (g *GoL) drawPaused() {
	msg := " PAUSED — p resume   c clear   d draw   r random   q quit "
	col := max((g.width-len([]rune(msg)))/2, 0)
	row := g.height / 2
	// theme
	fmt.Fprintf(g.buffer, "\x1b[%d;%dH%s%s%s%s", row, col+1, bgLine, fgPink, msg, bgBase)
	g.buffer.Flush()
}

// copies a small ASCII pattern into the buffer
func (g *GoL) stampPattern(pattern []string, row, col int) {
	for dy, line := range pattern {
		for dx, ch := range line {
			i := ((row+dy)%g.height + g.height) % g.height
			j := ((col+dx)%g.width + g.width) % g.width
			if ch == 'o' {
				g.matrix1[i][j] = true
			}
		}
	}
}

// TODO: rotate?
var patternGlider = []string{
	".o.",
	"..o",
	"ooo",
}

// renders the drawing buffer with the cursor cell highlighted
// hint is the bottom-row instruction text lets callers say "enter start"
// on first entry or "enter resume" when re-opened from the pause menu
func (g *GoL) drawEditor(hint string) {
	fmt.Fprint(g.buffer, fgGreen)

	for i := range g.height {
		fmt.Fprintf(g.buffer, "\x1b[%d;2H", i+2)
		for j := range g.width {
			atCursor := i == g.cursorRow && j == g.cursorCol
			if atCursor {
				fmt.Fprint(g.buffer, bgLine)
			}
			if g.matrix1[i][j] {
				g.buffer.WriteByte(alive)
			} else {
				g.buffer.WriteByte(dead)
			}
			if atCursor {
				fmt.Fprint(g.buffer, bgBase) // restore normal background immediately after
			}
		}
	}

	g.writeCentered(g.height+1, fgComment, hint)
	g.buffer.Flush()
}

// draw a starting pattern with hjkl + space, until they press enter (start/resume) or quit
// hint is passed straight through to drawEditor now
func (g *GoL) runEditor(keyCh <-chan byte, hint string) {
	g.cursorRow, g.cursorCol = g.height/2, g.width/2
	g.drawEditor(hint)

	for {
		select {
		case k := <-keyCh:
			switch k {
			case 'h':
				if g.cursorCol > 0 {
					g.cursorCol--
				}
			case 'l':
				if g.cursorCol < g.width-1 {
					g.cursorCol++
				}
			case 'k':
				if g.cursorRow > 0 {
					g.cursorRow--
				}
			case 'j':
				if g.cursorRow < g.height-1 {
					g.cursorRow++
				}
			case ' ':
				g.toggleCell(g.cursorRow, g.cursorCol)
			case 'g':
				g.stampPattern(patternGlider, g.cursorRow, g.cursorCol)
			case 'r':
				g.initRandom()
			case '\r', '\n':
				return // enter: done drawing, go start/resume the simulation
			case 'q', 3:
				g.shutdown()
				return
			}
			g.drawEditor(hint)
		case <-g.done:
			return
		}
	}
}

// handles keys during actual gameplay (both running and
// paused). keyCh is only needed for the 'd' (draw again) pause action
// which re-enters the blocking editor loop
func (g *GoL) handleInput(k byte, keyCh <-chan byte) {
	switch k {
	case 'q', 3: //3 = Ctrl+C is disabled in raw mode. it arrives as
		// this raw byte over stdin instead, and must be handled like any other key
		g.shutdown()
	case 'p':
		g.isPaused = !g.isPaused
		if g.isPaused {
			g.drawPaused()
		}
	case 'r':
		g.initRandom()
		if g.isPaused {
			// ticker doesnot call draw() while paused, so repaint by hand
			g.draw()
			g.drawPaused()
		}
	case 'c':
		if g.isPaused { // clear only makes sense as a pause-menu action
			g.clearGrid()
			g.draw()
			g.drawPaused()
		}
	case 'd':
		if g.isPaused { // draw-again only makes sense as a pause-menu action
			g.runEditor(keyCh, "hjkl move  space toggle  g glider  r random  enter resume")
			g.draw()
			g.drawPaused()
		}
	}
}

func (g *GoL) welcomeLoop(keyCh <-chan byte) {

welcomeLoop:
	for {
		select {
		case k := <-keyCh:
			switch k {
			case 'd':
				g.runEditor(keyCh, "hjkl move  space toggle  g glider  r random  enter start")
				break welcomeLoop
			case 'r':
				g.initRandom()
				break welcomeLoop
			case 'q', 3:
				g.shutdown()
				break welcomeLoop
			}
			// any other key: ignore, keep waiting on the welcome screen
		case <-g.done:
			return
		}
	}
}

func Run() {

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

	g.drawBorder()
	g.buffer.Flush()

	// welcome screen: draw ONCE, then block for specifically d/r/q
	// they're handled directly rather than routed through handleInput
	g.drawWelcome()
	g.welcomeLoop(keyCh)


	select {
	case <-g.done:
		return // 'q'/Ctrl+C during welcome or the editor. don't fall through
	default:
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
			g.handleInput(k, keyCh)
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



