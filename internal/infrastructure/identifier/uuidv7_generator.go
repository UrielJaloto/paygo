package identifier

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	versionBits  = 0x7000
	counterLimit = 0x0FFF
	reseedMask   = 0x07FF
	variantBits  = 0x80
	variantMask  = 0x3F
	randomOffset = 8
	uuidLength   = 16
)

type UUIDv7Generator struct {
	clock    interface{ Now() time.Time }
	random   io.Reader
	mutex    sync.Mutex
	last     uint64
	sequence uint16
}

func NewMoney(clock interface{ Now() time.Time }, random io.Reader) *UUIDv7Generator {
	return &UUIDv7Generator{clock: clock, random: random}
}

func (g *UUIDv7Generator) NewID() string {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.advanceSequence()
	return formatUUID(g.encodeID())
}

func (g *UUIDv7Generator) advanceSequence() {
	milliseconds := uint64(g.clock.Now().UnixMilli())
	if milliseconds > g.last {
		g.last = milliseconds
		g.sequence = g.drawSeed()
		return
	}
	if g.sequence < counterLimit {
		g.sequence++
		return
	}
	g.last++
	g.sequence = g.drawSeed()
}

func (g *UUIDv7Generator) drawSeed() uint16 {
	var bytes [2]byte
	g.readRandom(bytes[:])
	return binary.BigEndian.Uint16(bytes[:]) & reseedMask
}

func (g *UUIDv7Generator) encodeID() [uuidLength]byte {
	var id [uuidLength]byte
	binary.BigEndian.PutUint64(id[:], g.last<<16)
	binary.BigEndian.PutUint16(id[6:], versionBits|g.sequence)
	g.readRandom(id[randomOffset:])
	id[randomOffset] = id[randomOffset]&variantMask | variantBits
	return id
}

func (g *UUIDv7Generator) readRandom(target []byte) {
	if _, err := io.ReadFull(g.random, target); err != nil {
		panic(fmt.Errorf("generating uuid: %w", err))
	}
}

func formatUUID(id [uuidLength]byte) string {
	text := hex.EncodeToString(id[:])
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:]
}
