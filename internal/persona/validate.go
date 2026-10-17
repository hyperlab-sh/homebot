package persona

import (
	"errors"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/language"
	"golang.org/x/text/width"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var placeholder = regexp.MustCompile(`\{([a-z][a-z0-9_]*)(?::[^{}]*)?\}`)
var positionPrefix = regexp.MustCompile(`^\{[a-z][a-z0-9_]*\}: `)

// factToken finds the tokens W7 looks for (plan S5, M13): a digit, /, ~, --
// or a word that starts with - and a letter, homebot, http, www., a backtick,
// $, =, @ and a file extension such as .yml. factTokens drops the matches that
// are not tokens.
var factToken = regexp.MustCompile("(?i)(?:--[a-z][a-z-]*|-[a-z][a-z-]*|https?://[^\\s]+|www\\.[^\\s]+|[\\w./~@=\\-]*[0-9][\\w./~@=\\-]*|[\\w-]+\\.(?:yml|toml|age|txt)|homebot|[/~=$@`])")

// factTokens are the tokens of s that W7 compares. A dash that follows a letter
// or a digit is the hyphen of a word (well-known), not the start of a flag,
// and the dash-word the pattern finds there is not a token.
func factTokens(s string) []string {
	s = strings.ToLower(s)
	var out []string
	for _, at := range factToken.FindAllStringIndex(s, -1) {
		token := s[at[0]:at[1]]
		if token[0] == '-' {
			if prev, _ := utf8.DecodeLastRuneInString(s[:at[0]]); unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '_' {
				continue
			}
		}
		out = append(out, token)
	}
	return out
}

func Validate(ctx *app.Context, f *File, _ ValidateOptions) []Diagnostic {
	if f == nil {
		return []Diagnostic{diagnostic("v28", "nil persona", 0, 0, "", 0)}
	}
	var ds []Diagnostic
	add := func(code, detail string) { ds = append(ds, diagnostic(code, detail, 0, 0, "", 0)) }
	if !f.schemaSet || f.schema != 1 {
		add("v3", "schema")
	}
	if !ValidID(f.ID) || f.ID == None || f.declaredID != "" && (f.declaredID != f.ID || !ValidID(f.declaredID)) {
		add("v4", f.ID)
	}
	for _, fieldValue := range []struct{ field, value string }{
		{"name", f.Name}, {"tagline", f.Tagline}, {"extends", f.Extends}, {"lang", f.Lang},
	} {
		field, value := fieldValue.field, fieldValue.value
		if _, _, unsafe := msg.UnsafeRune(value); unsafe || strings.ContainsAny(value, "\r\t\n") {
			add("v0", field)
		}
	}
	if f.declaredID != "" {
		if _, _, unsafe := msg.UnsafeRune(f.declaredID); unsafe || strings.ContainsAny(f.declaredID, "\r\t\n") {
			add("v0", "id")
		}
	}
	if n := utf8.RuneCountInString(f.Name); n < 1 || n > 32 {
		add("v5", "name")
	}
	if !msg.ValidEmoji(f.Emoji) || strings.IndexFunc(f.Emoji, func(r rune) bool { return r < utf8.RuneSelf }) >= 0 {
		add("v6", "emoji")
	}
	if f.Emoji != "" && !msg.ValidEmoji(f.Emoji) {
		if _, _, unsafe := msg.UnsafeRune(f.Emoji); unsafe || strings.ContainsAny(f.Emoji, "\r\t\n") {
			add("v0", "emoji")
		}
	}
	if strings.ContainsAny(f.Tagline, "`$") || utf8.RuneCountInString(f.Tagline) > 100 || strings.ContainsAny(f.Tagline, "\r\n") {
		add("v7", "tagline")
	}
	// "neatpik" is the embedded base, whatever personas/neatpik.toml says, so
	// it needs no file (and a neatpik.toml may name it).
	if f.Extends != "" && f.Extends != None && f.Extends != Default {
		switch {
		case !ValidID(f.Extends):
			add("v9", "extends")
		case f.Extends == f.ID:
			add("v9_cycle", f.Extends) // the shortest cycle: a file that extends itself
		case ctx != nil:
			if _, _, err := Read(ctx, f.Extends); err != nil {
				add("v9", "extends")
			}
		}
	}
	if f.Lang != "" {
		if _, err := language.Parse(f.Lang); err != nil {
			add("v9", "lang")
		} else if f.Lang != "en" {
			add("w6", f.Lang)
		}
	}
	for _, tokenPair := range []struct {
		token string
		pair  Pair
	}{
		{"accent", f.Colors.Accent}, {"ok", f.Colors.OK}, {"muted", f.Colors.Muted},
	} {
		token, pair := tokenPair.token, tokenPair.pair
		for _, sideValue := range []struct{ side, value string }{{"light", pair.Light}, {"dark", pair.Dark}} {
			side, value := sideValue.side, sideValue.value
			if value == "" {
				continue
			}
			if _, _, unsafe := msg.UnsafeRune(value); unsafe || strings.ContainsAny(value, "\r\t\n") {
				add("v0", "colors."+token+"."+side)
			}
			if !validColor(value) {
				add("v9", "colors."+token+"."+side)
				continue
			}
			if contrast(value, side == "dark") < 4.5 {
				add("w1", "colors."+token+"."+side)
			}
		}
	}
	if msg.ValidEmoji(f.Emoji) && (ansi.StringWidth(f.Emoji) != 2 || width.LookupRune([]rune(f.Emoji)[0]).Kind() != width.EastAsianWide) {
		add("w2", "emoji")
	}
	if fact := newFact(f.Tagline, ""); fact != "" {
		add("w7", "tagline: "+fact)
	}
	lineIDs := make([]string, 0, len(f.Lines))
	for id := range f.Lines {
		lineIDs = append(lineIDs, id)
	}
	sort.Strings(lineIDs)
	for _, id := range lineIDs {
		variants := f.Lines[id]
		keyDiag := func(code string) Diagnostic {
			d := diagnostic(code, id, 0, 0, id, 0)
			d.inLines = true
			return d
		}
		if _, _, unsafe := msg.UnsafeRune(id); unsafe || strings.ContainsAny(id, "\r\t\n") {
			d := keyDiag("v0")
			d.Severity = Warning
			ds = append(ds, d)
			continue
		}
		meta, found := msg.Lookup(id)
		if !found {
			ds = append(ds, keyDiag("v11"))
			continue
		}
		if !meta.Persona {
			ds = append(ds, keyDiag("v12"))
			continue
		}
		if len(variants) == 0 {
			ds = append(ds, keyDiag("v13"))
			continue
		}
		seen := map[string]bool{}
		for i, line := range variants {
			variant := i + 1
			lineDiag := func(code, detail string) { ds = append(ds, diagnostic(code, detail, 0, 0, id, variant)) }
			if seen[line] {
				lineDiag("w4", id)
			}
			seen[line] = true
			if _, _, unsafe := msg.UnsafeRune(line); unsafe {
				lineDiag("v0", id)
				continue
			}
			if err := msg.CheckLine(id, line); err != nil {
				var le *msg.LineError
				if errors.As(err, &le) {
					lineDiag(lineProblem(le.Problem), le.Name)
				} else {
					lineDiag("v14", id)
				}
				continue
			}
			validateLayout(meta, line, variant, &ds)
		}
	}
	for i := range ds {
		ds[i].Path = f.Path
	}
	return ds
}

func lineProblem(p msg.LineProblem) string {
	switch p {
	case msg.LineUnknownID:
		return "v11"
	case msg.LineNotVoiced:
		return "v12"
	case msg.LineUnsafe:
		return "v0"
	case msg.LineSyntax:
		return "v14"
	case msg.LineMissing:
		return "v15"
	case msg.LineUnknownName:
		return "v16"
	case msg.LineSelector:
		return "v17"
	}
	return "v14"
}

func validateLayout(meta msg.Meta, line string, variant int, ds *[]Diagnostic) {
	add := func(code string) { *ds = append(*ds, diagnostic(code, meta.ID, 0, 0, meta.ID, variant)) }
	english := meta.Text
	if leading(line) != leading(english) || trailing(line) != trailing(english) {
		add("v19")
	}
	if prefix := positionPrefix.FindString(english); prefix != "" && !strings.HasPrefix(line, prefix) {
		add("v20")
	}
	if english != "" && strings.ContainsRune(",;(", []rune(english)[0]) && (line == "" || []rune(line)[0] != []rune(english)[0]) {
		add("v21")
	}
	if meta.Append != "" && lastNonSpace(line) != lastNonSpace(english) {
		add("v22")
	}
	if kept := touchingPairs(line); slices.ContainsFunc(touchingPairs(english), func(pair string) bool { return !slices.Contains(kept, pair) }) {
		add("v23")
	}
	if strings.Count(line, "\n") != strings.Count(english, "\n") {
		add("v24")
	}
	length, englishLength := utf8.RuneCountInString(line), utf8.RuneCountInString(english)
	if length > 4*englishLength+200 {
		add("v26")
	} else if length > max(3*englishLength, englishLength+80) {
		add("w5")
	}
	englishTemplate, _ := catalog.Parse(english)
	lineTemplate, _ := catalog.Parse(line)
	hidden := map[string]bool{}
	for _, ref := range englishTemplate.Refs() {
		if !showsCount(meta, ref) || hidden[ref.Name] {
			continue
		}
		used, shown := false, false
		for _, candidate := range lineTemplate.Refs() {
			if candidate.Name == ref.Name {
				used = true
				shown = shown || candidate.Forms == nil || formsContainCount(candidate.Forms)
			}
		}
		if used && !shown {
			hidden[ref.Name] = true
			add("v18")
		}
	}
	if fact := newFact(line, english); fact != "" {
		*ds = append(*ds, diagnostic("w7", fact, 0, 0, meta.ID, variant))
	}
}

// touchingPairs are the placeholders that touch in s, as "a>b" for {a}{b},
// with or without a plural selector. A run of three or more gives every
// neighbouring pair: {a}{b}{c} is a>b and b>c, so that a persona cannot split
// the end of a run (the shell command {git_prefix}{repo_abs}{abort}) any more
// than its start.
func touchingPairs(s string) []string {
	var pairs []string
	prevEnd, prev := -1, ""
	for _, at := range placeholder.FindAllStringSubmatchIndex(s, -1) {
		name := s[at[2]:at[3]]
		if at[0] == prevEnd {
			pairs = append(pairs, prev+">"+name)
		}
		prevEnd, prev = at[1], name
	}
	return pairs
}

// showsCount reports whether the English shows the value of ref as a count: a
// plural selector whose forms write it with '#', or a bare placeholder of an
// int slot ({n}). A variant hides that count (V18) only when it uses the
// placeholder and none of its references shows the count: a bare {n}, or a
// selector with a '#' in a form. "{n} {n:step|steps}" shows it once, in the
// bare {n}, and the selector only picks the noun.
func showsCount(meta msg.Meta, ref catalog.Ref) bool {
	if ref.Forms != nil {
		return formsContainCount(ref.Forms)
	}
	for _, slot := range meta.Slots {
		if slot.Name == ref.Name {
			return slot.Type == catalog.Int
		}
	}
	return false
}

func formsContainCount(forms []catalog.Form) bool {
	for _, f := range forms {
		if strings.Contains(f.Text, "#") {
			return true
		}
	}
	return false
}

func leading(s string) string  { return s[:len(s)-len(strings.TrimLeftFunc(s, unicode.IsSpace))] }
func trailing(s string) string { return s[len(strings.TrimRightFunc(s, unicode.IsSpace)):] }
func lastNonSpace(s string) rune {
	r := []rune(strings.TrimRightFunc(s, unicode.IsSpace))
	if len(r) == 0 {
		return 0
	}
	return r[len(r)-1]
}

func validColor(s string) bool {
	if hexColor.MatchString(s) {
		return true
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 0 && n <= 255 && strconv.Itoa(n) == s
}

func contrast(s string, dark bool) float64 {
	r, g, b := colorRGB(s)
	background := 1.0
	if dark {
		background = luminance(30, 30, 30)
	}
	foreground := luminance(r, g, b)
	return (math.Max(foreground, background) + 0.05) / (math.Min(foreground, background) + 0.05)
}

func colorRGB(s string) (int, int, int) {
	if strings.HasPrefix(s, "#") {
		r, _ := strconv.ParseInt(s[1:3], 16, 32)
		g, _ := strconv.ParseInt(s[3:5], 16, 32)
		b, _ := strconv.ParseInt(s[5:7], 16, 32)
		return int(r), int(g), int(b)
	}
	n, _ := strconv.Atoi(s)
	base := [16][3]int{{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0}, {0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192}, {128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0}, {0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255}}
	if n < 16 {
		return base[n][0], base[n][1], base[n][2]
	}
	if n >= 232 {
		gray := 8 + 10*(n-232)
		return gray, gray, gray
	}
	n -= 16
	values := [6]int{0, 95, 135, 175, 215, 255}
	return values[n/36], values[(n/6)%6], values[n%6]
}

func luminance(r, g, b int) float64 {
	channel := func(n int) float64 {
		v := float64(n) / 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

func newFact(line, english string) string {
	existing := map[string]bool{}
	for _, v := range factTokens(english) {
		existing[v] = true
	}
	for _, v := range factTokens(line) {
		if !existing[v] {
			return v
		}
	}
	return ""
}
