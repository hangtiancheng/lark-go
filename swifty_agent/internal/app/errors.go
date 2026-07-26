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

package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
)

// structuredError mirrors the Next.js /api/chat error envelope
// ({name, message, statusCode, url, responseBody}) so the frontend can parse
// upstream LLM API failures identically regardless of which backend served it.
type structuredError struct {
	Name         string `json:"name"`
	Message      string `json:"message"`
	StatusCode   int    `json:"statusCode,omitempty"`
	URL          string `json:"url,omitempty"`
	ResponseBody string `json:"responseBody,omitempty"`
}

// structuredErrorMessage extracts diagnostic fields from an LLM/API error and
// returns a JSON string suitable for the {message, data} response shape used by
// ctx.Throw. It aligns with the Next.js app/api/chat/route.ts error handler
// (lib/ai/models.ts -> APICallError).
//
// Extraction is reflection-based so the app package does not need to import
// provider SDKs. It works with any error type exposing these fields/methods —
// notably *anthropic.Error (StatusCode / Request.URL / RawJSON()) — and falls
// back to name+message for wrapped fmt.Errorf errors from other providers
// (e.g. the OpenAI-compatible ACL, whose errors do not expose HTTP metadata).
func structuredErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	se := structuredError{
		Name:    fmt.Sprintf("%T", err),
		Message: err.Error(),
	}

	// Reflect into the concrete error type to pull HTTP diagnostics generically.
	v := reflect.ValueOf(err)
	for v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct {
		if f := v.FieldByName("StatusCode"); f.IsValid() && f.Kind() == reflect.Int {
			se.StatusCode = int(f.Int())
		}
		if f := v.FieldByName("Request"); f.IsValid() && f.Kind() == reflect.Pointer && !f.IsNil() {
			se.URL = requestURL(f)
		}
		// *anthropic.Error exposes RawJSON() returning the cached response body.
		if m := v.MethodByName("RawJSON"); m.IsValid() && m.Type().NumIn() == 0 && m.Type().NumOut() == 1 {
			if outs := m.Call(nil); len(outs) == 1 && outs[0].Kind() == reflect.String {
				se.ResponseBody = outs[0].String()
			}
		}
	}

	// net/url.Error fallback for URL (covers low-level transport errors).
	var urlErr *url.Error
	if se.URL == "" && errors.As(err, &urlErr) {
		se.URL = urlErr.URL
	}

	b, mErr := json.Marshal(se)
	if mErr != nil {
		// Marshal failure: return the plain message so the client still gets
		// something useful rather than an empty body.
		return se.Message
	}
	return string(b)
}

// requestURL extracts the URL string from a *http.Request reflect.Value.
func requestURL(reqVal reflect.Value) string {
	if reqVal.Kind() != reflect.Pointer || reqVal.IsNil() {
		return ""
	}
	req := reqVal.Elem()
	urlField := req.FieldByName("URL")
	if !urlField.IsValid() || urlField.Kind() != reflect.Pointer || urlField.IsNil() {
		return ""
	}
	if u, ok := urlField.Interface().(*url.URL); ok && u != nil {
		return u.String()
	}
	return ""
}
