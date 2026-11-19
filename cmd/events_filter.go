// Copyright 2026 Blink Labs Software
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

type eventFilter struct {
	key     string
	pattern string
	negate  bool
}

// parseEventFilters compiles --filter expressions into one predicate that
// requires all of them to match.
func parseEventFilters(exprs []string) (func(eventResult) bool, error) {
	filters := make([]eventFilter, 0, len(exprs))
	for _, e := range exprs {
		f, err := parseEventFilter(e)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
	return func(ev eventResult) bool {
		for _, f := range filters {
			if !f.matches(ev) {
				return false
			}
		}
		return true
	}, nil
}

func parseEventFilter(expr string) (eventFilter, error) {
	var f eventFilter
	key, pattern, ok := strings.Cut(expr, "!=")
	if ok {
		f.negate = true
	} else if key, pattern, ok = strings.Cut(expr, "="); !ok {
		return f, fmt.Errorf("invalid --filter %q: want key=value or key!=value", expr)
	}
	key = strings.TrimSpace(key)
	switch {
	case key == "origin":
		key = "origin_node_id"
	case key == "type", key == "origin_node_id", key == "id", key == "sequence":
	case strings.HasPrefix(key, "data.") && len(key) > len("data."):
	default:
		return f, fmt.Errorf("invalid --filter %q: unknown key %q (use type, origin_node_id, id, sequence, or data.<field>)", expr, key)
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return f, fmt.Errorf("invalid --filter %q: bad pattern: %w", expr, err)
	}
	f.key, f.pattern = key, pattern
	return f, nil
}

func (f eventFilter) matches(ev eventResult) bool {
	v, ok := ev.lookup(f.key)
	hit := ok
	if ok {
		// path.Match treats "/" specially; values here may contain it.
		hit = globMatch(f.pattern, v)
	}
	return hit != f.negate
}

func (r eventResult) lookup(key string) (string, bool) {
	switch key {
	case "type":
		return r.Type, true
	case "origin_node_id":
		return r.OriginNodeID, true
	case "id":
		return r.ID, true
	case "sequence":
		return strconv.FormatUint(r.Sequence, 10), true
	}
	var cur any = r.Data
	for _, part := range strings.Split(strings.TrimPrefix(key, "data."), ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		if cur, ok = m[part]; !ok {
			return "", false
		}
	}
	switch cur.(type) {
	case map[string]any, []any, nil:
		return "", false
	}
	return fmt.Sprint(cur), true
}

// globMatch matches v against a pattern with * and ? wildcards. "/" is
// swapped out first because path.Match would not let a wildcard cross it.
func globMatch(pattern, v string) bool {
	ok, _ := path.Match(
		strings.ReplaceAll(pattern, "/", "\x00"),
		strings.ReplaceAll(v, "/", "\x00"),
	)
	return ok
}
