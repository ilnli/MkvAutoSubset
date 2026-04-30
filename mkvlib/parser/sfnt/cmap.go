// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sfnt

import (
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/traditionalchinese"
)

// Platform IDs and Platform Specific IDs as per
// https://www.microsoft.com/typography/otspec/name.htm
const (
	pidUnicode   = 0
	pidMacintosh = 1
	pidWindows   = 3

	psidUnicode2BMPOnly        = 3
	psidUnicode2FullRepertoire = 4
	// Note that FontForge may generate a bogus Platform Specific ID (value 10)
	// for the Unicode Platform ID (value 0). See
	// https://github.com/fontforge/fontforge/issues/2728

	psidMacintoshRoman              = 0
	psidMacintoshTraditionalChinese = 2

	psidWindowsSymbol = 0
	psidWindowsUCS2   = 1
	psidWindowsBig5   = 4
	psidWindowsUCS4   = 10
)

type cmapRuneEncoder func(rune) (rune, bool)

type cmapMapping struct {
	glyphIndex glyphIndexFunc
	encode     cmapRuneEncoder
}

type cmapSubtable struct {
	pid, psid     uint16
	format        uint16
	offset        uint32
	length        uint32
	width         int
	encode        cmapRuneEncoder
	legacyEncoded bool
}

func encodeIdentity(r rune) (rune, bool) {
	return r, true
}

func encodeMacintoshRoman(r rune) (rune, bool) {
	x, ok := charmap.Macintosh.EncodeRune(r)
	return rune(x), ok
}

func encodeBig5(r rune) (rune, bool) {
	b, err := traditionalchinese.Big5.NewEncoder().Bytes([]byte(string(r)))
	if err != nil || len(b) == 0 || len(b) > 2 {
		return 0, false
	}
	x := rune(0)
	for _, c := range b {
		x = x<<8 | rune(c)
	}
	return x, true
}

// platformEncodingWidth returns the number of bytes per character assumed by
// the given Platform ID and Platform Specific ID.
//
// Very old fonts, from before Unicode was widely adopted, assume only 1 byte
// per character: a character map.
//
// Old fonts, from when Unicode meant the Basic Multilingual Plane (BMP),
// assume that 2 bytes per character is sufficient.
//
// Recent fonts naturally support the full range of Unicode code points, which
// can take up to 4 bytes per character. Such fonts might still choose one of
// the legacy encodings if e.g. their repertoire is limited to the BMP, for
// greater compatibility with older software, or because the resultant file
// size can be smaller.
func platformEncodingWidth(pid, psid uint16) int {
	width, _, _ := platformEncoding(pid, psid)
	return width
}

func platformEncoding(pid, psid uint16) (int, cmapRuneEncoder, bool) {
	switch pid {
	case pidUnicode:
		switch psid {
		case psidUnicode2BMPOnly:
			return 2, encodeIdentity, false
		case psidUnicode2FullRepertoire:
			return 4, encodeIdentity, false
		}

	case pidMacintosh:
		switch psid {
		case psidMacintoshRoman:
			return 1, encodeMacintoshRoman, false
		case psidMacintoshTraditionalChinese:
			return 2, encodeBig5, true
		}

	case pidWindows:
		switch psid {
		case psidWindowsSymbol:
			return 2, encodeIdentity, false
		case psidWindowsUCS2:
			return 2, encodeIdentity, false
		case psidWindowsBig5:
			return 2, encodeBig5, true
		case psidWindowsUCS4:
			return 4, encodeIdentity, false
		}
	}
	return 0, nil, false
}

// The various cmap formats are described at
// https://www.microsoft.com/typography/otspec/cmap.htm

var supportedCmapFormat = func(format, pid, psid uint16) bool {
	switch format {
	case 0:
		return pid == pidMacintosh && psid == psidMacintoshRoman
	case 2:
		return (pid == pidMacintosh && psid == psidMacintoshTraditionalChinese) ||
			(pid == pidWindows && psid == psidWindowsBig5)
	case 4:
		return true
	case 6:
		return true
	case 12:
		return true
	}
	return false
}

func (f *Font) makeCachedGlyphIndex(buf []byte, offset, length uint32, format uint16) ([]byte, glyphIndexFunc, error) {
	switch format {
	case 0:
		return f.makeCachedGlyphIndexFormat0(buf, offset, length)
	case 2:
		return f.makeCachedGlyphIndexFormat2(buf, offset, length)
	case 4:
		return f.makeCachedGlyphIndexFormat4(buf, offset, length)
	case 6:
		return f.makeCachedGlyphIndexFormat6(buf, offset, length)
	case 12:
		return f.makeCachedGlyphIndexFormat12(buf, offset, length)
	}
	panic("unreachable")
}

func (f *Font) makeCachedGlyphIndexFormat0(buf []byte, offset, length uint32) ([]byte, glyphIndexFunc, error) {
	if length != 6+256 || offset+length > f.cmap.length {
		return nil, nil, errInvalidCmapTable
	}
	var err error
	buf, err = f.src.view(buf, int(f.cmap.offset+offset), int(length))
	if err != nil {
		return nil, nil, err
	}
	var table [256]byte
	copy(table[:], buf[6:])
	return buf, func(f *Font, b *Buffer, r rune) (GlyphIndex, error) {
		if r < 0 || r > 0xff {
			return 0, nil
		}
		return GlyphIndex(table[byte(r)]), nil
	}, nil
}

func (f *Font) makeCachedGlyphIndexFormat2(buf []byte, offset, length uint32) ([]byte, glyphIndexFunc, error) {
	const headerSize = 6
	const subHeaderKeysSize = 512
	const subHeaderSize = 8
	if length < headerSize+subHeaderKeysSize || offset+length > f.cmap.length {
		return nil, nil, errInvalidCmapTable
	}
	var err error
	buf, err = f.src.view(buf, int(f.cmap.offset+offset), int(length))
	if err != nil {
		return nil, nil, err
	}
	if uint32(u16(buf[2:])) != length {
		return nil, nil, errInvalidCmapTable
	}

	subHeaderKeys := make([]uint16, 256)
	maxKey := uint16(0)
	for i := range subHeaderKeys {
		key := u16(buf[headerSize+2*i:])
		if key%subHeaderSize != 0 {
			return nil, nil, errInvalidCmapTable
		}
		subHeaderKeys[i] = key
		if key > maxKey {
			maxKey = key
		}
	}

	subHeadersOffset := headerSize + subHeaderKeysSize
	subHeaderCount := int(maxKey/subHeaderSize) + 1
	if subHeadersOffset+subHeaderCount*subHeaderSize > len(buf) {
		return nil, nil, errInvalidCmapTable
	}

	return buf, func(f *Font, b *Buffer, r rune) (GlyphIndex, error) {
		if r < 0 || r > 0xffff {
			return 0, nil
		}
		c := uint16(r)
		hi := byte(c >> 8)
		lo := byte(c)
		key := subHeaderKeys[hi]
		if hi != 0 && key == 0 {
			return 0, nil
		}

		subHeaderOffset := subHeadersOffset + int(key)
		firstCode := u16(buf[subHeaderOffset:])
		entryCount := u16(buf[subHeaderOffset+2:])
		idDelta := u16(buf[subHeaderOffset+4:])
		idRangeOffset := u16(buf[subHeaderOffset+6:])
		if uint16(lo) < firstCode || uint16(lo) >= firstCode+entryCount {
			return 0, nil
		}
		if idRangeOffset == 0 {
			return GlyphIndex(uint16(lo) + idDelta), nil
		}

		glyphOffset := subHeaderOffset + 6 + int(idRangeOffset) + 2*int(uint16(lo)-firstCode)
		if glyphOffset+2 > len(buf) {
			return 0, errInvalidCmapTable
		}
		glyph := u16(buf[glyphOffset:])
		if glyph == 0 {
			return 0, nil
		}
		return GlyphIndex(glyph + idDelta), nil
	}, nil
}

func (f *Font) makeCachedGlyphIndexFormat4(buf []byte, offset, length uint32) ([]byte, glyphIndexFunc, error) {
	const headerSize = 14
	if offset+headerSize > f.cmap.length {
		return nil, nil, errInvalidCmapTable
	}
	var err error
	buf, err = f.src.view(buf, int(f.cmap.offset+offset), headerSize)
	if err != nil {
		return nil, nil, err
	}
	offset += headerSize

	segCount := u16(buf[6:])
	if segCount&1 != 0 {
		return nil, nil, errInvalidCmapTable
	}
	segCount /= 2
	if segCount > maxCmapSegments {
		return nil, nil, errUnsupportedNumberOfCmapSegments
	}

	eLength := 8*uint32(segCount) + 2
	if offset+eLength > f.cmap.length {
		return nil, nil, errInvalidCmapTable
	}
	buf, err = f.src.view(buf, int(f.cmap.offset+offset), int(eLength))
	if err != nil {
		return nil, nil, err
	}
	offset += eLength

	entries := make([]cmapEntry16, segCount)
	for i := range entries {
		entries[i] = cmapEntry16{
			end:    u16(buf[0*len(entries)+0+2*i:]),
			start:  u16(buf[2*len(entries)+2+2*i:]),
			delta:  u16(buf[4*len(entries)+2+2*i:]),
			offset: u16(buf[6*len(entries)+2+2*i:]),
		}
	}
	indexesBase := f.cmap.offset + offset
	indexesLength := f.cmap.length - offset

	return buf, func(f *Font, b *Buffer, r rune) (GlyphIndex, error) {
		if uint32(r) > 0xffff {
			return 0, nil
		}

		c := uint16(r)
		for i, j := 0, len(entries); i < j; {
			h := i + (j-i)/2
			entry := &entries[h]
			if c < entry.start {
				j = h
			} else if entry.end < c {
				i = h + 1
			} else if entry.offset == 0 {
				return GlyphIndex(c + entry.delta), nil
			} else {
				offset := uint32(entry.offset) + 2*uint32(h-len(entries)+int(c-entry.start))
				if offset > indexesLength || offset+2 > indexesLength {
					return 0, errInvalidCmapTable
				}
				if b == nil {
					b = &Buffer{}
				}
				x, err := b.view(&f.src, int(indexesBase+offset), 2)
				if err != nil {
					return 0, err
				}
				glyph := u16(x)
				if glyph == 0 {
					return 0, nil
				}
				return GlyphIndex(glyph + entry.delta), nil
			}
		}
		return 0, nil
	}, nil
}

func (f *Font) makeCachedGlyphIndexFormat6(buf []byte, offset, length uint32) ([]byte, glyphIndexFunc, error) {
	const headerSize = 10
	if offset+headerSize > f.cmap.length {
		return nil, nil, errInvalidCmapTable
	}
	var err error
	buf, err = f.src.view(buf, int(f.cmap.offset+offset), headerSize)
	if err != nil {
		return nil, nil, err
	}
	offset += headerSize

	firstCode := u16(buf[6:])
	entryCount := u16(buf[8:])

	eLength := 2 * uint32(entryCount)
	if offset+eLength > f.cmap.length {
		return nil, nil, errInvalidCmapTable
	}

	if entryCount != 0 {
		buf, err = f.src.view(buf, int(f.cmap.offset+offset), int(eLength))
		if err != nil {
			return nil, nil, err
		}
		offset += eLength
	}

	entries := make([]uint16, entryCount)
	for i := range entries {
		entries[i] = u16(buf[2*i:])
	}

	return buf, func(f *Font, b *Buffer, r rune) (GlyphIndex, error) {
		if uint16(r) < firstCode {
			return 0, nil
		}

		c := int(uint16(r) - firstCode)
		if c >= len(entries) {
			return 0, nil
		}
		return GlyphIndex(entries[c]), nil
	}, nil
}

func (f *Font) makeCachedGlyphIndexFormat12(buf []byte, offset, _ uint32) ([]byte, glyphIndexFunc, error) {
	const headerSize = 16
	if offset+headerSize > f.cmap.length {
		return nil, nil, errInvalidCmapTable
	}
	var err error
	buf, err = f.src.view(buf, int(f.cmap.offset+offset), headerSize)
	if err != nil {
		return nil, nil, err
	}
	length := u32(buf[4:])
	if f.cmap.length < offset || length > f.cmap.length-offset {
		return nil, nil, errInvalidCmapTable
	}
	offset += headerSize

	numGroups := u32(buf[12:])
	if numGroups > maxCmapSegments {
		return nil, nil, errUnsupportedNumberOfCmapSegments
	}

	eLength := 12 * numGroups
	if headerSize+eLength != length {
		return nil, nil, errInvalidCmapTable
	}
	buf, err = f.src.view(buf, int(f.cmap.offset+offset), int(eLength))
	if err != nil {
		return nil, nil, err
	}
	offset += eLength

	entries := make([]cmapEntry32, numGroups)
	for i := range entries {
		entries[i] = cmapEntry32{
			start: u32(buf[0+12*i:]),
			end:   u32(buf[4+12*i:]),
			delta: u32(buf[8+12*i:]),
		}
	}

	return buf, func(f *Font, b *Buffer, r rune) (GlyphIndex, error) {
		c := uint32(r)
		for i, j := 0, len(entries); i < j; {
			h := i + (j-i)/2
			entry := &entries[h]
			if c < entry.start {
				j = h
			} else if entry.end < c {
				i = h + 1
			} else {
				return GlyphIndex(c - entry.start + entry.delta), nil
			}
		}
		return 0, nil
	}, nil
}

type cmapEntry16 struct {
	end, start, delta, offset uint16
}

type cmapEntry32 struct {
	start, end, delta uint32
}
