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
	"strings"
)

var SkipDirs = map[string]bool{
	".git": true, ".venv": true, "node_modules": true,
	"__pycache__": true, ".tox": true, ".mypy_cache": true,
}

// MaxOutputChars is the spill threshold applied before a single tool result
// enters the conversation history: content exceeding this character count is
// written to disk, leaving only a preview and the file path in history. Set
// to 50000 rather than a smaller value so the model can see enough content
// in one pass without needing an extra ReadFile round-trip.
const MaxOutputChars = 50000

type ToolResult struct {
	Output  string
	IsError bool
	// ContentBlocks 用于把工具结果发成结构化 content block 而不是纯文本。
	// 目前只有官方 Anthropic 端点下的 ToolSearch 会用：它回 tool_reference 块，
	// 由服务端把 schema 展开进上下文。填了这个字段时 Output 仍保留等价文本，
	// 供 TUI 和日志展示。
	ContentBlocks []map[string]any
}

// McpLoadingMode 决定 MCP 工具怎么进上下文，由 internal/mcp 在连上服务器后写入
// Registry。放在这里而不是 internal/mcp，是因为 Registry 要持有它，而
// internal/mcp 依赖 internal/tools，反向引用会成环。
type McpLoadingMode string

const (
	// McpLoadingEager：schema 总量小于上下文的一成，全量放进 tools[]，不延迟。
	McpLoadingEager McpLoadingMode = "eager"
	// McpLoadingNative：官方端点。工具带 defer_loading 留在 tools[] 里但服务端
	// 不给模型看，ToolSearch 回 tool_reference 让服务端展开 schema。
	McpLoadingNative McpLoadingMode = "native"
	// McpLoadingDispatch：其他端点不支持 defer_loading，MCP 工具完全不进
	// tools[]，走 McpCall 统一入口。
	McpLoadingDispatch McpLoadingMode = "dispatch"
)

// MCPTool 是 MCP 工具包装器额外暴露给分发和分流逻辑的能力。用结构化接口而不是
// 直接引用 internal/mcp，同样是为了避开循环依赖。
type MCPTool interface {
	Tool
	MCPServerName() string
	MCPInputSchema() map[string]any
	SetDeferLoading(bool)
}

// ToolSearchToolName 是工具检索的名字，注册表按模式筛它时要用。
const ToolSearchToolName = "ToolSearch"

type ToolCategory string

const (
	CategoryRead    ToolCategory = "read"
	CategoryWrite   ToolCategory = "write"
	CategoryCommand ToolCategory = "command"
)

type Tool interface {
	Name() string
	Description() string
	Category() ToolCategory
	Schema() map[string]any
	Execute(ctx context.Context, args map[string]any) ToolResult
}

// DeferrableTool lets a tool declare whether it should be lazily loaded.
// Deferred tools do not appear in the initial tool list; the model must first
// use ToolSearch to retrieve the schema before invoking them.
//
// Only MCP tools implement this interface. MCP servers are configured
// per-project and can expose dozens of tools with lengthy schemas; including
// all of them in the initial tool list would consume a large portion of the
// context, and most tools are unused in any given session. Built-in tools are
// a fixed, manageable set — hiding them would only force the model through an
// extra ToolSearch round-trip, so they are never deferred and always expose
// their full schema.
type DeferrableTool interface {
	ShouldDefer() bool
}

type Registry struct {
	tools           map[string]Tool
	discoveredTools map[string]bool
	// McpLoadingMode 由 mcp.DecideAndApply 在连上服务器后写入。没有 MCP 时保持
	// eager，行为等同于不延迟。
	McpLoadingMode McpLoadingMode

	// ExposeToolSearch / ExposeMcpCall 决定这两个工具发不发给模型，由
	// mcp.ApplyMode 在会话启动时算一次。不每轮按「当前还有没有延迟工具」现算：
	// 工具可能被运行时禁用，现算会让 tools[] 中途少一个，那就是一次数组变动，
	// 缓存前缀照样断。
	ExposeToolSearch bool
	ExposeMcpCall    bool
}

func NewRegistry() *Registry {
	return &Registry{
		tools:           make(map[string]Tool),
		discoveredTools: make(map[string]bool),
		McpLoadingMode:  McpLoadingEager,
	}
}

func (r *Registry) MarkDiscovered(name string) {
	r.discoveredTools[name] = true
}

func (r *Registry) IsDiscovered(name string) bool {
	return r.discoveredTools[name]
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) Tool {
	return r.tools[name]
}

func (r *Registry) ListTools() []Tool {
	result := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}

func isDeferred(t Tool) bool {
	if dt, ok := t.(DeferrableTool); ok {
		return dt.ShouldDefer()
	}
	return false
}

func isOpenAIProtocol(protocol string) bool {
	return protocol == "openai" || protocol == "openai-compat"
}

func (r *Registry) GetAllSchemas(protocol string) []map[string]any {
	// 官方端点走原生延迟：工具留在 tools[] 里但打上 defer_loading，由服务端决定
	// 给不给模型看。这样即使发现了新工具，tools 数组的字节也不变，prompt cache
	// 的前缀不会被打断。其他端点只能把延迟工具整个藏起来，靠 McpCall 兜。
	native := r.McpLoadingMode == McpLoadingNative && !isOpenAIProtocol(protocol)
	schemas := make([]map[string]any, 0, len(r.tools))
	for _, t := range r.tools {
		// 检索和分发只在用得上的模式里发。eager 下没有延迟工具可搜、也不需要
		// 分发，两个都发过去只是白占 token，还可能引诱模型去绕一圈。
		if name := t.Name(); name == ToolSearchToolName && !r.ExposeToolSearch {
			continue
		} else if name == McpCallToolName && !r.ExposeMcpCall {
			continue
		}
		deferred := isDeferred(t) && !r.discoveredTools[t.Name()]
		if deferred && !native {
			continue
		}
		base := t.Schema()
		if isOpenAIProtocol(protocol) {
			schemas = append(schemas, map[string]any{
				"type":        "function",
				"name":        base["name"],
				"description": base["description"],
				"parameters":  base["input_schema"],
			})
		} else {
			if deferred {
				withFlag := make(map[string]any, len(base)+1)
				for k, v := range base {
					withFlag[k] = v
				}
				withFlag["defer_loading"] = true
				base = withFlag
			}
			schemas = append(schemas, base)
		}
	}
	return schemas
}

func (r *Registry) GetDeferredToolNames() []string {
	var names []string
	for _, t := range r.tools {
		if isDeferred(t) && !r.discoveredTools[t.Name()] {
			names = append(names, t.Name())
		}
	}
	return names
}

func (r *Registry) GetDeferredTools() []Tool {
	var result []Tool
	for _, t := range r.tools {
		if isDeferred(t) {
			result = append(result, t)
		}
	}
	return result
}

func (r *Registry) SearchDeferred(query string, maxResults int, protocol string) []map[string]any {
	query = strings.ToLower(query)
	var matches []map[string]any
	for _, t := range r.tools {
		if !isDeferred(t) {
			continue
		}
		name := strings.ToLower(t.Name())
		desc := strings.ToLower(t.Description())
		if strings.Contains(name, query) || strings.Contains(desc, query) {
			base := t.Schema()
			if isOpenAIProtocol(protocol) {
				matches = append(matches, map[string]any{
					"type":        "function",
					"name":        base["name"],
					"description": base["description"],
					"parameters":  base["input_schema"],
				})
			} else {
				matches = append(matches, base)
			}
			if len(matches) >= maxResults {
				break
			}
		}
	}
	return matches
}

func (r *Registry) FindDeferredByNames(names []string, protocol string) []map[string]any {
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[strings.ToLower(n)] = true
	}
	var matches []map[string]any
	for _, t := range r.tools {
		if nameSet[strings.ToLower(t.Name())] {
			base := t.Schema()
			if isOpenAIProtocol(protocol) {
				matches = append(matches, map[string]any{
					"type":        "function",
					"name":        base["name"],
					"description": base["description"],
					"parameters":  base["input_schema"],
				})
			} else {
				matches = append(matches, base)
			}
		}
	}
	return matches
}

type DefaultTools struct {
	Registry  *Registry
	WriteFile *WriteFileTool
	EditFile  *EditFileTool
}

func CreateDefaultRegistry() *Registry {
	dt := CreateDefaultTools()
	return dt.Registry
}

func CreateDefaultToolsWithWorkDir(workDir string) DefaultTools {
	fsc := NewFileStateCache()
	wf := &WriteFileTool{FileStateCache: fsc}
	ef := &EditFileTool{FileStateCache: fsc}
	reg := NewRegistry()
	reg.Register(&ReadFileTool{FileStateCache: fsc})
	reg.Register(wf)
	reg.Register(ef)
	reg.Register(&BashTool{WorkDir: workDir})
	reg.Register(&GlobTool{})
	reg.Register(&GrepTool{})
	return DefaultTools{Registry: reg, WriteFile: wf, EditFile: ef}
}

func CreateDefaultTools() DefaultTools {
	return CreateDefaultToolsWithWorkDir("")
}
