package provider

import (
	"encoding/binary"
	"hash"
)

// cxSHA is SHA-256 with an injectable initial state, which the Comix token
// needs: both digests start from words of the site's fingerprint key instead
// of the standard IV. The compression function is the FIPS 180-4 round
// function, verified against the standard digest for the default IV.
type cxSHA struct {
	state [8]uint32
	buf   [64]byte
	n     int
	total int
}

var cxK = [64]uint32{
	0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
	0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
	0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
	0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
	0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
	0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
	0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
	0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
}

var cxStdIV = [8]uint32{0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19}

func newCxSHA() *cxSHA {
	h := &cxSHA{}
	h.Reset()
	return h
}

// SetState installs a custom initial state (32 bytes, big-endian words). It
// must be called before any Write.
func (h *cxSHA) SetState(iv []byte) {
	for i := range h.state {
		h.state[i] = binary.BigEndian.Uint32(iv[i*4:])
	}
}

// SetTotalBits fixes the message length recorded in the final padding block.
// The Comix construction counts a synthetic 64-byte prefix block that is
// never written, so callers set the total directly and stream the payload
// with WriteRaw.
func (h *cxSHA) SetTotalBits(bits int) {
	h.total = bits
}

// WriteRaw streams bytes without counting them toward the total.
func (h *cxSHA) WriteRaw(p []byte) {
	for _, b := range p {
		h.buf[h.n] = b
		h.n++
		if h.n == 64 {
			h.compress(h.buf[:])
			h.n = 0
		}
	}
}

func (h *cxSHA) Write(p []byte) (int, error) {
	h.total += len(p) * 8
	h.WriteRaw(p)
	return len(p), nil
}
func (h *cxSHA) Sum(b []byte) []byte {
	// Clone so the digest can be taken mid-stream; padding is built from the
	// buffered tail exactly like a classic Merkle-Damgard finalization.
	clone := *h
	tail := append([]byte(nil), clone.buf[:clone.n]...)
	tail = append(tail, 0x80)
	for len(tail)%64 != 56 {
		tail = append(tail, 0)
	}
	var lenb [8]byte
	binary.BigEndian.PutUint64(lenb[:], uint64(clone.total))
	tail = append(tail, lenb[:]...)
	for i := 0; i < len(tail); i += 64 {
		clone.compress(tail[i : i+64])
	}
	out := make([]byte, 0, 32)
	for _, w := range clone.state {
		out = binary.BigEndian.AppendUint32(out, w)
	}
	return append(b, out...)
}

func (h *cxSHA) Reset() {
	h.state = cxStdIV
	h.n = 0
	h.total = 0
}

func (h *cxSHA) Size() int      { return 32 }
func (h *cxSHA) BlockSize() int { return 64 }

func (h *cxSHA) compress(block []byte) {
	var w [64]uint32
	for i := range 16 {
		w[i] = binary.BigEndian.Uint32(block[i*4:])
	}
	for i := 16; i < 64; i++ {
		s0 := rotr32(w[i-15], 7) ^ rotr32(w[i-15], 18) ^ (w[i-15] >> 3)
		s1 := rotr32(w[i-2], 17) ^ rotr32(w[i-2], 19) ^ (w[i-2] >> 10)
		w[i] = w[i-16] + s0 + w[i-7] + s1
	}
	a, b, c, d, e, f, g, hh := h.state[0], h.state[1], h.state[2], h.state[3], h.state[4], h.state[5], h.state[6], h.state[7]
	for i := range 64 {
		s1 := rotr32(e, 6) ^ rotr32(e, 11) ^ rotr32(e, 25)
		ch := (e & f) ^ (^e & g)
		t1 := hh + s1 + ch + cxK[i] + w[i]
		s0 := rotr32(a, 2) ^ rotr32(a, 13) ^ rotr32(a, 22)
		maj := (a & b) ^ (a & c) ^ (b & c)
		t2 := s0 + maj
		hh, g, f, e, d, c, b, a = g, f, e, d+t1, c, b, a, t1+t2
	}
	h.state[0] += a
	h.state[1] += b
	h.state[2] += c
	h.state[3] += d
	h.state[4] += e
	h.state[5] += f
	h.state[6] += g
	h.state[7] += hh
}

func rotr32(x uint32, n uint) uint32 { return x>>n | x<<(32-n) }

var _ hash.Hash = (*cxSHA)(nil)
