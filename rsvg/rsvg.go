// Package rsvg is a small binding to librsvg: draw an SVG page with cairo,
// and find or recolour single elements by id.
package rsvg

/*
#cgo pkg-config: librsvg-2.0 cairo
#include <stdlib.h>
#include <librsvg/rsvg.h>

// Draws one element in a solid colour, keeping its shape and antialiasing.
static gboolean lp_render_tinted(RsvgHandle *h, cairo_t *cr, const char *id,
                                 const RsvgRectangle *viewport,
                                 double r, double g, double b, GError **err) {
	cairo_save(cr);
	cairo_push_group(cr);
	gboolean ok = rsvg_handle_render_layer(h, cr, id, viewport, err);
	cairo_pattern_t *shape = cairo_pop_group(cr);
	if (ok) {
		cairo_set_source_rgb(cr, r, g, b);
		cairo_mask(cr, shape);
	}
	cairo_pattern_destroy(shape);
	cairo_restore(cr);
	return ok;
}
*/
import "C"

import (
	"errors"
	"unsafe"

	"github.com/diamondburned/gotk4/pkg/cairo"
)

// Rect is a rectangle in the same units as the viewport it was asked for.
type Rect struct{ X, Y, W, H float64 }

// Doc is a loaded SVG document.
type Doc struct {
	h *C.RsvgHandle
	// Size at 96 dpi, in CSS pixels. Render with a viewport of this size
	// to get the document at 100%.
	Width, Height float64
}

func Open(path string) (*Doc, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var gerr *C.GError
	h := C.rsvg_handle_new_from_file(cpath, &gerr)
	if h == nil {
		return nil, takeError(gerr)
	}
	C.rsvg_handle_set_dpi(h, 96)
	d := &Doc{h: h}
	var w, ht C.gdouble
	if C.rsvg_handle_get_intrinsic_size_in_pixels(h, &w, &ht) == 0 {
		d.Close()
		return nil, errors.New(path + ": SVG has no usable size")
	}
	d.Width, d.Height = float64(w), float64(ht)
	return d, nil
}

func (d *Doc) Close() {
	if d.h != nil {
		C.g_object_unref(C.gpointer(d.h))
		d.h = nil
	}
}

// Viewport is the whole document at 100%.
func (d *Doc) Viewport() Rect { return Rect{0, 0, d.Width, d.Height} }

// Render draws the document into viewport.
func (d *Doc) Render(cr *cairo.Context, viewport Rect) error {
	var gerr *C.GError
	vp := viewport.c()
	if C.rsvg_handle_render_document(d.h, ctx(cr), &vp, &gerr) == 0 {
		return takeError(gerr)
	}
	return nil
}

// Bounds returns the ink rectangle of the element with this id, in the
// coordinates of viewport.
func (d *Doc) Bounds(id string, viewport Rect) (Rect, error) {
	cid := C.CString("#" + id)
	defer C.free(unsafe.Pointer(cid))
	var gerr *C.GError
	var ink, logical C.RsvgRectangle
	vp := viewport.c()
	if C.rsvg_handle_get_geometry_for_layer(d.h, cid, &vp, &ink, &logical, &gerr) == 0 {
		return Rect{}, takeError(gerr)
	}
	return Rect{float64(ink.x), float64(ink.y), float64(ink.width), float64(ink.height)}, nil
}

// RenderTinted draws only the element with this id, in a solid colour.
func (d *Doc) RenderTinted(cr *cairo.Context, id string, viewport Rect, r, g, b float64) error {
	cid := C.CString("#" + id)
	defer C.free(unsafe.Pointer(cid))
	var gerr *C.GError
	vp := viewport.c()
	if C.lp_render_tinted(d.h, ctx(cr), cid, &vp, C.double(r), C.double(g), C.double(b), &gerr) == 0 {
		return takeError(gerr)
	}
	return nil
}

func (r Rect) c() C.RsvgRectangle {
	return C.RsvgRectangle{x: C.double(r.X), y: C.double(r.Y), width: C.double(r.W), height: C.double(r.H)}
}

func ctx(cr *cairo.Context) *C.cairo_t {
	// The cairo_t lives in C memory, so holding it as a uintptr is safe;
	// converting through a variable keeps go vet quiet about that.
	p := cr.Native()
	return *(**C.cairo_t)(unsafe.Pointer(&p))
}

func takeError(gerr *C.GError) error {
	if gerr == nil {
		return errors.New("librsvg: unknown error")
	}
	defer C.g_error_free(gerr)
	return errors.New("librsvg: " + C.GoString(gerr.message))
}
