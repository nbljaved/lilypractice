%% Repeats for lilypractice to follow: a volta repeat with two endings, one
%% played three times, and a D.S. al Fine. The MIDI plays them unfolded,
%% so lpcheck can compare lilypractice's order with LilyPond's.

\version "2.26.0"

\header { title = "lilypractice repeats test" }

music = \relative c' {
  \time 4/4
  c1 |                                % 0
  \repeat volta 2 { d1 | e1 | }       % 1-3
  \alternative { { f1 | } { g1 | } }  % 3, 4
  a1 |                                % 5
  \repeat volta 3 { b1 | }            % 6
  c1 |                                % 7
  \repeat segno 2 {
    d1 |                              % 8
    \volta 2 \fine
    e1 |                              % 9
  }
}

\score {
  \new Staff \music
  \layout { }
}

\score {
  \unfoldRepeats \new Staff \music
  \midi { }
}
