// HLMUX
//
// Copyright (C) 2022 hlpkg-dev
//
// This program is free software: you can redistribute it and/or modify it under
// the terms of the GNU General Public License as published by the Free Software
// Foundation, either version 3 of the License, or (at your option) any later
// version.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS
// FOR A PARTICULAR PURPOSE. See the GNU General Public License for more
// details.
//
// You should have received a copy of the GNU General Public License along with
// this program. If not, see <https://www.gnu.org/licenses/>.

package hlmux

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"math/bits"
)

// a wrapper of buffered IO reader
type Reader struct {
	r *bufio.Reader
}

func NewReader(data []byte) *Reader {
	return &Reader{
		r: bufio.NewReader(bytes.NewReader(data)),
	}
}

func (rd *Reader) Read(p []byte) (n int, err error) {
	return rd.r.Read(p)
}

func (rd *Reader) ReadByte() (byte, error) {
	return rd.r.ReadByte()
}

func (rd *Reader) ReadBytes(delim byte) ([]byte, error) {
	return rd.r.ReadBytes(delim)
}

func (rd *Reader) ReadLine() (line []byte, isPrefix bool, err error) {
	return rd.r.ReadLine()
}

func (rd *Reader) ReadRune() (r rune, size int, err error) {
	return rd.r.ReadRune()
}

func (rd *Reader) ReadSlice(delim byte) (line []byte, err error) {
	return rd.r.ReadSlice(delim)
}

func (rd *Reader) ReadString(delim byte) (string, error) {
	return rd.r.ReadString(delim)
}

func (rd *Reader) Size() int {
	return rd.r.Size()
}

func (rd *Reader) Peek(n int) ([]byte, error) {
	return rd.r.Peek(n)
}

func (rd *Reader) PeekByte() (byte, error) {
	data, err := rd.r.Peek(1)
	if err != nil {
		return 0, err
	}

	return data[0], nil
}

func (rd *Reader) PeekUint16() (uint16, error) {
	data, err := rd.r.Peek(2)
	if err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint16(data), nil
}

func (rd *Reader) PeekUint32() (uint32, error) {
	data, err := rd.r.Peek(4)
	if err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint32(data), nil
}

func (rd *Reader) ReadUint16() (uint16, error) {
	data := make([]byte, 2)
	_, err := io.ReadFull(rd.r, data)
	if err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint16(data), nil
}

func (rd *Reader) ReadUint32() (uint32, error) {
	data := make([]byte, 4)
	_, err := io.ReadFull(rd.r, data)
	if err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint32(data), nil
}

func (rd *Reader) Unmunge2(seq uint32) {
	data := make([]byte, rd.r.Size())
	rd.r.Read(data)

	seq = bits.ReverseBytes32(^seq) ^ seq

	table := []uint32{
		0xffffe7a5,
		0xbfefffe5,
		0xffbfefff,
		0xbfefbfed,
		0xbfafefbf,
		0xffbfafef,
		0xffefbfad,
		0xffffefbf,
		0xffeff7ef,
		0xbfefe7f5,
		0xbfbfe7e5,
		0xffafb7e7,
		0xbfffafb5,
		0xbfafffaf,
		0xffafa7ff,
		0xffefa7a5,
	}

	groups := len(data) / 4
	for i := 0; i < groups; i++ {
		block := data[i*4 : (i+1)*4]
		unmunged := make([]byte, 4)
		binary.LittleEndian.PutUint32(unmunged, binary.LittleEndian.Uint32(block)^seq^table[i%16])
		copy(block, unmunged)
	}

	rd.r = bufio.NewReader(bytes.NewReader(data))
}
