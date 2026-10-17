package gitx

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// Object is what git cat-file --batch-check says about one object spec.
type Object struct {
	Spec    string
	Missing bool
	// ID, Type and Size are set unless Missing.
	ID   string
	Type string
	Size int64
}

// CatFileCheck asks git about each spec (such as "HEAD:path/to/file") in one
// process and returns one Object per spec, in order. Specs are sent
// NUL-delimited, and so are the answers, so a path may hold a newline.
func CatFileCheck(c *app.Context, dir string, specs []string) ([]Object, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	r := RunIn(c, dir, strings.Join(specs, "\x00")+"\x00", "cat-file", "--batch-check", "-Z")
	if r.Code != 0 {
		return nil, failed(r, "cat-file --batch-check")
	}
	return ParseBatchCheck(r.Stdout, specs)
}

// ParseBatchCheck parses cat-file --batch-check -Z output for specs: one
// NUL-terminated record per spec, either "<spec> missing" or
// "<id> <type> <size>". An ambiguous spec is an error.
func ParseBatchCheck(out string, specs []string) ([]Object, error) {
	records := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if len(records) != len(specs) {
		return nil, fmt.Errorf("git cat-file: %d answers for %d specs", len(records), len(specs))
	}
	objs := make([]Object, 0, len(specs))
	for i, spec := range specs {
		rec := records[i]
		if strings.HasSuffix(rec, " missing") {
			objs = append(objs, Object{Spec: spec, Missing: true})
			continue
		}
		f := strings.Split(rec, " ")
		size, err := strconv.ParseInt(f[len(f)-1], 10, 64)
		if len(f) != 3 || err != nil {
			return nil, fmt.Errorf("git cat-file: cannot read the answer for %q: %q", spec, rec)
		}
		objs = append(objs, Object{Spec: spec, ID: f[0], Type: f[1], Size: size})
	}
	return objs, nil
}
