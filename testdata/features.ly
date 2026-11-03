%% A small piece that exercises the tricky cases:
%% pickup bar, chords, ties (single and chord), tuplet, grace note, pedal,
%% two voices on one staff, tempo and time signature changes.

\version "2.26.0"

\header { title = "lilypractice test piece" }

right = \relative c'' {
  \clef treble \key g \major \time 3/4 \tempo 4 = 90
  \partial 4 d4 |
  g,8 a b c d4 ~ | d4 <b d>2 ~ | <b d>4 \tuplet 3/2 { c8 b a } g4 |
  \grace { a16 } g2. |
  \time 2/4 \tempo 4 = 120
  << { e'4 d } \\ { c4 b } >> |
  c2 \bar "|."
}

left = \relative c' {
  \clef bass \key g \major \time 3/4
  \partial 4 r4 |
  <g b>2.\sustainOn | g,2\sustainOff\sustainOn a4 | b2\sustainOff c4 | d2. |
  \time 2/4
  e4 fis | g2 |
}

\score {
  \new PianoStaff <<
    \new Staff = "right" \right
    \new Staff = "left" \left
  >>
  \layout { }
  \midi { }
}
