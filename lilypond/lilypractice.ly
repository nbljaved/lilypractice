%% lilypractice.ly -- LilyPond side of lilypractice.
%%
%% Load it without touching the score:
%%   lilypond -dbackend=svg -dinclude-settings=/path/to/lilypractice.ly piece.ly
%%
%% It does two things:
%%  1. gives every note head an SVG id ("n1", "n2", ...; in a file of several
%%     scores "s2n1", "s2n2", ... for the second) and the class "lp-note";
%%  2. writes the notes, ties, bars, tempos, time signatures, pedals and
%%     repeats to a JSON file:
%%     $LILYPRACTICE_JSON if set, otherwise <output-name>.notes.json.
%%
%% Musical times are written as [numerator, denominator] in whole notes.
%% A moment is [main, grace]; the grace part is negative for grace notes.

\version "2.26.0"

#(define lp-output-name (ly:parser-output-name))
#(define lp-score-count 0) % scores written out so far
#(define lp-staff-count 0)

%% Lines of music (systems) are numbered in the order they are met; note
%% heads and staves carry their system's number as data-system in the SVG.
#(define lp-systems '())
#(define (lp-system-index grob)
   (let ((system (ly:grob-system grob)))
     (if (not (ly:grob? system))
         -1
         (let ((hit (assq system lp-systems)))
           (if hit
               (cdr hit)
               (let ((i (length lp-systems)))
                 (set! lp-systems (cons (cons system i) lp-systems))
                 i))))))

#(define (lp-main m)
   ;; Some properties are moments, some (since 2.25) plain rationals.
   (if (ly:moment? m) (ly:moment-main m) m))

#(define (lp-rational r)
   (let ((r (lp-main r)))
     (format #f "[~a,~a]" (numerator r) (denominator r))))

#(define (lp-moment m)
   (format #f "[~a,~a]"
           (lp-rational (ly:moment-main m))
           (lp-rational (ly:moment-grace m))))

#(define (lp-string s)
   (let loop ((chars (string->list s)) (out '()))
     (cond ((null? chars) (string-append "\"" (list->string (reverse out)) "\""))
           ((memv (car chars) '(#\" #\\))
            (loop (cdr chars) (cons (car chars) (cons #\\ out))))
           ((char<? (car chars) #\space) (loop (cdr chars) out))
           (else (loop (cdr chars) (cons (car chars) out))))))

#(define (lp-join strings)
   (string-join strings ",\n  "))

#(define (lp-json-path score)
   (let ((base (or (getenv "LILYPRACTICE_JSON")
                   (string-append lp-output-name ".notes.json"))))
     (if (= score 1)
         base
         ;; several \score blocks: piece.notes.json, piece.notes-2.json, ...
         (let ((dot (string-rindex base #\.)))
           (string-append (substring base 0 dot)
                          "-" (number->string score)
                          (substring base dot))))))

#(define (Lp_notes_engraver context)
   ;; The score's number is given out at the end, and only if it has
   ;; staves: a chord drawn in a markup with \score has none, and is no
   ;; music to practise.
   (let ((score #f)
         (staffed #f)
         (notes '())        ; newest first
         (head-ids (make-hash-table)) ; note head grob -> its number
         (tie-grobs '())
         (staves '())       ; ((staff-context index name) ...)
         (groups '())
         (voices '())
         (bars '())
         (tempos '())
         (time-sigs '())
         (pedals '())
         (repeats '())
         (tremolos '())     ; two-note tremolos: where they start and end
         (tremolo-start #f)
         (last-bar #f)
         (last-tempo #f)
         (last-time-sig #f)
         (count 0))

     (define (pedal! name event)
       ;; A pedal goes down (START) or comes up; a pedal change is an up
       ;; and a down at the same moment, in that order.
       (set! pedals
             (cons (format #f "{\"at\":~a,\"pedal\":~a,\"down\":~a}"
                           (lp-moment (ly:context-current-moment context))
                           (lp-string name)
                           (if (eqv? (ly:event-property event 'span-direction) START)
                               "true" "false"))
                   pedals)))

     (define (repeat! kind event)
       ;; The structure of repeats: where they start and end, how often
       ;; their end goes back, and which pass each alternative ending is
       ;; for; segno, coda, D.S. and Fine from \repeat segno too. Each
       ;; staff's music may repeat on its own, so these come once per staff.
       (let ((number (lambda (key)
                       (let ((v (ly:event-property event key #f)))
                         (if (number? v) v "null"))))
             (numbers (ly:event-property event 'volta-numbers '())))
         (set! repeats
               (cons (format #f "{\"at\":~a,\"kind\":~a,\"count\":~a,\"returns\":~a,\"dir\":~a,\"numbers\":[~a]}"
                             (lp-moment (ly:context-current-moment context))
                             (lp-string kind)
                             (number 'repeat-count)
                             (number 'return-count)
                             (number 'alternative-dir)
                             (string-join (map number->string
                                               (filter integer? (if (list? numbers) numbers '())))
                                          ","))
                     repeats))))

     (define (index-of ctx table set-table! name-of)
       ;; Number contexts in order of first appearance.
       (let ((hit (assq ctx table)))
         (if hit
             (cadr hit)
             (let ((i (length table)))
               (set-table! (cons (list ctx i (name-of ctx)) table))
               i))))

     (define (group-of staff)
       ;; The kind of group a staff is in, such as PianoStaff, and which
       ;; one, to tell a piano's two staves from a singer's above them.
       (let ((parent (ly:context-parent staff)))
         (if (and parent (not (eq? 'Score (ly:context-name parent))))
             (format #f "~a~a" (ly:context-name parent)
                     (index-of parent groups (lambda (t) (set! groups t)) ly:context-id))
             "")))

     (define (staff-index ctx)
       (index-of ctx staves (lambda (t) (set! staves t))
                 (lambda (c) (cons (ly:context-id c) (group-of c)))))

     (define (voice-index ctx)
       (index-of ctx voices (lambda (t) (set! voices t)) ly:context-id))

     (define (tremolo-of head)
       ;; A one-note tremolo (\repeat tremolo 4 c8, or c2:8) is struck
       ;; over and over: its stem's strokes say how fast, one stroke for
       ;; eighths.
       (let* ((stem (ly:grob-object head 'stem))
              (flag (and (ly:grob? stem) (ly:grob-object stem 'tremolo-flag)))
              (strokes (and (ly:grob? flag) (ly:grob-property flag 'flag-count #f))))
         (if (and (integer? strokes) (> strokes 0))
             (format #f "[1,~a]" (expt 2 (+ strokes 2)))
             "null")))

     (define (note-id k)
       (if (= score 1)
           (format #f "n~a" k)
           (format #f "s~an~a" score k)))

     ;; Not read from output-attributes: that would run its callback now,
     ;; before line breaking, and fix the note's line of music at -1.
     (define (head-id head)
       (let ((k (and (ly:grob? head) (hashq-ref head-ids head #f))))
         (and k (note-id k))))

     (define (write-json)
       (let ((path (lp-json-path score))
             (named (lambda (table)
                      (map (lambda (entry)
                             (let ((name (caddr entry)))
                               (if (pair? name)
                                   (format #f "{\"index\":~a,\"name\":~a,\"group\":~a}"
                                           (cadr entry) (lp-string (car name)) (lp-string (cdr name)))
                                   (format #f "{\"index\":~a,\"name\":~a}"
                                           (cadr entry) (lp-string name)))))
                           (reverse table)))))
         (call-with-output-file path
           (lambda (port)
             (format port "{\n\"version\": 1,\n")
             (format port "\"staves\": [\n  ~a],\n" (lp-join (named staves)))
             (format port "\"voices\": [\n  ~a],\n" (lp-join (named voices)))
             (format port "\"notes\": [\n  ~a],\n"
                     (lp-join (map (lambda (n) (if (procedure? n) (n) n)) (reverse notes))))
             (format port "\"ties\": [\n  ~a],\n"
                     (lp-join
                      (filter-map
                       (lambda (tie)
                         (let ((from (head-id (ly:spanner-bound tie LEFT)))
                               (to (head-id (ly:spanner-bound tie RIGHT))))
                           (and from to
                                (format #f "[~a,~a]" (lp-string from) (lp-string to)))))
                       (reverse tie-grobs))))
             (format port "\"bars\": [\n  ~a],\n" (lp-join (reverse bars)))
             (format port "\"tempos\": [\n  ~a],\n" (lp-join (reverse tempos)))
             (format port "\"pedals\": [\n  ~a],\n" (lp-join (reverse pedals)))
             (format port "\"repeats\": [\n  ~a],\n" (lp-join (reverse repeats)))
             (format port "\"tremolos\": [\n  ~a],\n" (lp-join (reverse tremolos)))
             (format port "\"timeSignatures\": [\n  ~a]\n}\n"
                     (lp-join (reverse time-sigs)))))
         (ly:message "lilypractice: ~a notes written to ~a" count path)))

     (make-engraver
      ((process-music engraver)
       (let ((now (ly:context-current-moment context))
             (bar (ly:context-property context 'currentBarNumber #f))
             (position (ly:context-property context 'measurePosition #f))
             (tempo (ly:context-property context 'tempoWholesPerMinute #f))
             (time-sig (ly:context-property context 'timeSignature #f))
             (measure-length (ly:context-property context 'measureLength #f)))
         (when (and bar (not (equal? bar last-bar)))
           (set! last-bar bar)
           (set! bars (cons (format #f "{\"at\":~a,\"number\":~a,\"position\":~a}"
                                    (lp-moment now) bar (lp-rational position))
                            bars)))
         (when (and tempo (not (equal? tempo last-tempo)))
           (set! last-tempo tempo)
           (set! tempos (cons (format #f "{\"at\":~a,\"wholesPerMinute\":~a}"
                                      (lp-moment now) (lp-rational tempo))
                              tempos)))
         (when (and measure-length
                    (not (equal? (cons time-sig measure-length) last-time-sig)))
           (set! last-time-sig (cons time-sig measure-length))
           ;; Compound meters are not a simple pair; their numerator and
           ;; denominator are null, and the measure length still applies.
           (let ((simple? (and (pair? time-sig)
                               (number? (car time-sig))
                               (number? (cdr time-sig)))))
             (set! time-sigs
                   (cons (format #f "{\"at\":~a,\"numerator\":~a,\"denominator\":~a,\"measureLength\":~a}"
                                 (lp-moment now)
                                 (if simple? (car time-sig) "null")
                                 (if simple? (cdr time-sig) "null")
                                 (lp-rational measure-length))
                         time-sigs))))))

      (listeners
       ((sustain-event engraver event) (pedal! "sustain" event))
       ((sostenuto-event engraver event) (pedal! "sostenuto" event))
       ((una-corda-event engraver event) (pedal! "una-corda" event))
       ((tremolo-span-event engraver event)
        ;; \repeat tremolo 4 { c16 e }: the two notes alternate from the
        ;; start to the stop, at the speed the start gives. When one
        ;; tremolo follows another, the next may start before the last
        ;; stops.
        (let ((now (ly:context-current-moment context)))
          (define (close!)
            (when (and tremolo-start (ly:moment<? (car tremolo-start) now))
              (set! tremolos
                    (cons (format #f "{\"from\":~a,\"to\":~a,\"interval\":[1,~a]}"
                                  (lp-moment (car tremolo-start)) (lp-moment now)
                                  (cdr tremolo-start))
                          tremolos))
              (set! tremolo-start #f)))
          (close!)
          (when (eqv? (ly:event-property event 'span-direction) START)
            (set! tremolo-start (cons now (ly:event-property event 'tremolo-type 16))))))
       ((volta-repeat-start-event engraver event) (repeat! "start" event))
       ((volta-repeat-end-event engraver event) (repeat! "end" event))
       ((alternative-event engraver event) (repeat! "alternative" event))
       ((segno-mark-event engraver event) (repeat! "segno" event))
       ((coda-mark-event engraver event) (repeat! "coda" event))
       ((dal-segno-event engraver event) (repeat! "dal-segno" event))
       ((fine-event engraver event) (repeat! "fine" event)))

      (acknowledgers
       ((note-head-interface engraver grob source-engraver)
        (let* ((event (ly:grob-property grob 'cause))
               (written (and (ly:stream-event? event)
                             (ly:event-property event 'pitch #f)))
               ;; A transposing instrument's notes sound other than written.
               (transposition (ly:context-property (ly:translator-context source-engraver)
                                                   'instrumentTransposition #f))
               (pitch (if (and written (ly:pitch? transposition))
                          (ly:pitch-transpose written transposition)
                          written))
               (duration (and pitch (ly:event-property event 'duration #f))))
          ;; Drum and other unpitched heads are skipped.
          (when (and pitch duration)
            (set! count (1+ count))
            (let* ((k count)
                   ;; An invisible note (transparent, \hideNotes, or no
                   ;; stencil) still sounds, but nobody can read it.
                   (hidden (or (eq? #t (ly:grob-property-data grob 'transparent))
                               (not (ly:grob-property-data grob 'stencil))))
                   (voice (ly:translator-context source-engraver))
                   (staff (ly:context-find voice 'Staff)))
              (hashq-set! head-ids grob k)
              ;; A callback, because which line of music (system) the note
              ;; ends up on is only known after line breaking.
              (ly:grob-set-property! grob 'output-attributes
                                     (let ((own (ly:grob-property grob 'output-attributes '())))
                                       (lambda (g)
                                         (if score
                                             (append `((id . ,(note-id k)) (class . "lp-note")
                                                       (data-system . ,(lp-system-index g)))
                                                     own)
                                             own))))
              (set! notes
                    (cons (let ((entry (format #f "\"at\":~a,\"duration\":~a,\"pitch\":~a,\"staff\":~a,\"voice\":~a,\"bar\":~a,\"hidden\":~a"
                                               (lp-moment (ly:context-current-moment context))
                                               (lp-rational (ly:duration->moment duration))
                                               (+ 60 (ly:pitch-semitones pitch))
                                               (if staff (staff-index staff) -1)
                                               (voice-index voice)
                                               (or last-bar 0)
                                               (if hidden "true" "false"))))
                            ;; The id and tremolo are known when the notes
                            ;; are written: by then a chord's stem is
                            ;; complete.
                            (lambda ()
                              (format #f "{\"id\":~a,~a,\"tremolo\":~a}"
                                      (lp-string (note-id k)) entry (tremolo-of grob))))
                          notes))))))
       ((tie-interface engraver grob source-engraver)
        ;; Only ties from one note to another: a repeat tie or a laissez
        ;; vibrer tie hangs off a single note, and is no spanner.
        (when (ly:spanner? grob)
          (set! tie-grobs (cons grob tie-grobs))))
       ((staff-symbol-interface engraver grob source-engraver)
        ;; Staves are numbered as they begin, top to bottom, rather than
        ;; by which has the first note.
        (set! staffed #t)
        (staff-index (ly:translator-context source-engraver))
        ;; Each line of music gets its own piece of the staff, so the
        ;; callback runs once per piece and gives each its own id.
        (ly:grob-set-property! grob 'output-attributes
                               (lambda (g)
                                 (set! lp-staff-count (1+ lp-staff-count))
                                 `((id . ,(format #f "staff~a" lp-staff-count))
                                   (class . "lp-staff")
                                   (data-system . ,(lp-system-index g)))))))

      ((finalize engraver)
       (when staffed
         (set! lp-score-count (1+ lp-score-count))
         (set! score lp-score-count)
         (write-json))))))

\layout {
  \context {
    \Score
    \consists #Lp_notes_engraver
  }
}
