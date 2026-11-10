package identifier

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	versionBits    = 0x7000
	counterLimit   = 0x0FFF
	reseedMask     = 0x07FF
	variantBits    = 0x80
	variantMask    = 0x3F
	randomOffset   = 8
	uuidLength     = 16
	timestampShift = 16
	versionOffset  = 6
)

type UUIDv7Generator struct {
	now      func() time.Time
	random   io.Reader
	mutex    sync.Mutex
	last     uint64
	sequence uint16
}

func NewUUIDv7Generator(now func() time.Time, random io.Reader) *UUIDv7Generator {
	return &UUIDv7Generator{now: now, random: random}
}

func (g *UUIDv7Generator) NewID() string {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.advanceSequence()
	return formatUUID(g.encodeID())
}

func (g *UUIDv7Generator) advanceSequence() {
	milliseconds := uint64(g.now().UnixMilli())
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
	binary.BigEndian.PutUint64(id[:], g.last<<timestampShift)
	binary.BigEndian.PutUint16(id[versionOffset:], versionBits|g.sequence)
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
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}
