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

func New(clock interface{ Now() time.Time }, random io.Reader) *UUIDv7Generator {
	return &UUIDv7Generator{clock: clock, random: random}
}

func (g *UUIDv7Generator) NewID() string {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.advance()
	return format(g.encode())
}

func (g *UUIDv7Generator) advance() {
	millis := uint64(g.clock.Now().UnixMilli())
	if millis > g.last {
		g.last = millis
		g.sequence = g.seed()
		return
	}
	if g.sequence < counterLimit {
		g.sequence++
		return
	}
	g.last++
	g.sequence = g.seed()
}

func (g *UUIDv7Generator) seed() uint16 {
	var bytes [2]byte
	g.read(bytes[:])
	return binary.BigEndian.Uint16(bytes[:]) & reseedMask
}

func (g *UUIDv7Generator) encode() [uuidLength]byte {
	var id [uuidLength]byte
	binary.BigEndian.PutUint64(id[:], g.last<<16)
	binary.BigEndian.PutUint16(id[6:], versionBits|g.sequence)
	g.read(id[randomOffset:])
	id[randomOffset] = id[randomOffset]&variantMask | variantBits
	return id
}

func (g *UUIDv7Generator) read(target []byte) {
	if _, err := io.ReadFull(g.random, target); err != nil {
		panic(fmt.Errorf("generating uuid: %w", err))
	}
}

func format(id [uuidLength]byte) string {
	text := hex.EncodeToString(id[:])
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:]
}
