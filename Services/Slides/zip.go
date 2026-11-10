package Slides

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"hash/crc32"
)

// zipEntry is one file in the package before it is written. Everything is held
// in memory, so every local header can carry real sizes and CRCs — which is the
// whole point: Go's archive/zip writer streams and sets the data-descriptor bit
// (flag bit 3), and LibreOffice's ODF importer refuses a package whose entries
// use data descriptors. A hand-written, fully-buffered zip with flag=0 imports
// cleanly.
type zipEntry struct {
	name  string
	data  []byte
	store bool // store uncompressed (mimetype) vs deflate
}

// buildZip assembles the entries into a single self-contained zip in memory.
func buildZip(entries []zipEntry) ([]byte, error) {
	var out bytes.Buffer
	var cd bytes.Buffer
	var count uint16
	var offset uint32
	for _, e := range entries {
		var raw []byte
		method := uint16(0)
		if !e.store {
			var comp bytes.Buffer
			fw, err := flate.NewWriter(&comp, flate.DefaultCompression)
			if err != nil {
				return nil, err
			}
			if _, err := fw.Write(e.data); err != nil {
				_ = fw.Close()
				return nil, err
			}
			if err := fw.Close(); err != nil {
				return nil, err
			}
			if comp.Len() < len(e.data) {
				raw = comp.Bytes()
				method = 8
			} else {
				raw = e.data
			}
		} else {
			raw = e.data
		}
		crc := crc32.ChecksumIEEE(e.data)
		localHeader(&out, e.name, method, crc, uint32(len(e.data)), uint32(len(raw)))
		out.Write(raw)
		centralEntry(&cd, e.name, method, crc, uint32(len(e.data)), uint32(len(raw)), offset)
		offset += uint32(len(raw)) + uint32(30+len(e.name))
		count++
	}
	cdStart := uint32(out.Len())
	out.Write(cd.Bytes())
	eocd(&out, count, cdStart, uint32(cd.Len()))
	return out.Bytes(), nil
}

// localHeader writes a local file header with flag 0, sizes and CRC inline.
func localHeader(w *bytes.Buffer, name string, method uint16, crc uint32, size, csize uint32) {
	b := make([]byte, 30+len(name))
	binary.LittleEndian.PutUint32(b[0:], 0x04034b50)
	binary.LittleEndian.PutUint16(b[4:], 20) // version needed: 2.0
	binary.LittleEndian.PutUint16(b[6:], 0)  // flags: no data descriptor
	binary.LittleEndian.PutUint16(b[8:], method)
	binary.LittleEndian.PutUint16(b[10:], 0) // mod time
	binary.LittleEndian.PutUint16(b[12:], 0) // mod date
	binary.LittleEndian.PutUint32(b[14:], crc)
	binary.LittleEndian.PutUint32(b[18:], csize)
	binary.LittleEndian.PutUint32(b[22:], size)
	binary.LittleEndian.PutUint16(b[26:], uint16(len(name)))
	binary.LittleEndian.PutUint16(b[28:], 0) // extra length
	copy(b[30:], name)
	w.Write(b)
}

// centralEntry writes one central directory record.
func centralEntry(w *bytes.Buffer, name string, method uint16, crc uint32, size, csize, offset uint32) {
	b := make([]byte, 46+len(name))
	binary.LittleEndian.PutUint32(b[0:], 0x02014b50)
	binary.LittleEndian.PutUint16(b[4:], 20) // version made by
	binary.LittleEndian.PutUint16(b[6:], 20) // version needed
	binary.LittleEndian.PutUint16(b[8:], 0)  // flags
	binary.LittleEndian.PutUint16(b[10:], method)
	binary.LittleEndian.PutUint16(b[12:], 0) // mod time
	binary.LittleEndian.PutUint16(b[14:], 0) // mod date
	binary.LittleEndian.PutUint32(b[16:], crc)
	binary.LittleEndian.PutUint32(b[20:], csize)
	binary.LittleEndian.PutUint32(b[24:], size)
	binary.LittleEndian.PutUint16(b[28:], uint16(len(name)))
	binary.LittleEndian.PutUint16(b[30:], 0) // extra length
	binary.LittleEndian.PutUint16(b[32:], 0) // comment length
	binary.LittleEndian.PutUint16(b[34:], 0) // disk number
	binary.LittleEndian.PutUint16(b[36:], 0) // internal attrs
	binary.LittleEndian.PutUint32(b[38:], 0) // external attrs
	binary.LittleEndian.PutUint32(b[42:], offset)
	copy(b[46:], name)
	w.Write(b)
}

// eocd writes the end-of-central-directory record.
func eocd(w *bytes.Buffer, count uint16, cdStart, cdSize uint32) {
	b := make([]byte, 22)
	binary.LittleEndian.PutUint32(b[0:], 0x06054b50)
	binary.LittleEndian.PutUint16(b[4:], 0) // disk number
	binary.LittleEndian.PutUint16(b[6:], 0) // disk with cd
	binary.LittleEndian.PutUint16(b[8:], count)
	binary.LittleEndian.PutUint16(b[10:], count)
	binary.LittleEndian.PutUint32(b[12:], cdSize)
	binary.LittleEndian.PutUint32(b[16:], cdStart)
	binary.LittleEndian.PutUint16(b[20:], 0) // comment length
	w.Write(b)
}
