package voice

import (
	"encoding/json"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
)

// choiceState owns both the random source and the last-pick map. Forked
// contexts share it, so a voice id has one choice throughout a process.
//
// The history file is read once, by the first pick that needs it, so a run
// that speaks nothing in a persona (version, help, any piped command) never
// touches voice.json. loaded is true once that read has happened.
type choiceState struct {
	mu     sync.Mutex
	path   string
	rand   *rand.Rand
	loaded bool
	last   map[string]map[string]int
	picked map[string]map[string]int
	dirty  bool
}

type choiceKey struct{}

func choices(ctx *app.Context) *choiceState {
	return app.OncePerProcess(ctx, choiceKey{}, func() *choiceState {
		s := &choiceState{
			path:   filepath.Join(ctx.CacheDir, "voice.json"),
			rand:   ctx.Rand,
			last:   map[string]map[string]int{},
			picked: map[string]map[string]int{},
		}
		return s
	})
}

// load reads the history file the first time it is called. A missing or
// unreadable file is an empty history. The caller holds mu.
func (s *choiceState) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	if json.Unmarshal(data, &s.last) != nil || s.last == nil {
		s.last = map[string]map[string]int{}
	}
}

type rotatingSpeaker struct {
	voice   *persona.Voice
	choices *choiceState
}

func (s rotatingSpeaker) Name() string { return s.voice.Name }

func (s rotatingSpeaker) Line(id string) (string, language.Tag, bool) {
	lines := s.voice.Lines[id]
	if len(lines) == 0 {
		return "", language.Und, false
	}
	choice, ok := s.choices.pick(s.voice.ID, id, len(lines), func(i int) bool {
		return msg.CheckLine(id, lines[i].Text) == nil
	})
	if !ok {
		return "", language.Und, false
	}
	line := lines[choice]
	return line.Text, line.Lang, true
}

// pick chooses which of a persona's n variants of id this process says, and
// remembers the choice (in memory for the run, in the history for the next).
//
// Only variants the renderer accepts (usable) are candidates, so a variant
// msg refuses is never picked and never counts as used: the history holds
// what was said. When none is usable there is no pick and nothing is
// recorded; the caller falls back to the catalog.
func (s *choiceState) pick(personaID, id string, n int, usable func(i int) bool) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if byID := s.picked[personaID]; byID != nil {
		if i, ok := byID[id]; ok {
			return i, true
		}
	}
	candidates := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if usable(i) {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return 0, false
	}
	s.load()
	// Draw from the candidates without the last pick, when there is another
	// to draw. The random draw stays under this mutex because
	// app.Context.Rand is not concurrent.
	if last, ok := s.last[personaID][id]; ok && len(candidates) > 1 {
		kept := make([]int, 0, len(candidates))
		for _, c := range candidates {
			if c != last {
				kept = append(kept, c)
			}
		}
		candidates = kept
	}
	i := candidates[0]
	if len(candidates) > 1 {
		i = candidates[s.random(len(candidates))]
	}
	if s.picked[personaID] == nil {
		s.picked[personaID] = map[string]int{}
	}
	s.picked[personaID][id] = i
	if s.last[personaID] == nil {
		s.last[personaID] = map[string]int{}
	}
	s.last[personaID][id] = i
	s.dirty = true
	return i, true
}

// random is set by choices for the context's seeded source.
// It is called only while mu is held.
func (s *choiceState) random(n int) int { return s.rand.IntN(n) }

// SaveChoices writes the choice history once at CLI exit. Failure only loses
// repetition avoidance for the next process; speaking still succeeds.
func SaveChoices(ctx *app.Context) {
	s := choices(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return
	}
	data, err := json.Marshal(s.last)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".voice-*")
	if err != nil {
		return
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		if os.Rename(f.Name(), s.path) == nil {
			s.dirty = false
		}
	}
}

type namedOnly struct{ name string }

func (s namedOnly) Name() string { return s.name }
func (namedOnly) Line(string) (string, language.Tag, bool) {
	return "", language.Und, false
}

// stableSpeaker chooses from the voice and id plus a content key, without
// changing the run's random choice or the persisted history.
type stableSpeaker struct {
	voice *persona.Voice
	key   string
}

func (s stableSpeaker) Name() string { return s.voice.Name }

func (s stableSpeaker) Line(id string) (string, language.Tag, bool) {
	lines := s.voice.Lines[id]
	if len(lines) == 0 {
		return "", language.Und, false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(s.voice.ID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(id))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(s.key))
	line := lines[int(h.Sum32()%uint32(len(lines)))]
	return line.Text, line.Lang, true
}
