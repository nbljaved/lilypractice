// Package alsa is a small binding to the ALSA sequencer: one client with
// named ports that sends and receives note events. PipeWire shows these
// ports in qpwgraph next to JACK and PipeWire ones.
package alsa

/*
#cgo pkg-config: alsa
#include <stdlib.h>
#include <string.h>
#include <alsa/asoundlib.h>

static int lp_open(snd_seq_t **seq, const char *name) {
	int err = snd_seq_open(seq, "default", SND_SEQ_OPEN_DUPLEX, 0);
	if (err < 0) return err;
	return snd_seq_set_client_name(*seq, name);
}

static int lp_port(snd_seq_t *seq, const char *name, int input) {
	unsigned int caps = input
		? SND_SEQ_PORT_CAP_WRITE | SND_SEQ_PORT_CAP_SUBS_WRITE
		: SND_SEQ_PORT_CAP_READ | SND_SEQ_PORT_CAP_SUBS_READ;
	return snd_seq_create_simple_port(seq, name, caps,
		SND_SEQ_PORT_TYPE_MIDI_GENERIC | SND_SEQ_PORT_TYPE_APPLICATION);
}

// type: 0 note on, 1 note off, 2 controller.
static int lp_send(snd_seq_t *seq, int port, int type, int channel, int a, int b) {
	snd_seq_event_t ev;
	snd_seq_ev_clear(&ev);
	snd_seq_ev_set_source(&ev, port);
	snd_seq_ev_set_subs(&ev);
	snd_seq_ev_set_direct(&ev);
	switch (type) {
	case 0: snd_seq_ev_set_noteon(&ev, channel, a, b); break;
	case 1: snd_seq_ev_set_noteoff(&ev, channel, a, b); break;
	default: snd_seq_ev_set_controller(&ev, channel, a, b); break;
	}
	return snd_seq_event_output_direct(seq, &ev);
}

typedef struct { int type, channel, a, b, port; } lp_event;

// Waits for the next event. type: 0 note on, 1 note off, 2 controller, -1 other.
static int lp_receive(snd_seq_t *seq, lp_event *out) {
	snd_seq_event_t *ev;
	int err = snd_seq_event_input(seq, &ev);
	if (err < 0) return err;
	out->port = ev->dest.port;
	out->type = -1;
	switch (ev->type) {
	case SND_SEQ_EVENT_NOTEON:
	case SND_SEQ_EVENT_NOTEOFF:
		out->type = (ev->type == SND_SEQ_EVENT_NOTEON && ev->data.note.velocity > 0) ? 0 : 1;
		out->channel = ev->data.note.channel;
		out->a = ev->data.note.note;
		out->b = ev->data.note.velocity;
		break;
	case SND_SEQ_EVENT_CONTROLLER:
		out->type = 2;
		out->channel = ev->data.control.channel;
		out->a = ev->data.control.param;
		out->b = ev->data.control.value;
		break;
	}
	return 0;
}

static int lp_connect(snd_seq_t *seq, int port, const char *other, int outgoing) {
	snd_seq_addr_t addr;
	int err = snd_seq_parse_address(seq, &addr, other);
	if (err < 0) return err;
	return outgoing ? snd_seq_connect_to(seq, port, addr.client, addr.port)
	                : snd_seq_connect_from(seq, port, addr.client, addr.port);
}

// Writes the names of all other clients into buf, one per line.
static void lp_clients(snd_seq_t *seq, char *buf, size_t size) {
	snd_seq_client_info_t *info;
	snd_seq_client_info_alloca(&info);
	snd_seq_client_info_set_client(info, -1);
	buf[0] = 0;
	size_t used = 0;
	while (snd_seq_query_next_client(seq, info) >= 0) {
		const char *name = snd_seq_client_info_get_name(info);
		size_t n = strlen(name);
		if (used + n + 2 > size) break;
		memcpy(buf + used, name, n);
		used += n;
		buf[used++] = '\n';
		buf[used] = 0;
	}
}
*/
import "C"

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"
)

type Port int

type Kind int

const (
	NoteOn Kind = iota
	NoteOff
	Controller
	Other
)

// Event is an incoming event. For notes A is the note and B the velocity;
// for controllers A is the controller number and B its value.
type Event struct {
	Kind    Kind
	Port    Port // our port that received it
	Channel int  // 0-15
	A, B    int
}

// Seq is one ALSA sequencer client.
type Seq struct {
	seq    *C.snd_seq_t
	sendMu sync.Mutex
}

// Open creates a sequencer client with this name.
func Open(name string) (*Seq, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	var seq *C.snd_seq_t
	if err := C.lp_open(&seq, cname); err < 0 {
		return nil, alsaError("opening the ALSA sequencer", err)
	}
	return &Seq{seq: seq}, nil
}

func (s *Seq) Close() { C.snd_seq_close(s.seq) }

// InputPort creates a port that other programs can send to.
func (s *Seq) InputPort(name string) (Port, error) { return s.port(name, 1) }

// OutputPort creates a port that other programs can listen to.
func (s *Seq) OutputPort(name string) (Port, error) { return s.port(name, 0) }

func (s *Seq) port(name string, input C.int) (Port, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	p := C.lp_port(s.seq, cname, input)
	if p < 0 {
		return 0, alsaError("creating port "+name, p)
	}
	return Port(p), nil
}

func (s *Seq) NoteOn(p Port, channel, note, velocity int) error {
	return s.send(p, 0, channel, note, velocity)
}

func (s *Seq) NoteOff(p Port, channel, note int) error {
	return s.send(p, 1, channel, note, 0)
}

func (s *Seq) Controller(p Port, channel, param, value int) error {
	return s.send(p, 2, channel, param, value)
}

func (s *Seq) send(p Port, kind, channel, a, b int) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if err := C.lp_send(s.seq, C.int(p), C.int(kind), C.int(channel), C.int(a), C.int(b)); err < 0 {
		return alsaError("sending", err)
	}
	return nil
}

// Receive waits for the next event sent to one of our input ports.
// Call it from one goroutine only.
func (s *Seq) Receive() (Event, error) {
	for {
		var ev C.lp_event
		err := C.lp_receive(s.seq, &ev)
		if err == -C.ENOSPC { // input overran; events were lost, carry on
			continue
		}
		if err < 0 {
			return Event{}, alsaError("receiving", err)
		}
		kind := Other
		switch ev._type {
		case 0:
			kind = NoteOn
		case 1:
			kind = NoteOff
		case 2:
			kind = Controller
		}
		return Event{Kind: kind, Port: Port(ev.port), Channel: int(ev.channel), A: int(ev.a), B: int(ev.b)}, nil
	}
}

// ConnectTo connects our port to another program's port, given as
// "client:port", a client number, or a client name such as "FLUID Synth".
func (s *Seq) ConnectTo(p Port, dest string) error { return s.connect(p, dest, 1) }

// ConnectFrom connects another program's port to our port.
func (s *Seq) ConnectFrom(p Port, src string) error { return s.connect(p, src, 0) }

func (s *Seq) connect(p Port, other string, outgoing C.int) error {
	cother := C.CString(other)
	defer C.free(unsafe.Pointer(cother))
	if err := C.lp_connect(s.seq, C.int(p), cother, outgoing); err < 0 {
		return alsaError("connecting to "+other, err)
	}
	return nil
}

// Clients lists the names of the sequencer clients, including ours.
func (s *Seq) Clients() []string {
	buf := make([]byte, 16384)
	C.lp_clients(s.seq, (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)))
	var names []string
	for _, line := range strings.Split(C.GoString((*C.char)(unsafe.Pointer(&buf[0]))), "\n") {
		if line != "" {
			names = append(names, line)
		}
	}
	return names
}

func alsaError(doing string, code C.int) error {
	return fmt.Errorf("%s: %s", doing, C.GoString(C.snd_strerror(code)))
}
