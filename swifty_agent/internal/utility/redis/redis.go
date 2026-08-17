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

// Package redis provides a client factory for connecting to Redis Stack (RediSearch).
// It configures the client for vector search (FT.SEARCH) and ensures the vector index exists.
package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/ai/embedder"
	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/config"
	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/consts"
	"github.com/hangtiancheng/swifty.go/swifty_agent/internal/utility/logger"
	"github.com/redis/go-redis/v9"
)

// NewClient creates a Redis client configured for vector search and ensures the
// RediSearch vector index exists (creating it on first connection if necessary).
//
// Reconnection uses exponential backoff (mirrors the Next.js socket
// reconnectStrategy: retries*100ms capped at 5s) so transient network blips or
// Redis restarts don't permanently break the singleton.
func NewClient(ctx context.Context, cfg *config.Config) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:            cfg.Redis.Addr,
		Password:        cfg.Redis.Password,
		DB:              cfg.Redis.DB,
		Protocol:        2, // Required: FT.SEARCH needs RESP2, otherwise results are raw.
		MaxRetries:      3,
		MinRetryBackoff: 100 * time.Millisecond,
		MaxRetryBackoff: 5 * time.Second,
	})
	client.Options().UnstableResp3 = true // Required for vector search.

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("connect to Redis: %w", err)
	}
	if err := ensureIndex(ctx, client, cfg); err != nil {
		return nil, fmt.Errorf("ensure Redis index: %w", err)
	}
	return client, nil
}

// ensureIndex verifies that the RediSearch vector index exists and that its
// vector dimension matches the actual embedding provider output. The dimension
// is probed from the live provider (not taken from static config) so that
// provider switches (openai ↔ ollama) or models whose output differs from
// config are handled correctly. If the index is missing it is created; if the
// dimension differs the index is dropped, stale hashes are purged, and the
// index is recreated. This mirrors the Next.js ensureIndex logic in
// lib/redis/client.ts.
func ensureIndex(ctx context.Context, client *redis.Client, cfg *config.Config) error {
	wantDim, err := embedder.ProbeDimension(ctx, cfg)
	if err != nil {
		return fmt.Errorf("probe embedding dimension: %w", err)
	}
	logger.L().Info("probed embedding dimension", "dim", wantDim)

	needCreate := true
	if info, err := client.Do(ctx, "FT.INFO", consts.RedisIndexName).Result(); err == nil {
		if storedDim, ok := parseVectorDim(info); ok && storedDim != wantDim {
			logger.L().Warn("redis index dimension mismatch; dropping and recreating",
				"stored", storedDim, "probed", wantDim)
			if derr := client.Do(ctx, "FT.DROPINDEX", consts.RedisIndexName).Err(); derr != nil {
				return fmt.Errorf("drop index on dimension mismatch: %w", derr)
			}
			// Purge stale hashes so RediSearch's background rescan does not
			// resurrect old vectors that no longer match the new dimension.
			if derr := deleteStaleHashes(ctx, client); derr != nil {
				return fmt.Errorf("delete stale hashes: %w", derr)
			}
		} else {
			needCreate = false
		}
	}

	if !needCreate {
		return nil
	}

	return client.Do(ctx, "FT.CREATE", consts.RedisIndexName,
		"ON", "HASH",
		"PREFIX", "1", consts.RedisKeyPrefix,
		"SCHEMA",
		consts.RedisContentField, "TEXT",
		consts.RedisSourceField, "TAG",
		consts.RedisVectorField, "VECTOR", "HNSW", "6",
		"TYPE", "FLOAT32",
		"DIM", strconv.Itoa(wantDim),
		"DISTANCE_METRIC", "COSINE",
	).Err()
}

// deleteStaleHashes scans all keys under the Redis key prefix and deletes them.
// Called after a dimension mismatch to prevent old vectors (with the wrong
// dimension) from being re-indexed. Mirrors the Next.js scanIterator cleanup
// in lib/redis/client.ts.
func deleteStaleHashes(ctx context.Context, client *redis.Client) error {
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, consts.RedisKeyPrefix+"*", 500).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
			logger.L().Info("purged stale hashes", "count", len(keys))
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

// parseVectorDim extracts the VECTOR field dimension from an FT.INFO result.
// FT.INFO (RESP2) returns a flat [key, value, key, value, ...] array at the top
// level; the "attributes" value is a list of attribute arrays, each itself a
// flat [key, value, ...] array. We locate the attribute whose type is VECTOR and
// return its DIM/DIMS value. Returns ok=false if the dimension cannot be determined.
func parseVectorDim(info any) (int, bool) {
	arr, ok := info.([]any)
	if !ok {
		return 0, false
	}
	for i := 0; i+1 < len(arr); i += 2 {
		key, _ := arr[i].(string)
		if key != "attributes" {
			continue
		}
		attrs, ok := arr[i+1].([]any)
		if !ok {
			continue
		}
		for _, a := range attrs {
			attr, ok := a.([]any)
			if !ok {
				continue
			}
			if !attrHasType(attr, "VECTOR") {
				continue
			}
			for j := 0; j+1 < len(attr); j += 2 {
				k, _ := attr[j].(string)
				if k == "DIM" || k == "DIMS" {
					if d, err := strconv.Atoi(fmt.Sprintf("%v", attr[j+1])); err == nil {
						return d, true
					}
				}
			}
		}
	}
	return 0, false
}

// attrHasType reports whether a flat attribute [key, value, ...] array has a
// "type" key equal to the given value.
func attrHasType(attr []any, want string) bool {
	for j := 0; j+1 < len(attr); j += 2 {
		k, _ := attr[j].(string)
		v := fmt.Sprintf("%v", attr[j+1])
		if k == "type" && v == want {
			return true
		}
	}
	return false
}
