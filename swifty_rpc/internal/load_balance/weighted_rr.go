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

package load_balance

import (
	"sync"

	"github.com/hangtiancheng/swifty.go/swifty_rpc/internal/registry"
)

// WeightedRR implements smooth weighted round-robin selection.
type WeightedRR struct {
	mu            sync.Mutex
	weights       []int
	currentWeight []int
	totalWeight   int
}

func NewWeightedRR(weights []int) *WeightedRR {
	w := &WeightedRR{
		weights:       make([]int, len(weights)),
		currentWeight: make([]int, len(weights)),
	}

	total := 0
	for i, wt := range weights {
		if wt < 0 {
			wt = 0
		}
		w.weights[i] = wt
		total += wt
	}
	w.totalWeight = total

	return w
}

func (w *WeightedRR) Select(list []registry.Instance) registry.Instance {
	if len(list) == 0 {
		return registry.Instance{}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if len(list) != len(w.weights) {
		return registry.Instance{}
	}
	if w.totalWeight <= 0 {
		return registry.Instance{}
	}

	maxIdx := -1
	for i := range list {
		w.currentWeight[i] += w.weights[i]

		if maxIdx == -1 || w.currentWeight[i] > w.currentWeight[maxIdx] {
			maxIdx = i
		}
	}

	w.currentWeight[maxIdx] -= w.totalWeight
	return list[maxIdx]
}
