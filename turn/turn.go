// Package turn shows a GTK widget turned a quarter turn. GTK draws it so,
// and clicks and touches land where they look to be; what opens in a
// window of its own (a popover, a tooltip) is not turned with it.
package turn

/*
#cgo pkg-config: gtk4
#include <gtk/gtk.h>

// LpTurn holds one child, laid out turned: it is as wide as LpTurn is
// tall, and as tall as it is wide.
typedef struct {
	GtkWidget parent;
	GtkWidget *child;
	gboolean left; // anticlockwise, or else clockwise
} LpTurn;

typedef struct {
	GtkWidgetClass parent;
} LpTurnClass;

G_DEFINE_TYPE(LpTurn, lp_turn, GTK_TYPE_WIDGET)

static GtkOrientation across(GtkOrientation o) {
	return o == GTK_ORIENTATION_HORIZONTAL ? GTK_ORIENTATION_VERTICAL : GTK_ORIENTATION_HORIZONTAL;
}

static void lp_turn_init(LpTurn *t) {}

static void lp_turn_dispose(GObject *o) {
	LpTurn *t = (LpTurn *)o;
	g_clear_pointer(&t->child, gtk_widget_unparent);
	G_OBJECT_CLASS(lp_turn_parent_class)->dispose(o);
}

static GtkSizeRequestMode lp_turn_request_mode(GtkWidget *w) {
	switch (gtk_widget_get_request_mode(((LpTurn *)w)->child)) {
	case GTK_SIZE_REQUEST_HEIGHT_FOR_WIDTH:
		return GTK_SIZE_REQUEST_WIDTH_FOR_HEIGHT;
	case GTK_SIZE_REQUEST_WIDTH_FOR_HEIGHT:
		return GTK_SIZE_REQUEST_HEIGHT_FOR_WIDTH;
	default:
		return GTK_SIZE_REQUEST_CONSTANT_SIZE;
	}
}

static void lp_turn_measure(GtkWidget *w, GtkOrientation o, int for_size,
                            int *min, int *nat, int *min_base, int *nat_base) {
	gtk_widget_measure(((LpTurn *)w)->child, across(o), for_size, min, nat, NULL, NULL);
}

static void lp_turn_compute_expand(GtkWidget *w, gboolean *hexpand, gboolean *vexpand) {
	GtkWidget *child = ((LpTurn *)w)->child;
	*hexpand = gtk_widget_compute_expand(child, GTK_ORIENTATION_VERTICAL);
	*vexpand = gtk_widget_compute_expand(child, GTK_ORIENTATION_HORIZONTAL);
}

static void lp_turn_size_allocate(GtkWidget *w, int width, int height, int baseline) {
	LpTurn *t = (LpTurn *)w;
	// The child's top left corner goes to the bottom left (anticlockwise)
	// or the top right (clockwise).
	GskTransform *tr = t->left
		? gsk_transform_rotate(gsk_transform_translate(NULL, &GRAPHENE_POINT_INIT(0, height)), -90)
		: gsk_transform_rotate(gsk_transform_translate(NULL, &GRAPHENE_POINT_INIT(width, 0)), 90);
	gtk_widget_allocate(t->child, height, width, -1, tr);
}

static void lp_turn_class_init(LpTurnClass *c) {
	G_OBJECT_CLASS(c)->dispose = lp_turn_dispose;
	GtkWidgetClass *wc = GTK_WIDGET_CLASS(c);
	wc->get_request_mode = lp_turn_request_mode;
	wc->measure = lp_turn_measure;
	wc->compute_expand = lp_turn_compute_expand;
	wc->size_allocate = lp_turn_size_allocate;
	gtk_widget_class_set_css_name(wc, "turn");
}

static GtkWidget *lp_turn_new(GtkWidget *child, gboolean left) {
	LpTurn *t = g_object_new(lp_turn_get_type(), NULL);
	t->child = child;
	t->left = left;
	gtk_widget_set_parent(child, GTK_WIDGET(t));
	return GTK_WIDGET(t);
}
*/
import "C"

import (
	"runtime"
	"unsafe"

	glib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// New shows child, which must have no parent, turned a quarter turn to the
// left (anticlockwise) or else to the right.
func New(child gtk.Widgetter, left bool) gtk.Widgetter {
	p := glib.BaseObject(child).Native()
	c := *(**C.GtkWidget)(unsafe.Pointer(&p))
	l := C.gboolean(0)
	if left {
		l = 1
	}
	t := C.lp_turn_new(c, l)
	runtime.KeepAlive(child)
	return glib.Take(unsafe.Pointer(t)).WalkCast(func(o glib.Objector) bool {
		_, ok := o.(gtk.Widgetter)
		return ok
	}).(gtk.Widgetter)
}
