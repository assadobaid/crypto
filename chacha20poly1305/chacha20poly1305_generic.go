// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chacha20poly1305

import (
	"encoding/binary"
	"sync"

	"golang.org/x/crypto/chacha20"
	"golang.org/x/crypto/internal/subtle"
	"golang.org/x/crypto/poly1305"
)

func roundTo16(n int) int {
	return 16 * ((n + 15) / 16)
}

type syncpool struct{ sync.Pool }

func (s *syncpool) get(n int) []byte {
	if b, _ := s.Pool.Get().([]byte); cap(b) >= n {
		return b[:n]
	}
	return make([]byte, n)
}

func (s *syncpool) put(b []byte) {
	for i := range b {
		b[i] = 0
	}
	s.Pool.Put(b)
}

var pool = syncpool{}

func (c *chacha20poly1305) sealGeneric(dst, nonce, plaintext, additionalData []byte) []byte {
	ret, out := sliceForAppend(dst, len(plaintext)+poly1305.TagSize)
	if subtle.InexactOverlap(out, plaintext) {
		panic("chacha20poly1305: invalid buffer overlap")
	}

	var polyKey, discardBuf [32]byte
	s, _ := chacha20.NewUnauthenticatedCipher(c.key[:], nonce)
	s.XORKeyStream(polyKey[:], polyKey[:])
	s.XORKeyStream(discardBuf[:], discardBuf[:]) // skip the next 32 bytes
	s.XORKeyStream(out, plaintext)

	polyInput := pool.get(roundTo16(len(additionalData)) + roundTo16(len(plaintext)) + 8 + 8)
	copy(polyInput, additionalData)
	copy(polyInput[roundTo16(len(additionalData)):], out[:len(plaintext)])
	binary.LittleEndian.PutUint64(polyInput[len(polyInput)-16:], uint64(len(additionalData)))
	binary.LittleEndian.PutUint64(polyInput[len(polyInput)-8:], uint64(len(plaintext)))

	var tag [poly1305.TagSize]byte
	poly1305.Sum(&tag, polyInput, &polyKey)
	copy(out[len(plaintext):], tag[:])

	pool.put(polyInput)

	return ret
}

func (c *chacha20poly1305) openGeneric(dst, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	var tag [poly1305.TagSize]byte
	copy(tag[:], ciphertext[len(ciphertext)-16:])
	ciphertext = ciphertext[:len(ciphertext)-16]

	var polyKey, discardBuf [32]byte
	s, _ := chacha20.NewUnauthenticatedCipher(c.key[:], nonce)
	s.XORKeyStream(polyKey[:], polyKey[:])
	s.XORKeyStream(discardBuf[:], discardBuf[:]) // skip the next 32 bytes

	polyInput := make([]byte, roundTo16(len(additionalData))+roundTo16(len(ciphertext))+8+8)
	copy(polyInput, additionalData)
	copy(polyInput[roundTo16(len(additionalData)):], ciphertext)
	binary.LittleEndian.PutUint64(polyInput[len(polyInput)-16:], uint64(len(additionalData)))
	binary.LittleEndian.PutUint64(polyInput[len(polyInput)-8:], uint64(len(ciphertext)))

	ret, out := sliceForAppend(dst, len(ciphertext))
	if subtle.InexactOverlap(out, ciphertext) {
		panic("chacha20poly1305: invalid buffer overlap")
	}
	if !poly1305.Verify(&tag, polyInput, &polyKey) {
		for i := range out {
			out[i] = 0
		}
		return nil, errOpen
	}

	s.XORKeyStream(out, ciphertext)
	return ret, nil
}
