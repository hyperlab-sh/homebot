package profile

import (
	"regexp"
	"slices"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// Profile YAML dialect:
//
//	description: Everything for a desktop
//	include: [base]
//	steps:
//	  - brew: "{os}/Brewfile"
//	  - fonts: [JetBrainsMono, FiraCode]
//	  - apt: linux/apt.txt
//	    on: linux
//	  - unlock

var (
	profileKeys = []string{"description", "include", "steps"}
	stepOptions = []string{"on", "name"}
	actionName  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

func parseOn(value any, where string) ([]app.OS, error) {
	list, ok := value.([]any)
	if s, isString := value.(string); isString {
		list, ok = []any{s}, true
	}
	if !ok || len(list) == 0 {
		return nil, fail.Say(msg.ManifestErrorOn(where))
	}
	out := make([]app.OS, 0, len(list))
	for _, v := range list {
		os, _ := v.(string)
		if os != string(app.Linux) && os != string(app.MacOS) {
			return nil, fail.Say(msg.ManifestErrorOnOS(where, jsString(v)))
		}
		out = append(out, app.OS(os))
	}
	return out, nil
}

func isScalar(v any) bool {
	switch v.(type) {
	case string, float64, bool:
		return true
	}
	return false
}

func parseArgs(value any, where string) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}
	if isScalar(value) {
		return []string{jsString(value)}, nil
	}
	if list, ok := value.([]any); ok && slices.IndexFunc(list, func(v any) bool { return !isScalar(v) }) < 0 {
		out := make([]string, len(list))
		for i, v := range list {
			out[i] = jsString(v)
		}
		return out, nil
	}
	return nil, fail.Say(msg.ManifestErrorArgs(where))
}

func parseStep(raw any, file string, index int) (Step, error) {
	where := fail.Say(msg.ManifestWhereStep(file, index+1)).Error()
	source := StepSource{File: file, Index: index}
	if s, ok := raw.(string); ok {
		if !actionName.MatchString(s) {
			return Step{}, fail.Say(msg.ManifestErrorActionName(where, s))
		}
		return Step{Action: s, Args: []string{}, Source: source}, nil
	}
	rec, ok := raw.(*mapping)
	if !ok {
		return Step{}, fail.Say(msg.ManifestErrorStepShape(where))
	}
	var actions []string
	for _, k := range rec.orderedKeys() {
		if !slices.Contains(stepOptions, k) {
			actions = append(actions, k)
		}
	}
	switch {
	case len(actions) == 0:
		return Step{}, fail.Say(msg.ManifestErrorOneActionNone(where))
	case len(actions) > 1:
		return Step{}, fail.Say(msg.ManifestErrorOneAction(where, strings.Join(actions, ", ")))
	}
	action := actions[0]
	if !actionName.MatchString(action) {
		return Step{}, fail.Say(msg.ManifestErrorActionName(where, action))
	}
	value, _ := rec.get(action)
	args, err := parseArgs(value, where)
	if err != nil {
		return Step{}, err
	}
	step := Step{Action: action, Args: args, Source: source}
	if on, ok := rec.get("on"); ok {
		if step.On, err = parseOn(on, where); err != nil {
			return Step{}, err
		}
	}
	if name, ok := rec.get("name"); ok {
		s, isString := name.(string)
		if !isString {
			return Step{}, fail.Say(msg.ManifestErrorName(where))
		}
		step.Name = s
	}
	return step, nil
}

// Parse reads a profile's YAML. file is used in error messages; name is the
// profile's name. Its errors are fail.Say errors: it has no Context to say
// them with.
func Parse(text, file, name string) (*Profile, error) {
	doc, err := decodeYAML(text)
	if err != nil {
		return nil, fail.Say(msg.ManifestErrorYAML(file, err.Error()))
	}
	if doc == nil {
		doc = &mapping{vals: map[string]any{}}
	}
	rec, ok := doc.(*mapping)
	if !ok {
		return nil, fail.Say(msg.ManifestErrorNotMapping(file))
	}
	for _, k := range rec.orderedKeys() {
		if !slices.Contains(profileKeys, k) {
			return nil, fail.Say(msg.ManifestErrorUnknownKey(file, k, strings.Join(profileKeys, ", ")))
		}
	}

	include := []string{}
	if inc, ok := rec.get("include"); ok {
		var list []any
		switch v := inc.(type) {
		case string:
			list = []any{v}
		case []any:
			list = v
		default:
			return nil, fail.Say(msg.ManifestErrorInclude(file))
		}
		for _, x := range list {
			s, isString := x.(string)
			if !isString {
				return nil, fail.Say(msg.ManifestErrorInclude(file))
			}
			include = append(include, s)
		}
	}

	description := ""
	if d, ok := rec.get("description"); ok {
		if description, ok = d.(string); !ok {
			return nil, fail.Say(msg.ManifestErrorDescription(file))
		}
	}

	var rawSteps []any
	if s, _ := rec.get("steps"); s != nil {
		if rawSteps, ok = s.([]any); !ok {
			return nil, fail.Say(msg.ManifestErrorSteps(file))
		}
	}
	steps := make([]Step, 0, len(rawSteps))
	for i, raw := range rawSteps {
		step, err := parseStep(raw, file, i)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return &Profile{Name: name, File: file, Description: description, Include: include, Steps: steps}, nil
}
