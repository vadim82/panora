package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type rpcProvider struct {
	validations int
	failure     error
}

func (*rpcProvider) Describe(context.Context) (Description, error) {
	return Description{Protocol: Protocol, ID: "fixture"}, nil
}
func (p *rpcProvider) Validate(context.Context, json.RawMessage) error {
	p.validations++
	return p.failure
}
func (*rpcProvider) Discover(context.Context, DiscoverParams) (Page, error) {
	return Page{Candidates: []Candidate{}, NextCursor: json.RawMessage("null")}, nil
}
func (*rpcProvider) Resolve(context.Context, ResolveParams) (Resolved, error) { return Resolved{}, nil }

func serveRPC(t *testing.T, p Provider, input string) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := Serve(context.Background(), p, strings.NewReader(input+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestJSONRPCRequestsAndStandardErrors(t *testing.T) {
	for _, tc := range []struct {
		name, request, id string
		code              int
	}{
		{"string ID", `{"jsonrpc":"2.0","id":"abc","method":"describe","params":{"protocol":2}}`, `"abc"`, 0},
		{"large numeric ID", `{"jsonrpc":"2.0","id":9007199254740993,"method":"describe","params":{"protocol":2}}`, `9007199254740993`, 0},
		{"null ID", `{"jsonrpc":"2.0","id":null,"method":"describe","params":{"protocol":2}}`, `null`, 0},
		{"parse error", `{"jsonrpc":`, `null`, -32700},
		{"scalar", `1`, `null`, -32600},
		{"wrong version", `{"jsonrpc":"1.0","id":1,"method":"describe"}`, `null`, -32600},
		{"legacy envelope", `{"protocol":1,"id":"old","op":"describe","params":{}}`, `null`, -32600},
		{"case sensitive names", `{"JSONRPC":"2.0","id":1,"method":"describe"}`, `null`, -32600},
		{"boolean ID", `{"jsonrpc":"2.0","id":true,"method":"describe"}`, `null`, -32600},
		{"object ID", `{"jsonrpc":"2.0","id":{},"method":"describe"}`, `null`, -32600},
		{"empty method", `{"jsonrpc":"2.0","id":1,"method":""}`, `1`, -32601},
		{"null method", `{"jsonrpc":"2.0","id":1,"method":null}`, `null`, -32600},
		{"parameter case", `{"jsonrpc":"2.0","id":1,"method":"validate","params":{"protocol":2,"Config":{}}}`, `1`, -32602},
		{"unknown method", `{"jsonrpc":"2.0","id":1,"method":"rpc.unknown"}`, `1`, -32601},
		{"scalar params", `{"jsonrpc":"2.0","id":1,"method":"describe","params":false}`, `null`, -32600},
		{"positional params", `{"jsonrpc":"2.0","id":1,"method":"describe","params":[2]}`, `1`, -32602},
		{"missing params", `{"jsonrpc":"2.0","id":1,"method":"describe"}`, `1`, -32602},
		{"old application version", `{"jsonrpc":"2.0","id":1,"method":"describe","params":{"protocol":1}}`, `1`, 1000},
		{"missing config", `{"jsonrpc":"2.0","id":1,"method":"validate","params":{"protocol":2}}`, `1`, -32602},
		{"unknown param", `{"jsonrpc":"2.0","id":1,"method":"describe","params":{"protocol":2,"secret":"hidden"}}`, `1`, -32602},
		{"missing cursor", `{"jsonrpc":"2.0","id":1,"method":"discover","params":{"protocol":2,"config":{},"limit":1}}`, `1`, -32602},
		{"oversized page", `{"jsonrpc":"2.0","id":1,"method":"discover","params":{"protocol":2,"config":{},"cursor":null,"limit":101}}`, `1`, -32602},
		{"invalid hint", `{"jsonrpc":"2.0","id":1,"method":"discover","params":{"protocol":2,"config":{},"cursor":null,"limit":1,"filter":{"ratio":{"width":0,"height":9}}}}`, `1`, -32602},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var response Response
			data := serveRPC(t, &rpcProvider{}, tc.request)
			if err := json.Unmarshal(data, &response); err != nil {
				t.Fatal(err)
			}
			if response.JSONRPC != JSONRPC || string(response.ID) != tc.id {
				t.Fatalf("wrong envelope: %s", data)
			}
			if tc.code == 0 {
				if response.Error != nil || len(response.Result) == 0 {
					t.Fatalf("not a success: %s", data)
				}
			} else if response.Error == nil || response.Error.Code != tc.code {
				t.Fatalf("wrong error: %s", data)
			}
			if bytes.Contains(data, []byte("hidden")) {
				t.Fatal("raw parameters leaked")
			}
		})
	}
}

func TestJSONRPCBatchesAndNotifications(t *testing.T) {
	p := &rpcProvider{}
	notification := `{"jsonrpc":"2.0","method":"validate","params":{"protocol":2,"config":{}}}`
	if out := serveRPC(t, p, notification); len(out) != 0 || p.validations != 1 {
		t.Fatalf("notification: %s calls %d", out, p.validations)
	}
	if out := serveRPC(t, p, `{"jsonrpc":"2.0","method":"unknown"}`); len(out) != 0 {
		t.Fatalf("unknown notification replied: %s", out)
	}
	if out := serveRPC(t, p, "["+notification+","+notification+"]"); len(out) != 0 || p.validations != 3 {
		t.Fatalf("notification batch: %s", out)
	}
	out := serveRPC(t, p, "["+notification+`, 1, {"jsonrpc":"2.0","id":"b","method":"describe","params":{"protocol":2}}]`)
	var replies []Response
	if err := json.Unmarshal(out, &replies); err != nil || len(replies) != 2 || replies[0].Error.Code != -32600 || string(replies[1].ID) != `"b"` {
		t.Fatalf("batch: %s %v", out, err)
	}
	for _, input := range []string{"[]", "[" + strings.Repeat(notification+",", MaxBatchSize) + notification + "]"} {
		var response Response
		out := serveRPC(t, p, input)
		if err := json.Unmarshal(out, &response); err != nil || response.Error == nil || response.Error.Code != -32600 {
			t.Fatalf("batch limit: %s", out)
		}
	}
}

func TestJSONRPCProviderErrors(t *testing.T) {
	retry := int64(120)
	for _, failure := range []error{&Error{Code: "rate_limited", Message: "Try later", RetryAfterSeconds: &retry}, errors.New("private stack trace")} {
		p := &rpcProvider{failure: failure}
		out := serveRPC(t, p, `{"jsonrpc":"2.0","id":"a","method":"validate","params":{"protocol":2,"config":{}}}`)
		_, err := decodeResponse(out, "a")
		var typed *Error
		if errors.As(failure, &typed) {
			var actual *Error
			if !errors.As(err, &actual) || actual.Code != "rate_limited" || actual.RetryAfterSeconds == nil || *actual.RetryAfterSeconds != 120 {
				t.Fatalf("retry hint lost: %v", err)
			}
		} else {
			var rpc *RPCError
			if !errors.As(err, &rpc) || rpc.Code != -32603 || bytes.Contains(out, []byte("private")) {
				t.Fatalf("internal error: %s %v", out, err)
			}
		}
	}
}

func TestJSONRPCInputAndBatchOutputBudgets(t *testing.T) {
	for _, input := range []string{strings.Repeat(" ", MaxRequestBytes) + "\n", "{}", "{}\n{}\n"} {
		var out bytes.Buffer
		if err := Serve(context.Background(), &rpcProvider{}, strings.NewReader(input), &out); err == nil || out.Len() != 0 {
			t.Fatal("unbounded or broken framing accepted")
		}
	}
}

type largeRPCProvider struct{ rpcProvider }

func (largeRPCProvider) Describe(context.Context) (Description, error) {
	return Description{Name: strings.Repeat("x", MaxResponseBytes/2)}, nil
}

func TestJSONRPCBatchSharesResponseAndCallBudgets(t *testing.T) {
	call := `{"jsonrpc":"2.0","id":1,"method":"describe","params":{"protocol":2}}`
	var out bytes.Buffer
	if err := Serve(context.Background(), &largeRPCProvider{}, strings.NewReader("["+call+","+call+"]\n"), &out); err == nil || out.Len() != 0 {
		t.Fatal("batch bypassed response budget or emitted partial output")
	}
	p := &rpcProvider{}
	notification := `{"jsonrpc":"2.0","method":"validate","params":{"protocol":2,"config":{}}}`
	input := "[" + strings.Repeat(notification+",", MaxBatchSize-1) + notification + "]"
	if out := serveRPC(t, p, input); len(out) != 0 || p.validations != MaxBatchSize {
		t.Fatalf("valid batch limit: %s, calls=%d", out, p.validations)
	}
	p = &rpcProvider{}
	input = "[" + strings.Repeat(notification+",", MaxBatchSize) + notification + "]"
	serveRPC(t, p, input)
	if p.validations != 0 {
		t.Fatal("oversized batch executed calls")
	}
}
