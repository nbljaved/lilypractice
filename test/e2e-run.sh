#!/bin/sh
# The scenarios behind test/e2e.sh; run inside xvfb-run.
# Usage: e2e-run.sh PROJECTDIR OUTDIR
proj=$1 out=$2
piece=$proj/testdata/features.ly
app=$proj/out/bin/lilypractice
sim=$proj/out/bin/lpsim
failed=0
# A client name of our own, so a lilypractice you are running is left alone.
client=lp-e2e-$$
# Your saved settings (e.g. a smaller keyboard) must not change the results.
export XDG_CONFIG_HOME=$out/config
rm -rf "$out" && mkdir -p "$out"
cd "$out" || exit 1

# The left hand of features.ly, as lilypractice should play it.
left="55 59 43 45 47 48 50 52 54 55"

check() { # name, got, want
	if [ "$2" = "$3" ]; then
		echo "ok    $1"
	else
		echo "FAIL  $1: got '$2', want '$3'"
		failed=1
	fi
}

start() { # name, extra flags...
	name=$1
	shift
	"$app" -name "$client" "$@" "$piece" 2>"$name.log" &
	pid=$!
	# The title is the folder's until the piece has loaded.
	xdotool search --sync --name "^features " >/dev/null
}

press() {
	xdotool search --onlyvisible --name lilypractice windowfocus --sync key "$@" 2>/dev/null
}

notes() { # file, port: the notes switched on at lilypractice:port
	grep 'Note on' "$1" | awk -v p=":$2\$" '$1 ~ p {printf "%s%s", sep, $6+0; sep=" "}'
}

quit() { # sets status to the program's exit status
	press ctrl+q
	wait $pid
	status=$?
}

# The metronome is on FluidSynth even with -synth none, so every run
# gives it -audio file: no sound, only fluidsynth.wav here.
# (FluidSynth shows its options as "name value", so pgrep matches
# "id NAME-".)

# 1. A simulated MIDI keyboard plays the right hand; the program waits for
#    each note and plays the left hand.
start midi -synth none -audio file
aseqdump -p "$client:1" >midi.sent 2>&1 &
dump=$!
xdotool sleep 1
press space
"$sim" -hand right -delay 2100ms -to "$client:0" "$piece" >/dev/null
xdotool sleep 1
kill $dump
check "MIDI keyboard: left hand follows the right" "$(notes midi.sent 1)" "$left"
# Pedal down, changed and up, on both staves' channels.
check "MIDI keyboard: pedal marks are played" "$(grep -c "controller 64" midi.sent)" 8
check "metronome: FluidSynth runs with -synth none" "$(pgrep -c -f "alsa_seq.id $client-")" 1
quit
check "MIDI keyboard: program exits cleanly" "$status" 0

# 2. The same, played on the computer keyboard (bottom row: z=C ... m=B).
start keys -synth none -audio file
aseqdump -p "$client:1,$client:3" >keys.sent 2>&1 &
dump=$!
xdotool sleep 1
press space
xdotool sleep 2.2
press --delay 2300 x b n m z x m x z m n b n b c z x m z
xdotool sleep 1
kill $dump
check "computer keys: left hand follows the right" "$(notes keys.sent 1)" "$left"
check "computer keys: 19 notes sounded on keys-out" "$(notes keys.sent 3 | wc -w)" 19
quit
check "computer keys: program exits cleanly" "$status" 0

# 3. The program starts its own FluidSynth (writing fluidsynth.wav here),
#    which makes sound and stops with the program. (The runs above wrote
#    fluidsynth.wav too: their metronome's.)
rm -f fluidsynth.wav
start synth -audio file
xdotool sleep 2
press space
"$sim" -hand right -delay 2100ms -to "$client:0" "$piece" >/dev/null
check "FluidSynth: running while the program runs" "$(pgrep -c -f "alsa_seq.id $client-")" 1
quit
check "FluidSynth: program exits cleanly" "$status" 0
xdotool sleep 0.5
check "FluidSynth: stopped with the program" "$(pgrep -c -f "alsa_seq.id $client-")" 0
peak=$(od -A n -t d2 -j 44 fluidsynth.wav | tr -s ' ' '\n' | tr -d - | sort -n | tail -1)
check "FluidSynth: the recording has sound" "$([ "${peak:-0}" -gt 1000 ] && echo yes)" yes

# 4. Commands from the MIDI keyboard, as the default commands.json has
#    them: A0+A#0 takes command mode on, D4 twice moves two bars on (it
#    stays in command mode) and C4 plays from there; the sustain pedal
#    stops, A3 goes back to the start. With -thru, only A0 sounds: until
#    A#0 joins it, it is music.
keys() { # keys held together, as hex note numbers
	for n; do aseqsend -p "$client:0" "90 $n 40"; done
	xdotool sleep 0.1
	for n; do aseqsend -p "$client:0" "80 $n 00"; done
	xdotool sleep 0.3
}
pedal() {
	aseqsend -p "$client:0" "B0 40 7F"
	xdotool sleep 0.1
	aseqsend -p "$client:0" "B0 40 00"
	xdotool sleep 0.3
}
start commands -synth none -audio file -mode listen -thru
aseqdump -p "$client:1,$client:3" >commands1.sent 2>&1 &
dump=$!
xdotool sleep 1
keys 15 16; keys 3E; keys 3E; keys 3C # A0+A#0 D4 D4 C4
xdotool sleep 3                       # a bar of count-in (2 s), then the music
keys 15 16; pedal                     # A0+A#0 Sustain: stop
kill $dump
aseqdump -p "$client:1,$client:3" >commands2.sent 2>&1 &
dump=$!
xdotool sleep 1
keys 15 16; keys 39; keys 15 16; keys 3C # A0+A#0 A3, A0+A#0 C4
xdotool sleep 3
keys 15 16; keys 3C
kill $dump
check "command keys: the default file is written" "$(ls config/lilypractice/commands.json 2>&1)" config/lilypractice/commands.json
check "command keys: two bars on, then play (B2 first)" "$(notes commands1.sent 1 | cut -d' ' -f1)" 47
check "command keys: back to the start (D5 first)" "$(notes commands2.sent 1 | cut -d' ' -f1)" 74
check "command keys: only A0 of A0+A#0 sounds" "$(notes commands1.sent 3) $(notes commands2.sent 3)" "21 21 21 21 21"
check "command keys: the pedal in command mode is not passed on" "$(grep "controller 64" commands1.sent | awk '$1 ~ /:3$/' | wc -l)" 0
quit
check "command keys: program exits cleanly" "$status" 0

exit $failed
