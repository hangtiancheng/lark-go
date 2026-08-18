/**
 * Copyright (c) 2026 hangtiancheng
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { ChartBar } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import useDashboardStore, { type GroupSnapshot } from "../store/dashboard";
import { WS_URL } from "../config";
import { formatSize, formatExpire } from "../utils/format";

const ROW_HEIGHT = 36;
const OVER_SCAN = 5;
const DASHBOARD_WS = WS_URL + "/dashboard/ws";

interface FlatRow {
  group: string;
  key: string;
  size: number;
  sizeStr: string;
  level: number;
  expire_at: number;
  expireStr: string;
}

function flatten(groups: GroupSnapshot[]): FlatRow[] {
  const rows: FlatRow[] = [];
  for (const g of groups) {
    if (!g.entries) continue;
    for (const e of g.entries) {
      rows.push({
        group: g.name,
        key: e.key,
        size: e.size,
        sizeStr: formatSize(e.size),
        level: e.level,
        expire_at: e.expire_at,
        expireStr: formatExpire(e.expire_at),
      });
    }
  }
  return rows;
}

function StatusBadge({ status }: { status: string }) {
  if (status === "connected") {
    return (
      <Badge className="bg-success/15 text-success border-0">Connected</Badge>
    );
  }
  if (status === "connecting") {
    return (
      <Badge className="bg-warning/15 text-warning border-0">Connecting</Badge>
    );
  }
  return (
    <Badge
      variant="destructive"
      className="bg-destructive/15 text-destructive border-0"
    >
      Disconnected
    </Badge>
  );
}

export default function Dashboard() {
  const [totalEntries, setTotalEntries] = useState(0);
  const [status, setStatus] = useState("disconnected");
  const [visibleRows, setVisibleRows] = useState<FlatRow[]>([]);
  const [topPad, setTopPad] = useState(0);
  const [bottomPad, setBottomPad] = useState(0);

  const containerRef = useRef<HTMLDivElement>(null);
  const scrollTopRef = useRef(0);
  const rafRef = useRef(0);
  const allRowsRef = useRef<FlatRow[]>([]);

  const syncView = useCallback((s: string) => {
    const el = containerRef.current;
    if (!el) return;
    const rows = allRowsRef.current;
    const containerHeight = el.clientHeight;
    const totalRows = rows.length;
    const visibleCount =
      Math.ceil(containerHeight / ROW_HEIGHT) + OVER_SCAN * 2;
    const startIdx = Math.max(
      0,
      Math.floor(scrollTopRef.current / ROW_HEIGHT) - OVER_SCAN,
    );
    const endIdx = Math.min(totalRows, startIdx + visibleCount);
    setTotalEntries(totalRows);
    setVisibleRows(rows.slice(startIdx, endIdx));
    setTopPad(startIdx * ROW_HEIGHT);
    setBottomPad(Math.max(0, (totalRows - endIdx) * ROW_HEIGHT));
    setStatus(s);
  }, []);

  useEffect(() => {
    useDashboardStore.getState().connect(DASHBOARD_WS);
    const pollTimer = window.setInterval(() => {
      const s = useDashboardStore.getState();
      allRowsRef.current = flatten(s.groups);
      syncView(s.status);
    }, 500);
    syncView(useDashboardStore.getState().status);
    return () => {
      clearInterval(pollTimer);
      cancelAnimationFrame(rafRef.current);
      useDashboardStore.getState().disconnect();
    };
  }, [syncView]);

  const onScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    scrollTopRef.current = el.scrollTop;
    cancelAnimationFrame(rafRef.current);
    rafRef.current = requestAnimationFrame(() => {
      syncView(useDashboardStore.getState().status);
    });
  };

  const reconnect = () => {
    useDashboardStore.getState().connect(DASHBOARD_WS);
  };

  const deleteEntry = (group: string, key: string) => {
    useDashboardStore.getState().deleteKey(group, key);
  };

  return (
    <div className="bg-background flex min-h-screen items-center justify-center p-4">
      <Card className="shadow-primary/5 h-[700px] w-[1000px] gap-0 py-0 shadow-xl">
        <div className="border-border bg-muted/30 flex h-14 shrink-0 items-center justify-between border-b px-6">
          <div className="flex items-center gap-3">
            <ChartBar size={20} className="text-primary" />
            <span className="text-foreground text-lg font-semibold">
              Cache Dashboard
            </span>
          </div>
          <div className="flex items-center gap-3">
            <StatusBadge status={status} />
            <span className="text-muted-foreground text-xs">
              {totalEntries} entries
            </span>
          </div>
        </div>

        {status !== "connected" && (
          <div className="flex flex-1 items-center justify-center">
            <div className="text-center">
              <p className="text-muted-foreground mb-4 text-sm">
                WebSocket not connected
              </p>
              <Button size="sm" onClick={reconnect}>
                Connect
              </Button>
            </div>
          </div>
        )}

        {status === "connected" && (
          <div className="flex flex-1 flex-col overflow-hidden">
            <div className="border-border bg-muted/30 text-foreground flex h-10 shrink-0 items-center gap-2 border-b px-4 text-xs font-medium">
              <div className="w-16 text-center">Group</div>
              <div className="flex-1">Key</div>
              <div className="w-16 text-right">Size</div>
              <div className="w-14 text-center">Level</div>
              <div className="w-40 text-center">Expires</div>
              <div className="w-16 text-center">Action</div>
            </div>

            <div
              ref={containerRef}
              className="flex-1 overflow-y-auto"
              onScroll={onScroll}
            >
              <div
                style={{
                  paddingTop: `${topPad}px`,
                  paddingBottom: `${bottomPad}px`,
                }}
              >
                {visibleRows.map((row) => (
                  <div
                    key={row.group + row.key}
                    className="border-border hover:bg-accent/40 flex h-9 items-center gap-2 border-b px-4 text-sm transition-colors"
                  >
                    <div className="w-16 text-center">
                      <Badge variant="outline" className="font-normal">
                        {row.group}
                      </Badge>
                    </div>
                    <div
                      className="flex-1 truncate font-mono text-xs"
                      title={row.key}
                    >
                      {row.key}
                    </div>
                    <div className="text-muted-foreground w-16 text-right text-xs">
                      {row.sizeStr}
                    </div>
                    <div className="w-14 text-center">
                      {row.level === 1 ? (
                        <Badge className="bg-success/15 text-success border-0">
                          hot
                        </Badge>
                      ) : (
                        <Badge variant="secondary">cold</Badge>
                      )}
                    </div>
                    <div className="text-muted-foreground w-40 text-center text-xs">
                      {row.expireStr}
                    </div>
                    <div className="w-16 text-center">
                      <Button
                        variant="outline"
                        size="sm"
                        className="text-destructive hover:bg-destructive/10 hover:text-destructive h-6 px-2 text-xs"
                        onClick={() => deleteEntry(row.group, row.key)}
                      >
                        Delete
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>
        )}
      </Card>
    </div>
  );
}
