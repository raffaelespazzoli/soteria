/*
Copyright 2026 The Soteria Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package scylladb

import (
	"context"
	"fmt"

	"github.com/gocql/gocql"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

const cqlLimitClause = " LIMIT ?"

// candidateRow holds a label index result row including the label_value,
// which is needed for correct cross-page continuation with in/exists.
type candidateRow struct {
	labelValue string
	namespace  string
	name       string
}

// classifiedSelector holds the result of classifying a label selector into
// a primary pushable requirement and residual requirements that must be
// evaluated in-memory.
type classifiedSelector struct {
	primary     *labels.Requirement
	residual    []labels.Requirement
	hasPushable bool
}

// classifySelector partitions selector requirements into a primary (pushable)
// requirement and residual requirements. The most selective pushable operator
// is chosen as primary: Equals > In > Exists.
func classifySelector(sel labels.Selector) classifiedSelector {
	reqs, selectable := sel.Requirements()
	if !selectable || len(reqs) == 0 {
		return classifiedSelector{}
	}

	bestIdx := -1
	bestPrio := -1
	for i := range reqs {
		p := pushablePriority(reqs[i].Operator())
		if p > bestPrio {
			bestIdx = i
			bestPrio = p
		}
	}

	if bestIdx < 0 {
		return classifiedSelector{residual: reqs}
	}

	result := classifiedSelector{
		primary:     &reqs[bestIdx],
		hasPushable: true,
	}
	for i := range reqs {
		if i != bestIdx {
			result.residual = append(result.residual, reqs[i])
		}
	}
	return result
}

// pushablePriority returns a priority for the operator, higher = more
// selective. Returns -1 for non-pushable operators.
func pushablePriority(op selection.Operator) int {
	switch op {
	case selection.Equals, selection.DoubleEquals:
		return 3
	case selection.In:
		return 2
	case selection.Exists:
		return 1
	default:
		return -1
	}
}

// queryLabelIndex queries the kv_store_labels table for candidate objects
// matching the given primary requirement. Returns candidate rows with their
// label_value (needed for continuation) and the raw CQL row count before
// any in-memory filtering. The caller uses rawCount == 0 to detect true
// CQL exhaustion vs. all-filtered-away.
func queryLabelIndex(
	ctx context.Context,
	session *gocql.Session,
	keyspace string,
	apiGroup, resourceType string,
	req labels.Requirement,
	namespace string,
	continueValue, continueNS, continueName string,
	limit int64,
) ([]candidateRow, int, error) {
	switch req.Operator() {
	case selection.Equals, selection.DoubleEquals:
		values := req.Values().List()
		if len(values) != 1 {
			return nil, 0, fmt.Errorf("equality requirement should have exactly 1 value")
		}
		return queryLabelIndexEquality(ctx, session, keyspace,
			apiGroup, resourceType, req.Key(), values[0],
			namespace, continueNS, continueName, limit)

	case selection.In:
		values := req.Values().List()
		return queryLabelIndexIn(ctx, session, keyspace,
			apiGroup, resourceType, req.Key(), values,
			namespace, continueValue, continueNS, continueName, limit)

	case selection.Exists:
		return queryLabelIndexExists(ctx, session, keyspace,
			apiGroup, resourceType, req.Key(),
			namespace, continueValue, continueNS, continueName, limit)

	default:
		return nil, 0, fmt.Errorf("unsupported operator %v for label index query", req.Operator())
	}
}

func queryLabelIndexEquality(
	ctx context.Context,
	session *gocql.Session,
	keyspace string,
	apiGroup, resourceType, labelKey, labelValue string,
	namespace string,
	continueNS, continueName string,
	limit int64,
) ([]candidateRow, int, error) {
	var cql string
	var args []any

	base := fmt.Sprintf(
		`SELECT label_value, namespace, name FROM %s.kv_store_labels`+
			` WHERE api_group = ? AND resource_type = ? AND label_key = ?`,
		keyspace,
	)
	args = []any{apiGroup, resourceType, labelKey}

	switch {
	case namespace != "" && continueName != "":
		cql = base + ` AND label_value = ? AND namespace = ? AND name > ?`
		args = append(args, labelValue, namespace, continueName)
	case namespace != "":
		cql = base + ` AND label_value = ? AND namespace = ?`
		args = append(args, labelValue, namespace)
	case continueName != "":
		cql = base + ` AND (label_value, namespace, name) > (?, ?, ?)`
		args = append(args, labelValue, continueNS, continueName)
	default:
		cql = base + ` AND label_value = ?`
		args = append(args, labelValue)
	}

	if limit > 0 {
		cql += cqlLimitClause
		args = append(args, limit)
	}

	rows, err := execCandidateQuery(ctx, session, cql, args)
	if err != nil {
		return nil, 0, err
	}
	rawCount := len(rows)

	if continueName != "" && namespace == "" {
		filtered := rows[:0]
		for _, c := range rows {
			if c.labelValue == labelValue {
				filtered = append(filtered, c)
			}
		}
		rows = filtered
	}

	return rows, rawCount, nil
}

func queryLabelIndexIn(
	ctx context.Context,
	session *gocql.Session,
	keyspace string,
	apiGroup, resourceType, labelKey string,
	values []string,
	namespace string,
	continueValue, continueNS, continueName string,
	limit int64,
) ([]candidateRow, int, error) {
	if len(values) == 0 {
		return nil, 0, nil
	}

	base := fmt.Sprintf(
		`SELECT label_value, namespace, name FROM %s.kv_store_labels`+
			` WHERE api_group = ? AND resource_type = ? AND label_key = ?`+
			` AND label_value IN ?`,
		keyspace,
	)
	args := []any{apiGroup, resourceType, labelKey, values}

	var cql string
	switch {
	case namespace != "" && continueName == "":
		cql = base + ` AND namespace = ?`
		args = append(args, namespace)
	case namespace != "" && continueName != "":
		// ScyllaDB doesn't allow tuple comparison combined with IN.
		// Fetch with namespace filter and skip past the continue point in memory.
		cql = base + ` AND namespace = ?`
		args = append(args, namespace)
	default:
		cql = base
	}

	// Don't apply CQL LIMIT when we need in-memory continuation filtering
	if limit > 0 && continueName == "" {
		cql += cqlLimitClause
		args = append(args, limit)
	}

	rows, err := execCandidateQuery(ctx, session, cql, args)
	if err != nil {
		return nil, 0, err
	}
	rawCount := len(rows)

	// Skip past the continue point using full (label_value, namespace, name) ordering
	if continueName != "" {
		filtered := rows[:0]
		for _, r := range rows {
			if r.labelValue > continueValue ||
				(r.labelValue == continueValue && r.namespace > continueNS) ||
				(r.labelValue == continueValue && r.namespace == continueNS && r.name > continueName) {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
		if limit > 0 && int64(len(rows)) > limit {
			rows = rows[:limit]
		}
	}

	return rows, rawCount, nil
}

func queryLabelIndexExists(
	ctx context.Context,
	session *gocql.Session,
	keyspace string,
	apiGroup, resourceType, labelKey string,
	namespace string,
	continueValue, continueNS, continueName string,
	limit int64,
) ([]candidateRow, int, error) {
	base := fmt.Sprintf(
		`SELECT label_value, namespace, name FROM %s.kv_store_labels`+
			` WHERE api_group = ? AND resource_type = ? AND label_key = ?`,
		keyspace,
	)
	args := []any{apiGroup, resourceType, labelKey}

	var cql string
	switch {
	case continueValue != "" && continueName != "":
		cql = base + ` AND (label_value, namespace, name) > (?, ?, ?)`
		args = append(args, continueValue, continueNS, continueName)
	default:
		cql = base
	}

	// Don't apply CQL LIMIT when namespace filtering is required, because
	// the LIMIT would cap rows BEFORE the in-memory namespace filter,
	// producing undersized or empty pages while data still exists.
	if limit > 0 && namespace == "" {
		cql += cqlLimitClause
		args = append(args, limit)
	}

	rows, err := execCandidateQuery(ctx, session, cql, args)
	if err != nil {
		return nil, 0, err
	}
	rawCount := len(rows)

	if namespace != "" {
		filtered := rows[:0]
		for _, r := range rows {
			if r.namespace == namespace {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	return rows, rawCount, nil
}

func execCandidateQuery(ctx context.Context, session *gocql.Session, cql string, args []any) ([]candidateRow, error) {
	iter := session.Query(cql, args...).WithContext(ctx).Iter()
	var rows []candidateRow
	var r candidateRow
	for iter.Scan(&r.labelValue, &r.namespace, &r.name) {
		rows = append(rows, r)
		r = candidateRow{}
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return rows, nil
}

// residualMatches checks whether the given label set satisfies all residual
// requirements. Returns true if all residual requirements are met.
func residualMatches(objLabels map[string]string, residual []labels.Requirement) bool {
	objLabelSet := labels.Set(objLabels)
	for _, req := range residual {
		if !req.Matches(objLabelSet) {
			return false
		}
	}
	return true
}
