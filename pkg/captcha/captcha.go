// Package captcha implements a server-side slider (puzzle) captcha used to gate
// registration / send-code against bots.
//
// The backend generates a background image with a "hole" and a draggable puzzle
// piece cut from the same image. The secret target X position is kept server-side
// only — the client receives just the two images and the piece's vertical
// position (thumbY). Solving requires dragging the piece so its centre aligns
// with the hole within a small tolerance, which a head-less bot cannot do
// without expensive template matching. A solved challenge yields a single-use,
// short-lived ticket that must be presented to /auth/send-code.
package captcha

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	width        = 320
	height       = 160
	pieceR       = 18 // piece radius
	tolerance    = 6  // px tolerance for a correct drag
	challengeTTL = 5 * time.Minute
	ticketTTL    = 10 * time.Minute
)

// item is either an active challenge (targetX set) or an issued ticket.
type item struct {
	targetX int
	expire  time.Time
	consumed bool
}

// Store holds active captcha challenges and issued tickets in memory.
type Store struct {
	mu sync.Mutex
	m  map[string]*item
}

// NewStore builds an in-memory captcha store.
func NewStore() *Store {
	return &Store{m: make(map[string]*item)}
}

func (s *Store) sweep() {
	now := time.Now()
	for k, v := range s.m {
		if now.After(v.expire) {
			delete(s.m, k)
		}
	}
}

// Generate creates a new slider challenge. It returns a key, the background and
// piece images as data-URIs, and the piece's vertical position (thumbY). The
// secret target horizontal position is intentionally NOT returned to the client.
func (s *Store) Generate() (key, bgB64, thumbB64 string, thumbY int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()

	minX := pieceR + 10
	maxX := width - pieceR - 10
	targetX := rand.Intn(maxX-minX+1) + minX
	minY := pieceR + 10
	maxY := height - pieceR - 10
	thumbY = rand.Intn(maxY-minY+1) + minY

	bg, piece := drawScene(targetX, thumbY)
	var bgBuf, pieceBuf bytes.Buffer
	if err = png.Encode(&bgBuf, bg); err != nil {
		return
	}
	if err = png.Encode(&pieceBuf, piece); err != nil {
		return
	}
	key = uuid.NewString()
	s.m[key] = &item{targetX: targetX, expire: time.Now().Add(challengeTTL)}
	bgB64 = "data:image/png;base64," + base64.StdEncoding.EncodeToString(bgBuf.Bytes())
	thumbB64 = "data:image/png;base64," + base64.StdEncoding.EncodeToString(pieceBuf.Bytes())
	return
}

// Verify checks the dragged piece-centre X against the secret target. On success
// it consumes the challenge and returns a single-use ticket.
func (s *Store) Verify(key string, x int) (ticket string, ok bool, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, found := s.m[key]
	if !found {
		return "", false, "验证码不存在或已过期"
	}
	if time.Now().After(it.expire) {
		delete(s.m, key)
		return "", false, "验证码已过期"
	}
	if it.consumed {
		return "", false, "验证码已被使用"
	}
	if abs(x-it.targetX) > tolerance {
		return "", false, "验证失败，请重新拖动拼合"
	}
	it.consumed = true
	delete(s.m, key)
	ticket = "cap_" + uuid.NewString()
	s.m[ticket] = &item{expire: time.Now().Add(ticketTTL)}
	return ticket, true, ""
}

// UseTicket consumes a captcha ticket. Returns false if missing / expired / already used.
func (s *Store) UseTicket(ticket string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, found := s.m[ticket]
	if !found {
		return false
	}
	if time.Now().After(it.expire) {
		delete(s.m, ticket)
		return false
	}
	delete(s.m, ticket)
	return true
}

// DebugTarget returns the secret target X for a challenge (test helper only).
func (s *Store) DebugTarget(key string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.m[key]
	if !ok {
		return 0, false
	}
	return it.targetX, true
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// ---------- image generation ----------

// drawScene builds the background (with a dimmed hole at targetX,thumbY) and the
// piece image (the puzzle shape, positioned at the left, content copied from the
// hole location so it fits perfectly when dragged to the target).
func drawScene(targetX, thumbY int) (bg, piece *image.RGBA) {
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	bg = image.NewRGBA(image.Rect(0, 0, width, height))

	c1 := randColor(rnd)
	c2 := randColor(rnd)
	for y := 0; y < height; y++ {
		t := float64(y) / float64(height)
		for x := 0; x < width; x++ {
			bg.Set(x, y, lerpColor(c1, c2, t))
		}
	}
	// decorative noise so the piece content differs from its surroundings
	for i := 0; i < 45; i++ {
		fillCircle(bg, rnd.Intn(width), rnd.Intn(height), rnd.Intn(22)+4, randColor(rnd), 0.35)
	}
	for i := 0; i < 7; i++ {
		drawLine(bg, rnd.Intn(width), rnd.Intn(height), rnd.Intn(width), rnd.Intn(height), randColor(rnd), 0.5)
	}

	// piece: shape centred at (pieceR, thumbY), pixels copied from the hole location
	piece = image.NewRGBA(image.Rect(0, 0, width, height))
	dx := targetX - pieceR
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if inPiece(x, y, pieceR, thumbY) {
				sx := x + dx
				if sx < 0 || sx >= width {
					continue
				}
				piece.Set(x, y, bg.At(sx, y))
			}
		}
	}
	strokePiece(piece, pieceR, thumbY, color.RGBA{255, 255, 255, 220})

	// carve the hole in the background
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if inPiece(x, y, targetX, thumbY) {
				or := bg.At(x, y)
				r, g, b, _ := or.RGBA()
				bg.Set(x, y, color.RGBA{uint8(r >> 9), uint8(g >> 9), uint8(b >> 9), 255})
			}
		}
	}
	strokePiece(bg, targetX, thumbY, color.RGBA{0, 0, 0, 90})
	return
}

func randColor(rnd *rand.Rand) color.RGBA {
	return color.RGBA{uint8(rnd.Intn(200) + 30), uint8(rnd.Intn(200) + 30), uint8(rnd.Intn(200) + 30), 255}
}

func lerpColor(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		uint8(float64(a.R)*(1-t) + float64(b.R)*t),
		uint8(float64(a.G)*(1-t) + float64(b.G)*t),
		uint8(float64(a.B)*(1-t) + float64(b.B)*t),
		255,
	}
}

// inPiece reports whether (x,y) is inside the puzzle shape centred at (cx,cy).
func inPiece(x, y, cx, cy int) bool {
	dx := x - cx
	dy := y - cy
	r := pieceR
	if dx*dx+dy*dy <= r*r {
		return true
	}
	// little tab on the right side
	tx := cx + r - 3
	ty := cy - r/2
	tr := r / 2
	if (x-tx)*(x-tx)+(y-ty)*(y-ty) <= tr*tr {
		return true
	}
	return false
}

func fillCircle(img *image.RGBA, cx, cy, r int, c color.RGBA, w float64) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) > r*r {
				continue
			}
			blendAt(img, x, y, c, w)
		}
	}
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA, w float64) {
	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	x, y := x0, y0
	for {
		blendAt(img, x, y, c, w)
		if x == x1 && y == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x += sx
		}
		if e2 < dx {
			err += dx
			y += sy
		}
	}
}

func blendAt(img *image.RGBA, x, y int, c color.RGBA, w float64) {
	b := img.Bounds()
	if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
		return
	}
	or := img.At(x, y)
	r, g, bl, _ := or.RGBA()
	nr := uint8(float64(r>>8)*(1-w) + float64(c.R)*w)
	ng := uint8(float64(g>>8)*(1-w) + float64(c.G)*w)
	nb := uint8(float64(bl>>8)*(1-w) + float64(c.B)*w)
	img.Set(x, y, color.RGBA{nr, ng, nb, 255})
}

// strokePiece draws a 1px outline around the shape (so the piece / hole is visible).
func strokePiece(img *image.RGBA, cx, cy int, c color.RGBA) {
	r := pieceR + 2
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if inPiece(x, y, cx, cy) {
				continue
			}
			if inPiece(x+1, y, cx, cy) || inPiece(x-1, y, cx, cy) ||
				inPiece(x, y+1, cx, cy) || inPiece(x, y-1, cx, cy) {
				img.Set(x, y, c)
			}
		}
	}
}
