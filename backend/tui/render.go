package tui

import (
	"fmt"
	"strings"
	"time"
)

const (
	fgGold   = 220
	fgCyan   = 81
	fgGreen  = 42
	fgYellow = 214
	fgRed    = 203
	fgDim    = 245
	fgText   = 252
)

type style struct {
	fg        int
	bg        int
	bold      bool
	dim       bool
	underline bool
}

func (s style) seq() string {
	if s.fg == 0 && s.bg == 0 && !s.bold && !s.dim && !s.underline {
		return "\x1b[0m"
	}
	parts := []string{"0"}
	if s.bold {
		parts = append(parts, "1")
	}
	if s.dim {
		parts = append(parts, "2")
	}
	if s.underline {
		parts = append(parts, "4")
	}
	if s.fg > 0 {
		parts = append(parts, fmt.Sprintf("38;5;%d", s.fg))
	}
	if s.bg > 0 {
		parts = append(parts, fmt.Sprintf("48;5;%d", s.bg))
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

type cell struct {
	r  rune
	st style
}

type canvas struct {
	w, h    int
	cells   []cell
	color   bool
	unicode bool
}

func newCanvas(w, h int, color, unicode bool) *canvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w > 500 {
		w = 500
	}
	if h > 200 {
		h = 200
	}
	cells := make([]cell, w*h)
	for i := range cells {
		cells[i].r = ' '
	}
	return &canvas{w: w, h: h, cells: cells, color: color, unicode: unicode}
}

func (c *canvas) set(x, y int, r rune, st style) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	c.cells[y*c.w+x] = cell{r: r, st: st}
}

func (c *canvas) fill(x, y, w, h int, r rune, st style) {
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			c.set(x+col, y+row, r, st)
		}
	}
}

func (c *canvas) print(x, y int, st style, s string) {
	if y < 0 || y >= c.h {
		return
	}
	for _, r := range s {
		if x >= c.w {
			return
		}
		if x >= 0 {
			c.cells[y*c.w+x] = cell{r: r, st: st}
		}
		x++
	}
}

func (c *canvas) glyphs() (tl, tr, bl, br, hz, vt rune) {
	if c.unicode {
		return '┌', '┐', '└', '┘', '─', '│'
	}
	return '+', '+', '+', '+', '-', '|'
}

func (c *canvas) box(x, y, w, h int, title string) {
	if w < 2 || h < 2 {
		return
	}
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x >= c.w || y >= c.h {
		return
	}
	if x+w > c.w {
		w = c.w - x
	}
	if y+h > c.h {
		h = c.h - y
	}
	if w < 2 || h < 2 {
		return
	}
	tl, tr, bl, br, hz, vt := c.glyphs()
	dim := style{fg: fgDim}
	for i := 0; i < w; i++ {
		top := hz
		bot := hz
		switch i {
		case 0:
			top, bot = tl, bl
		case w - 1:
			top, bot = tr, br
		}
		c.set(x+i, y, top, dim)
		c.set(x+i, y+h-1, bot, dim)
	}
	for i := 1; i < h-1; i++ {
		c.set(x, y+i, vt, dim)
		c.set(x+w-1, y+i, vt, dim)
	}
	if title != "" && w > 4 {
		label := " " + title + " "
		max := w - 2
		if runeLen(label) > max {
			label = shorten(label, max)
		}
		c.print(x+1, y, style{fg: fgGold, bold: true}, label)
	}
}

func (c *canvas) String() string {
	var b strings.Builder
	// Disable auto-wrap so a full-width row does not scroll the screen.
	b.WriteString("\x1b[?7l\x1b[H")
	for y := 0; y < c.h; y++ {
		if y > 0 {
			b.WriteByte('\n')
		}
		if !c.color {
			for x := 0; x < c.w; x++ {
				b.WriteRune(c.cells[y*c.w+x].r)
			}
			b.WriteString("\x1b[K")
			continue
		}
		var cur style
		written := false
		for x := 0; x < c.w; x++ {
			cell := c.cells[y*c.w+x]
			if !written || cell.st != cur {
				b.WriteString(cell.st.seq())
				cur = cell.st
				written = true
			}
			b.WriteRune(cell.r)
		}
		b.WriteString("\x1b[0m\x1b[K")
	}
	return b.String()
}

// ViewState is the operator's place in the console, separate from node data.
// ASCII opts into '+' borders. The zero value uses Unicode box drawing.
type ViewState struct {
	View   View
	Help   bool
	Color  bool
	ASCII  bool
	Attach bool
	Now    time.Time
	Flash  string
}

// Render draws one frame. w and h are the terminal size in cells.
func Render(snap Snapshot, vs ViewState, w, h int) string {
	c := newCanvas(w, h, vs.Color, !vs.ASCII)
	if h < 8 || w < 40 {
		renderCompact(c, snap, vs)
	} else {
		switch vs.View {
		case ViewMonitor:
			renderMonitor(c, snap, vs)
		case ViewLogs:
			renderLogs(c, snap, vs)
		default:
			renderWelcome(c, snap, vs)
		}
		if vs.Help {
			renderHelp(c, vs)
		}
	}
	return c.String()
}

func (vs ViewState) withNow(snap Snapshot) time.Time {
	if !vs.Now.IsZero() {
		return vs.Now
	}
	if !snap.SampledAt.IsZero() {
		return snap.SampledAt
	}
	return time.Now()
}

func healthStyle(status string) style {
	switch status {
	case "healthy":
		return style{fg: fgGreen, bold: true}
	case "syncing", "starting":
		return style{fg: fgYellow, bold: true}
	case "degraded", "down", "stopping":
		return style{fg: fgRed, bold: true}
	default:
		return style{fg: fgYellow, bold: true}
	}
}

func statusWord(snap Snapshot) string {
	// Shutdown wins over the last healthy sample so the screen matches the process.
	if snap.Phase == PhaseStopping {
		return "stopping"
	}
	if snap.HealthStatus != "" {
		return snap.HealthStatus
	}
	if snap.Phase != "" {
		return string(snap.Phase)
	}
	return "starting"
}

func footerText(vs ViewState) string {
	if vs.Flash != "" {
		return vs.Flash
	}
	stop := "q stop"
	if vs.Attach {
		stop = "q close"
	}
	return stop + "   o open   w welcome   m monitor   l logs   ? help"
}

func (c *canvas) header(snap Snapshot, vs ViewState) {
	word := statusWord(snap)
	c.print(1, 0, style{fg: fgGold, bold: true}, "Stargate")
	c.print(11, 0, healthStyle(word), word)
	detailX := 12 + runeLen(word)
	ver := snap.Version
	verX := c.w - runeLen(ver) - 1
	if verX < detailX {
		verX = detailX
	}
	if snap.Detail != "" && detailX < verX-1 {
		c.print(detailX, 0, style{fg: fgDim}, shorten(snap.Detail, verX-detailX-1))
	}
	if ver != "" && verX > detailX && verX < c.w {
		c.print(verX, 0, style{fg: fgDim}, shorten(ver, c.w-verX))
	}
	_, _, _, _, hz, _ := c.glyphs()
	rule := style{fg: fgDim}
	for x := 0; x < c.w; x++ {
		c.set(x, 1, hz, rule)
	}
}

func (c *canvas) footer(vs ViewState) {
	y := c.h - 1
	c.print(1, y, style{fg: fgDim}, shorten(footerText(vs), c.w-2))
}

func renderCompact(c *canvas, snap Snapshot, vs ViewState) {
	word := statusWord(snap)
	c.print(0, 0, style{fg: fgGold, bold: true}, shorten("Stargate  "+word, c.w))
	if c.h > 1 {
		url := snap.UIURL
		if url == "" {
			url = "(waiting for the listen address)"
		}
		c.print(0, 1, style{fg: fgCyan, bold: true}, shorten(url, c.w))
	}
	if c.h > 2 {
		c.print(0, 2, style{fg: fgText}, shorten(chainLabel(snap), c.w))
	}
	if c.h > 3 {
		c.print(0, 3, style{fg: fgDim}, shorten("Open the URL in a browser. "+footerText(vs), c.w))
	}
}

func wrapText(s string, width int) []string {
	if width < 8 {
		width = 8
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	cur := ""
	for _, w := range words {
		if cur == "" {
			cur = w
			continue
		}
		if runeLen(cur)+1+runeLen(w) > width {
			lines = append(lines, cur)
			cur = w
			continue
		}
		cur += " " + w
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func renderWelcome(c *canvas, snap Snapshot, vs ViewState) {
	c.header(snap, vs)
	c.footer(vs)
	y := 3
	limit := c.h - 2
	if y >= limit {
		return
	}
	c.print(2, y, style{fg: fgGold, bold: true}, "Open this in your browser")
	y += 2
	if y >= limit {
		return
	}
	url := snap.UIURL
	if url == "" {
		url = "(waiting for the listen address)"
	}
	c.print(2, y, style{fg: fgCyan, bold: true, underline: true}, shorten(url, c.w-4))
	y += 2
	if snap.FetchError != "" && y < limit {
		c.print(2, y, style{fg: fgRed}, shorten(snap.FetchError, c.w-4))
		y += 2
	}
	intro := "Inscribe a wish, review the work, and pay from that page. This console is the live view of the node: chain, scanner, mirror, and agents."
	for _, line := range wrapText(intro, c.w-4) {
		if y >= limit-8 && c.h < 36 {
			break
		}
		if y >= limit {
			break
		}
		c.print(2, y, style{fg: fgText}, line)
		y++
	}
	y++
	links := [][2]string{
		{"Browser", snap.UIURL},
		{"Guides", snap.DocsURL},
		{"API", snap.APIURL},
		{"Agent tools", snap.MCPURL},
	}
	if y+len(links) < limit-6 || c.h >= 32 {
		for _, link := range links {
			if y >= limit {
				break
			}
			if link[1] == "" {
				continue
			}
			c.print(2, y, style{fg: fgDim}, link[0])
			c.print(14, y, style{fg: fgCyan}, shorten(link[1], c.w-16))
			y++
		}
		y++
	}
	rows := [][2]string{
		{"Chain", chainLabel(snap)},
		{"Scanner", scannerLabel(snap)},
		{"Storage", storageLabel(snap)},
		{"Mirror", mirrorLabel(snap)},
		{"Agents", agentLabel(snap)},
		{"Uptime", formatUptime(snap.StartedAt, vs.withNow(snap))},
	}
	boxH := len(rows) + 2
	if y+boxH <= limit {
		c.box(1, y, c.w-2, boxH, "Status")
		for i, row := range rows {
			c.kv(3, y+1+i, c.w-6, row[0], row[1], style{fg: fgText})
		}
		y += boxH + 1
	} else {
		for _, row := range rows {
			if y >= limit {
				break
			}
			c.kv(2, y, c.w-4, row[0], row[1], style{fg: fgText})
			y++
		}
	}
	if y < limit {
		c.print(2, y, style{fg: fgDim}, "Recent")
		y++
		c.logLines(2, y, limit, snap)
	}
}

func (c *canvas) kv(x, y, width int, label, value string, vs style) {
	if y < 0 || y >= c.h || width < 4 {
		return
	}
	labW := 12
	if width < 28 {
		labW = 10
	}
	c.print(x, y, style{fg: fgDim}, shorten(label, labW))
	c.print(x+labW, y, vs, shorten(value, width-labW))
}

func (c *canvas) logLines(x, y, limit int, snap Snapshot) {
	if y >= limit {
		return
	}
	room := limit - y
	lines := snap.Logs
	if len(lines) > room {
		lines = lines[len(lines)-room:]
	}
	width := c.w - x - 1
	for _, line := range lines {
		if y >= limit {
			return
		}
		c.print(x, y, style{fg: fgDim}, shorten(line, width))
		y++
	}
}

func renderMonitor(c *canvas, snap Snapshot, vs ViewState) {
	c.header(snap, vs)
	c.footer(vs)
	url := snap.UIURL
	if url == "" {
		url = "(waiting for the listen address)"
	}
	c.print(2, 2, style{fg: fgDim}, "Browser")
	c.print(12, 2, style{fg: fgCyan, bold: true}, shorten(url, c.w-14))
	now := vs.withNow(snap)
	topY := 4
	bottomLimit := c.h - 2
	if c.w >= 76 {
		gap := 1
		leftW := (c.w - 3 - gap) / 2
		rightW := c.w - 3 - gap - leftW
		topH := 10
		if bottomLimit-topY < 16 {
			topH = 8
		}
		if topY+topH > bottomLimit {
			topH = bottomLimit - topY
		}
		if topH >= 4 {
			c.chainBox(1, topY, leftW, topH, snap)
			c.nodeBox(1+leftW+gap, topY, rightW, topH, snap, now)
		}
		botY := topY + topH
		botH := 8
		if botY+botH > bottomLimit-2 {
			botH = bottomLimit - botY - 1
		}
		if botH >= 4 {
			c.monitorBox(1, botY, leftW, botH, snap)
			c.mirrorBox(1+leftW+gap, botY, rightW, botH, snap)
			botY += botH
		}
		if botY < bottomLimit {
			c.print(2, botY, style{fg: fgDim}, "Recent")
			c.logLines(2, botY+1, bottomLimit, snap)
		}
		return
	}
	y := topY
	h := 8
	if y+h <= bottomLimit {
		c.chainBox(1, y, c.w-2, h, snap)
		y += h
	}
	if y+h <= bottomLimit {
		c.nodeBox(1, y, c.w-2, h, snap, now)
		y += h
	}
	if y < bottomLimit {
		c.print(2, y, style{fg: fgDim}, "Recent")
		c.logLines(2, y+1, bottomLimit, snap)
	}
}

func (c *canvas) chainBox(x, y, w, h int, snap Snapshot) {
	c.box(x, y, w, h, "Chain")
	rows := [][2]string{
		{"Network", dash(snap.Network)},
		{"Mode", dash(snap.ChainMode)},
		{"Height", heightField(snap.BlockHeight, snap.BitcoinConnected)},
		{"Headers", heightField(snap.HeaderHeight, snap.HeaderHeight > 0)},
		{"Peers", peerLabel(snap)},
		{"Sync", syncLabel(snap)},
		{"Hash", hashField(snap.BestBlockHash)},
		{"Lag", lagField(snap)},
	}
	c.rows(x+2, y+1, w-4, h-2, rows, style{fg: fgText})
}

func (c *canvas) nodeBox(x, y, w, h int, snap Snapshot, now time.Time) {
	c.box(x, y, w, h, "Node")
	rows := [][2]string{
		{"Health", statusWord(snap)},
		{"Scanner", scannerLabel(snap)},
		{"Storage", storageLabel(snap)},
		{"Agents", agentLabel(snap)},
		{"Uptime", formatUptime(snap.StartedAt, now)},
		{"Port", dash(snap.HTTPPort)},
		{"Routines", formatInt(int64(snap.Goroutines))},
	}
	valueStyle := style{fg: fgText}
	c.rows(x+2, y+1, w-4, h-2, rows, valueStyle)
	// Recolor the health value specifically: the row printer uses one style.
	// Print health again in the pill color.
	if h > 2 {
		c.kv(x+2, y+1, w-4, "Health", statusWord(snap), healthStyle(statusWord(snap)))
	}
}

func (c *canvas) monitorBox(x, y, w, h int, snap Snapshot) {
	c.box(x, y, w, h, "Block monitor")
	state := "stopped"
	if snap.MonitorRunning {
		state = "running"
	} else if snap.Phase == PhaseStarting {
		state = "starting"
	}
	rows := [][2]string{
		{"State", state},
		{"Height", heightField(snap.MonitorHeight, snap.MonitorRunning || snap.MonitorHeight > 0)},
		{"Processed", formatInt(snap.BlocksProcessed)},
		{"Inscriptions", formatInt(snap.InscriptionsSeen)},
		{"Contracts", formatInt(snap.ContractsSeen)},
		{"Images", formatInt(snap.ImagesSeen)},
	}
	c.rows(x+2, y+1, w-4, h-2, rows, style{fg: fgText})
}

func (c *canvas) mirrorBox(x, y, w, h int, snap Snapshot) {
	c.box(x, y, w, h, "IPFS mirror")
	rows := [][2]string{
		{"Uploads", mirrorSide(snap.MirrorEnabled, snap.MirrorUploads, snap)},
		{"Wishes", mirrorSide(snap.WishMirrorOn, snap.MirrorWishes, snap)},
		{"Peers", formatInt(int64(snap.MirrorPeers))},
		{"Peer", hashField(snap.MirrorPeerID)},
	}
	c.rows(x+2, y+1, w-4, h-2, rows, style{fg: fgText})
}

func mirrorSide(on bool, n int, snap Snapshot) string {
	if !on {
		if snap.Phase == PhaseStarting || snap.HealthStatus == "starting" {
			return "starting"
		}
		return "off"
	}
	return onOff(true) + " · " + formatInt(int64(n)) + " files"
}

func (c *canvas) rows(x, y, w, h int, rows [][2]string, vs style) {
	if h < 1 || w < 4 {
		return
	}
	n := len(rows)
	if n > h {
		n = h
	}
	for i := 0; i < n; i++ {
		c.kv(x, y+i, w, rows[i][0], rows[i][1], vs)
	}
}

func heightField(n int64, known bool) string {
	if !known && n == 0 {
		return "—"
	}
	return formatInt(n)
}

func hashField(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func lagField(snap Snapshot) string {
	if snap.TipHashMismatch {
		return "hash mismatch"
	}
	if !snap.TipLagging {
		if snap.TipExternal > 0 {
			return "none"
		}
		return "—"
	}
	if snap.TipExternal > 0 {
		return fmt.Sprintf("%s behind %s", formatInt(snap.TipLagBlocks), formatInt(snap.TipExternal))
	}
	return formatInt(snap.TipLagBlocks) + " blocks"
}

func renderLogs(c *canvas, snap Snapshot, vs ViewState) {
	c.header(snap, vs)
	c.footer(vs)
	url := snap.UIURL
	if url == "" {
		url = "(waiting for the listen address)"
	}
	c.print(2, 2, style{fg: fgDim}, "Browser")
	c.print(12, 2, style{fg: fgCyan}, shorten(url, c.w-14))
	c.print(2, 3, style{fg: fgDim}, "Log")
	c.logLines(2, 4, c.h-1, snap)
}

func renderHelp(c *canvas, vs ViewState) {
	stop := "Stop the node"
	if vs.Attach {
		stop = "Disconnect"
	}
	lines := []string{
		"Welcome tells you where the browser UI is.",
		"Monitor is the live chain, mirror, and agents.",
		"",
		"w or 1    Welcome",
		"m or 2    Monitor",
		"l or 3    Logs",
		"tab       Cycle views",
		"o         Open the browser UI",
		"? or h    Toggle this help",
		"q         " + stop,
		"Ctrl-C    " + stop,
		"esc       Close help",
	}
	w := 52
	if c.w-4 < w {
		w = c.w - 4
	}
	h := len(lines) + 2
	if w < 20 || h+2 > c.h {
		return
	}
	x := (c.w - w) / 2
	y := (c.h - h) / 2
	if y < 1 {
		y = 1
	}
	// Paint a solid light panel first. The border alone leaves the console
	// text visible in every cell the help lines do not overwrite.
	const bgHelp = 231
	face := style{fg: 16, bg: bgHelp}
	edge := style{fg: 94, bg: bgHelp}
	c.fill(x, y, w, h, ' ', face)
	c.frame(x, y, w, h, edge)
	title := " Keys "
	if runeLen(title) > w-2 {
		title = shorten(title, w-2)
	}
	c.print(x+1, y, style{fg: 94, bold: true, bg: bgHelp}, title)
	inner := w - 4
	if inner < 1 {
		inner = 1
	}
	for i, line := range lines {
		text := shorten(line, inner)
		pad := inner - runeLen(text)
		if pad > 0 {
			text += strings.Repeat(" ", pad)
		}
		c.print(x+2, y+1+i, face, text)
	}
}

func (c *canvas) frame(x, y, w, h int, st style) {
	if w < 2 || h < 2 {
		return
	}
	tl, tr, bl, br, hz, vt := c.glyphs()
	for i := 0; i < w; i++ {
		top, bot := hz, hz
		switch i {
		case 0:
			top, bot = tl, bl
		case w - 1:
			top, bot = tr, br
		}
		c.set(x+i, y, top, st)
		c.set(x+i, y+h-1, bot, st)
	}
	for i := 1; i < h-1; i++ {
		c.set(x, y+i, vt, st)
		c.set(x+w-1, y+i, vt, st)
	}
}
