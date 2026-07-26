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

package swifty_cache

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/hangtiancheng/swifty.go/swifty_http"
)

type dashboardSnapshot struct {
	Type   string          `json:"type"`
	Groups []groupSnapshot `json:"groups"`
}

type groupSnapshot struct {
	Name    string          `json:"name"`
	Stats   map[string]any  `json:"stats"`
	Entries []entrySnapshot `json:"entries"`
}

type entrySnapshot struct {
	Key      string `json:"key"`
	Size     int    `json:"size"`
	ExpireAt int64  `json:"expire_at"`
	Level    int    `json:"level"`
}

type dashboardCommand struct {
	Action string `json:"action"`
	Group  string `json:"group"`
	Key    string `json:"key"`
}

var dashboardOnce sync.Once

// DashboardHandler returns a handler that can be mounted on any swifty_http application
// to serve the dashboard WebSocket endpoint.
func DashboardHandler() func(ctx *swifty_http.Context, next func()) {
	return func(ctx *swifty_http.Context, next func()) {
		ws, err := ctx.Upgrade(&swifty_http.UpgradeOptions{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
		})
		if err != nil {
			log.Printf("[Dashboard] upgrade failed: %v", err)
			return
		}

		serveDashboardConn(ws)
	}
}

func serveDashboardConn(ws *swifty_http.WSConn) {
	stopHeartbeat := ws.Heartbeat(30 * time.Second)

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		defer ws.Close()
		defer stopHeartbeat()

		if err := ws.WriteJSON(buildSnapshot()); err != nil {
			return
		}

		for {
			select {
			case <-ticker.C:
				if err := ws.WriteJSON(buildSnapshot()); err != nil {
					return
				}
			case <-ws.Closed():
				return
			}
		}
	}()

	go func() {
		for {
			var cmd dashboardCommand
			if err := ws.ReadJSON(&cmd); err != nil {
				break
			}
			handleCommand(cmd)
		}
	}()
}

// StartDashboard starts the dashboard HTTP server on addr.
// It is safe to call multiple times; only the first call takes effect.
func StartDashboard(addr string) {
	dashboardOnce.Do(func() {
		app := swifty_http.New()
		app.Get("/dashboard/ws", DashboardHandler())

		go func() {
			log.Printf("[Dashboard] listening on %s", addr)
			if err := app.Listen(addr); err != nil {
				log.Printf("[Dashboard] server error: %v", err)
			}
		}()
	})
}

func buildSnapshot() dashboardSnapshot {
	allGroups := GetAllGroups()
	snap := dashboardSnapshot{
		Type:   "snapshot",
		Groups: make([]groupSnapshot, 0, len(allGroups)),
	}

	for name, g := range allGroups {
		if !g.DashboardEnabled() {
			continue
		}
		entries := g.Entries()
		entrySnaps := make([]entrySnapshot, len(entries))
		for i, e := range entries {
			entrySnaps[i] = entrySnapshot(e)
		}

		snap.Groups = append(snap.Groups, groupSnapshot{
			Name:    name,
			Stats:   g.Stats(),
			Entries: entrySnaps,
		})
	}

	return snap
}

func handleCommand(cmd dashboardCommand) {
	g := GetGroup(cmd.Group)
	if g == nil || !g.DashboardEnabled() {
		log.Printf("[Dashboard] group %q not found or dashboard disabled", cmd.Group)
		return
	}

	ctx := context.Background()

	switch cmd.Action {
	case "delete":
		if err := g.Delete(ctx, cmd.Key); err != nil {
			log.Printf("[Dashboard] delete %q from %q failed: %v", cmd.Key, cmd.Group, err)
		}
	default:
		log.Printf("[Dashboard] unknown action %q", cmd.Action)
	}
}
