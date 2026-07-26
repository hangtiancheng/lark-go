// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package swifty_orm

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type condition struct {
	field string
	op    string
	value any
}

// Canonical internal ops beyond MongoDB "$" operators:
// "=", "null", "notNull", "between", "notBetween", "like", "ilike".
var opAliases = map[string]string{
	"=":           "=",
	"==":          "=",
	"!=":          "$ne",
	"<>":          "$ne",
	">":           "$gt",
	">=":          "$gte",
	"<":           "$lt",
	"<=":          "$lte",
	"in":          "$in",
	"not in":      "$nin",
	"nin":         "$nin",
	"like":        "like",
	"ilike":       "ilike",
	"between":     "between",
	"not between": "notBetween",
}

// parseWhere translates Where/OrWhere arguments into conditions.
// Supported forms:
//
//	parseWhere(map)                -> one equality condition per key
//	parseWhere(field, value)       -> equality (nil value becomes null check)
//	parseWhere(field, op, value)   -> operator condition
func parseWhere(args ...any) ([]condition, error) {
	switch len(args) {
	case 1:
		return parseWhereMap(args[0])
	case 2:
		field, ok := args[0].(string)
		if !ok || strings.TrimSpace(field) == "" {
			return nil, fmt.Errorf("where: field must be a non-empty string, got %T", args[0])
		}
		if args[1] == nil {
			return []condition{{field: field, op: "null"}}, nil
		}
		return []condition{{field: field, op: "=", value: args[1]}}, nil
	case 3:
		field, ok := args[0].(string)
		if !ok || strings.TrimSpace(field) == "" {
			return nil, fmt.Errorf("where: field must be a non-empty string, got %T", args[0])
		}
		rawOp, ok := args[1].(string)
		if !ok {
			return nil, fmt.Errorf("where: operator must be a string, got %T", args[1])
		}
		op, value, err := normalizeOp(rawOp, args[2])
		if err != nil {
			return nil, err
		}
		return []condition{{field: field, op: op, value: value}}, nil
	default:
		return nil, fmt.Errorf("where: expected 1 (map), 2 or 3 arguments, got %d", len(args))
	}
}

func parseWhereMap(arg any) ([]condition, error) {
	var doc map[string]any
	switch m := arg.(type) {
	case bson.M:
		doc = m
	case map[string]any:
		doc = m
	default:
		return nil, fmt.Errorf("where: single argument must be bson.M or map[string]any, got %T", arg)
	}
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	conditions := make([]condition, 0, len(keys))
	for _, k := range keys {
		if doc[k] == nil {
			conditions = append(conditions, condition{field: k, op: "null"})
			continue
		}
		conditions = append(conditions, condition{field: k, op: "=", value: doc[k]})
	}
	return conditions, nil
}

func normalizeOp(rawOp string, value any) (string, any, error) {
	op, known := opAliases[strings.ToLower(strings.TrimSpace(rawOp))]
	if !known {
		if strings.HasPrefix(rawOp, "$") {
			return rawOp, value, nil
		}
		return "", nil, fmt.Errorf("where: unsupported operator %q", rawOp)
	}
	switch op {
	case "between", "notBetween":
		pair, err := toBetweenPair(value)
		if err != nil {
			return "", nil, err
		}
		return op, pair, nil
	case "like", "ilike":
		if _, ok := value.(string); !ok {
			return "", nil, fmt.Errorf("where: %s requires a string pattern, got %T", op, value)
		}
	}
	return op, value, nil
}

func toBetweenPair(value any) ([2]any, error) {
	if pair, ok := value.([2]any); ok {
		return pair, nil
	}
	rv := reflect.ValueOf(value)
	if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) && rv.Len() == 2 {
		return [2]any{rv.Index(0).Interface(), rv.Index(1).Interface()}, nil
	}
	return [2]any{}, fmt.Errorf("where: between requires a 2-element slice or array, got %T", value)
}

// likeToRegex converts a SQL LIKE pattern (% and _ wildcards) into an anchored
// regular expression, escaping all regex metacharacters in the pattern.
func likeToRegex(pattern string) string {
	quoted := regexp.QuoteMeta(pattern)
	quoted = strings.ReplaceAll(quoted, "%", ".*")
	quoted = strings.ReplaceAll(quoted, "_", ".")
	return "^" + quoted + "$"
}

func (q *Query) buildFilter() bson.M {
	base := buildConditionFilter(q.conditions)
	if len(q.orGroups) == 0 {
		return base
	}
	orClauses := make([]bson.M, 0, len(q.orGroups)+1)
	if len(q.conditions) > 0 {
		orClauses = append(orClauses, base)
	}
	for _, group := range q.orGroups {
		if len(group) == 0 {
			continue
		}
		orClauses = append(orClauses, buildConditionFilter(group))
	}
	if len(orClauses) == 0 {
		return base
	}
	if len(orClauses) == 1 {
		return orClauses[0]
	}
	return bson.M{"$or": orClauses}
}

// buildConditionFilter merges conditions into a single filter document.
// Conditions on the same field that cannot be merged into one operator map
// (duplicate equalities, equality after operators, duplicate operators) are
// preserved through a top-level $and instead of being overwritten.
func buildConditionFilter(conditions []condition) bson.M {
	filter := bson.M{}
	// Tracks fields whose filter value is an operator map created here, as
	// opposed to a user-supplied equality value that happens to be a bson.M.
	opFields := make(map[string]bool)
	var andClauses []bson.M

	setEquality := func(field string, value any) {
		existing, exists := filter[field]
		if !exists {
			filter[field] = value
			return
		}
		if opFields[field] {
			m := existing.(bson.M)
			if _, has := m["$eq"]; !has {
				m["$eq"] = value
				return
			}
		}
		andClauses = append(andClauses, bson.M{field: value})
	}

	setOp := func(field string, op string, value any) {
		existing, exists := filter[field]
		if exists {
			if opFields[field] {
				m := existing.(bson.M)
				if _, has := m[op]; !has {
					m[op] = value
					return
				}
			}
			andClauses = append(andClauses, bson.M{field: bson.M{op: value}})
			return
		}
		filter[field] = bson.M{op: value}
		opFields[field] = true
	}

	for _, c := range conditions {
		switch c.op {
		case "=":
			setEquality(c.field, c.value)
		case "null":
			setEquality(c.field, nil)
		case "notNull":
			setOp(c.field, "$ne", nil)
		case "between":
			pair := c.value.([2]any)
			setOp(c.field, "$gte", pair[0])
			setOp(c.field, "$lte", pair[1])
		case "notBetween":
			pair := c.value.([2]any)
			setOp(c.field, "$not", bson.M{"$gte": pair[0], "$lte": pair[1]})
		case "like":
			setOp(c.field, "$regex", primitive.Regex{Pattern: likeToRegex(c.value.(string))})
		case "ilike":
			setOp(c.field, "$regex", primitive.Regex{Pattern: likeToRegex(c.value.(string)), Options: "i"})
		default:
			setOp(c.field, c.op, c.value)
		}
	}

	if len(andClauses) > 0 {
		filter["$and"] = andClauses
	}
	return filter
}

func (q *Query) buildProjection() bson.M {
	if len(q.fields) == 0 {
		return nil
	}
	proj := bson.M{}
	for _, f := range q.fields {
		if after, ok := strings.CutPrefix(f, "-"); ok {
			proj[after] = 0
			continue
		}
		proj[f] = 1
	}
	return proj
}
