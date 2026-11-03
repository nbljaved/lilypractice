package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// Just enough of a Standard MIDI File reader to list the struck notes.

type midiFile struct {
	ppq   int // ticks per quarter note
	notes []midiNote
}

type midiNote struct {
	tick  int64
	pitch int
	track int
}

func readMIDI(path string) (*midiFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 14 || string(b[:4]) != "MThd" {
		return nil, fmt.Errorf("%s: not a MIDI file", path)
	}
	headerLen := int(binary.BigEndian.Uint32(b[4:8]))
	tracks := int(binary.BigEndian.Uint16(b[10:12]))
	division := binary.BigEndian.Uint16(b[12:14])
	if division&0x8000 != 0 {
		return nil, fmt.Errorf("%s: SMPTE timing is not supported", path)
	}
	m := &midiFile{ppq: int(division)}
	pos := 8 + headerLen
	for t := 0; t < tracks; t++ {
		if pos+8 > len(b) || string(b[pos:pos+4]) != "MTrk" {
			return nil, fmt.Errorf("%s: track %d is missing", path, t)
		}
		n := int(binary.BigEndian.Uint32(b[pos+4 : pos+8]))
		if pos+8+n > len(b) {
			return nil, fmt.Errorf("%s: track %d is cut short", path, t)
		}
		if err := m.readTrack(b[pos+8:pos+8+n], t); err != nil {
			return nil, fmt.Errorf("%s: track %d: %w", path, t, err)
		}
		pos += 8 + n
	}
	return m, nil
}

var errShort = errors.New("event is cut short")

func (m *midiFile) readTrack(data []byte, track int) error {
	var tick int64
	var status byte
	i := 0
	varlen := func() (int64, error) {
		var v int64
		for k := 0; k < 4; k++ {
			if i >= len(data) {
				return 0, errShort
			}
			c := data[i]
			i++
			v = v<<7 | int64(c&0x7f)
			if c&0x80 == 0 {
				return v, nil
			}
		}
		return 0, errors.New("variable-length number is too long")
	}
	need := func(n int) error {
		if i+n > len(data) {
			return errShort
		}
		return nil
	}
	for i < len(data) {
		delta, err := varlen()
		if err != nil {
			return err
		}
		tick += delta
		if err := need(1); err != nil {
			return err
		}
		if data[i] >= 0x80 {
			status = data[i]
			i++
		} else if status == 0 {
			return errors.New("running status without a status byte")
		}
		switch {
		case status == 0xff: // meta event
			if err := need(1); err != nil {
				return err
			}
			i++ // type
			n, err := varlen()
			if err != nil {
				return err
			}
			i += int(n)
			status = 0
		case status == 0xf0 || status == 0xf7: // sysex
			n, err := varlen()
			if err != nil {
				return err
			}
			i += int(n)
			status = 0
		case status&0xf0 == 0xc0 || status&0xf0 == 0xd0:
			if err := need(1); err != nil {
				return err
			}
			i++
		default:
			if err := need(2); err != nil {
				return err
			}
			if status&0xf0 == 0x90 && data[i+1] > 0 {
				m.notes = append(m.notes, midiNote{tick: tick, pitch: int(data[i]), track: track})
			}
			i += 2
		}
	}
	return nil
}
