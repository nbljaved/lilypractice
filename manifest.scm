;; Development environment for lilypractice.
;; Usage: guix shell -m manifest.scm
(specifications->manifest
 '("lilypond"
   "go"
   "gcc-toolchain"
   "pkg-config"
   "gtk"            ; GTK 4
   "adwaita-icon-theme" ; icons for play, pause, settings, ...
   "gobject-introspection" ; needed by gotk4
   "librsvg"
   "xz"             ; pkg-config needs liblzma.pc for librsvg
   "cairo"
   "alsa-lib"
   "alsa-utils"     ; aconnect, aseqdump, aplaymidi (simulated MIDI input)
   "fluidsynth"     ; the default instrument
   "fluid-3"        ; its soundfont
   "coreutils"
   "bash"))
