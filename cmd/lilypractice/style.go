package main

import (
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// The look follows Sightread: dark bars around a white score, violet for
// what is active.
const css = `
.score-area { background-color: #232329; }

.sidebar { background-color: #18181d; }
.sidebar list { background: transparent; }
.sidebar-head {
	padding: 9px 12px;
	color: #a1a1aa;
	font-weight: bold;
	border-bottom: 1px solid #26262e;
}
.sidebar row { padding: 6px 12px; border-radius: 6px; margin: 1px 4px; }
.sidebar row.chosen { background: #6d28d9; color: white; }
.sidebar row.chosen .subtitle { color: #ddd6fe; }
.message-title { font-size: 18px; font-weight: bold; color: #e4e4e7; }

.transport {
	background-color: #141419;
	border-top: 1px solid #26262e;
	padding: 8px 14px;
}
.transport label { color: #e4e4e7; }

button.play-button {
	min-width: 44px;
	min-height: 44px;
	padding: 0;
	border-radius: 9999px;
	background: #7c3aed;
	color: white;
	box-shadow: 0 0 12px rgba(139, 92, 246, 0.35);
}
button.play-button:hover { background: #8b5cf6; }

button.round { border-radius: 9999px; min-width: 34px; min-height: 34px; padding: 0; }

.position { font-family: monospace; font-weight: bold; font-size: 15px; }

.pedal-indicator {
	padding: 2px 9px;
	border-radius: 9999px;
	border: 1px solid #3f3f46;
	color: #71717a;
	font-size: 11px;
	font-weight: bold;
}
.pedal-indicator.on { background: #6d28d9; border-color: #8b5cf6; color: white; }
.state { color: #a1a1aa; font-size: 11px; }

.segmented button { padding: 4px 12px; }
.segmented button:checked,
button.toggle.pill:checked,
menubutton.on > button {
	background: #6d28d9;
	color: white;
}

.tempo { font-family: monospace; font-weight: bold; min-width: 112px; }
.caption { color: #a1a1aa; font-size: 10px; }

.chip > button { border-radius: 9999px; padding: 2px 12px; font-weight: bold; }

.menu { padding: 4px; min-width: 200px; }
.menu separator { margin: 4px 0; }
button.menu-item { padding: 5px 12px; font-weight: normal; border-radius: 6px; }

.panel { padding: 12px; }
.section-title { font-weight: bold; color: #c4b5fd; margin-top: 10px; }
.subtitle { color: #a1a1aa; font-size: 11px; }
.key-name { font-family: monospace; font-weight: bold; color: #e4e4e7; }

.stat-tile { padding: 8px 14px; border-radius: 8px; background: rgba(0, 0, 0, 0.35); }
.stat-number { font-size: 22px; font-weight: bold; }
.stat-name { font-size: 10px; font-weight: bold; }
.big-number { font-size: 30px; font-weight: bold; }
.perfect { color: #4ade80; }
.good { color: #60a5fa; }
.missed { color: #fbbf24; }
.wrong { color: #f87171; }
`

func applyStyle() {
	gtk.SettingsGetDefault().SetObjectProperty("gtk-application-prefer-dark-theme", true)
	provider := gtk.NewCSSProvider()
	provider.LoadFromString(css)
	gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), provider, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
}
