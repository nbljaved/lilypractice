%% Tremolos, for the tests: two-note tremolos over another staff's notes
%% of the same length, one of them with beams of its own, and one-note
%% tremolos on eighths, whose flag counts as one of the strokes.

\version "2.26.0"

\new PianoStaff <<
  \new Staff { \time 2/4 \repeat tremolo 2 { c''16 e'' } \repeat tremolo 2 { c''16[ e''] } | c''8:16 c''8:32 c''4 | }
  \new Staff { \clef bass \time 2/4 c8 c c c | c2 | }
>>
