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

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// MysqlCrudInput defines the input for the MySQL CRUD tool.
// Tags follow eino's documented conventions: descriptions live in
// jsonschema_description (embedding them in the jsonschema tag gets truncated
// at commas), and operate_type carries a real enum matching the Next.js zod
// schema z.enum(["query", "insert", "update", "delete"]).
type MysqlCrudInput struct {
	DSN         string `json:"dsn" jsonschema:"required" jsonschema_description:"MySQL DSN, including username/password/host/port/database name, e.g., root:pass@tcp(host:3306)/db"`
	SQL         string `json:"sql" jsonschema:"required" jsonschema_description:"SQL statement to execute"`
	OperateType string `json:"operate_type" jsonschema:"required,enum=query,enum=insert,enum=update,enum=delete" jsonschema_description:"SQL operation type"`
}

// NewMysqlCrudTool creates a tool that executes SQL queries against a MySQL database.
// The web version removes the interactive stdin confirmation present in the source
// project and executes the SQL directly. Query results are returned in JSON format.
// Construction errors (rare; only JSON-schema inference) are returned to the caller
// instead of terminating the process via log.Fatal.
func NewMysqlCrudTool() (tool.InvokableTool, error) {
	t, err := utils.InferOptionableTool(
		"mysql_crud",
		"Execute SQL queries against a MySQL database and return results in JSON format. Supports query, insert, update, and delete operations. Results are formatted as JSON for easy parsing.",
		func(ctx context.Context, input *MysqlCrudInput, opts ...tool.Option) (string, error) {
			result, err := execMysqlSql(input)
			if err != nil {
				// Return a JSON error payload to the LLM instead of a tool error, so
				// the agent can reason about the failure (e.g. fix the DSN) and retry
				// rather than aborting the whole ReAct stream. Mirrors the Next.js AI
				// SDK behavior where tool execution errors are fed back to the model.
				b, _ := json.Marshal(map[string]any{
					"success": false,
					"error":   err.Error(),
					"message": "Failed to execute SQL against MySQL",
				})
				return string(b), nil
			}
			return result, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("infer mysql_crud tool: %w", err)
	}
	return t, nil
}

// normalizeDsn accepts both the go-sql-driver format (user:pass@tcp(host:port)/db)
// and the MySQL URL format (mysql://user:pass@host:port/db), converting the latter
// to the former. Mirrors the Next.js normalizeDsn, which supports both formats.
// parseTime=true is always ensured: without it temporal columns come back as
// []byte and GORM's scan into time.Time fails with "unsupported Scan".
func normalizeDsn(dsn string) string {
	if strings.HasPrefix(dsn, "mysql://") {
		u, err := url.Parse(dsn)
		if err != nil || u.User == nil {
			return dsn
		}
		password, _ := u.User.Password()
		db := strings.TrimPrefix(u.Path, "/")
		dsn = fmt.Sprintf("%s:%s@tcp(%s)/%s", u.User.Username(), password, u.Host, db)
	}
	if strings.Contains(dsn, "parseTime=") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "parseTime=true"
}

// execMysqlSql opens a GORM connection and executes the SQL exactly once based on
// operate_type: "query" returns rows as JSON; insert/update/delete return a success
// message. The previous implementation ran db.Exec then db.Raw for queries (double
// execution) and blocked on stdin for confirmation — both are fixed here.
func execMysqlSql(input *MysqlCrudInput) (string, error) {
	db, err := gorm.Open(mysql.Open(normalizeDsn(input.DSN)), &gorm.Config{})
	if err != nil {
		return "", fmt.Errorf("open mysql: %w", err)
	}
	// Mirror the Next.js db.destroy() in finally: each call opens a fresh pool,
	// so close it afterwards to avoid leaking connections.
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	if input.OperateType == "query" {
		var results []map[string]any
		if err := db.Raw(input.SQL).Scan(&results).Error; err != nil {
			return "", fmt.Errorf("query mysql: %w", err)
		}
		b, err := json.Marshal(results)
		if err != nil {
			return "", fmt.Errorf("marshal query result: %w", err)
		}
		return string(b), nil
	}

	// insert / update / delete
	if err := db.Exec(input.SQL).Error; err != nil {
		return "", fmt.Errorf("exec mysql: %w", err)
	}
	resp := map[string]any{
		"success": true,
		"message": fmt.Sprintf("Executed %s sql", input.OperateType),
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("marshal exec result: %w", err)
	}
	return string(b), nil
}
